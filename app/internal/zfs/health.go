package zfs

import (
	"bufio"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// PoolHealth represents the health status of a ZFS pool
type PoolHealth struct {
	Name        string       `json:"name"`
	State       string       `json:"state"`        // ONLINE, DEGRADED, FAULTED, OFFLINE, REMOVED, UNAVAIL
	Status      string       `json:"status,omitempty"` // Status message if any
	Action      string       `json:"action,omitempty"` // Recommended action
	ScanState   string       `json:"scan_state,omitempty"` // scrub, resilver, none
	ScanPercent float64      `json:"scan_percent,omitempty"` // Progress percentage
	ScanMessage string       `json:"scan_message,omitempty"` // Full scan line
	Errors      string       `json:"errors,omitempty"` // Error summary
	Vdevs       []VdevHealth `json:"vdevs"`
	TotalErrors int64        `json:"total_errors"` // Sum of all error counts
}

// VdevHealth represents per-vdev/device health
type VdevHealth struct {
	Name       string       `json:"name"`
	Type       string       `json:"type"`        // pool, raidz, mirror, disk, spare, log, cache
	State      string       `json:"state"`       // ONLINE, DEGRADED, FAULTED, OFFLINE, REMOVED, UNAVAIL
	DevicePath string       `json:"device_path,omitempty"` // /dev/sdX for leaf devices
	ReadErrs   int64        `json:"read_errors"`
	WriteErrs  int64        `json:"write_errors"`
	CksumErrs  int64        `json:"cksum_errors"`
	SlowIOs    int64        `json:"slow_ios,omitempty"`
	Children   []VdevHealth `json:"children,omitempty"` // Nested vdevs
	Depth      int          `json:"-"` // Indentation depth for parsing
}

// Pool states
const (
	StateOnline  = "ONLINE"
	StateDegraded = "DEGRADED"
	StateFaulted = "FAULTED"
	StateOffline = "OFFLINE"
	StateRemoved = "REMOVED"
	StateUnavail = "UNAVAIL"
)

// Vdev types
const (
	TypePool   = "pool"
	TypeRaidz  = "raidz"
	TypeMirror = "mirror"
	TypeDisk   = "disk"
	TypeSpare  = "spare"
	TypeLog    = "log"
	TypeCache  = "cache"
)

// GetPoolHealth parses zpool status for a specific pool
func GetPoolHealth(poolName string) (*PoolHealth, error) {
	out, err := exec.Command("zpool", "status", "-vL", poolName).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to get pool status: %w", err)
	}

	pools := parseZpoolStatus(string(out))
	if len(pools) == 0 {
		return nil, fmt.Errorf("pool not found: %s", poolName)
	}

	return pools[0], nil
}

// GetAllPoolHealth returns health for all pools
func GetAllPoolHealth() ([]*PoolHealth, error) {
	out, err := exec.Command("zpool", "status", "-vL").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to get pool status: %w", err)
	}

	return parseZpoolStatus(string(out)), nil
}

// IsDegraded returns true if pool is not fully healthy
func (p *PoolHealth) IsDegraded() bool {
	return p.State != StateOnline
}

// HasErrors returns true if any device has errors
func (p *PoolHealth) HasErrors() bool {
	return p.TotalErrors > 0
}

// GetFaultedDevices returns devices that are not ONLINE
func (p *PoolHealth) GetFaultedDevices() []VdevHealth {
	var faulted []VdevHealth
	for _, v := range p.Vdevs {
		faulted = append(faulted, getFaultedRecursive(v)...)
	}
	return faulted
}

// GetAllDevices returns all leaf devices (actual disks)
func (p *PoolHealth) GetAllDevices() []VdevHealth {
	var devices []VdevHealth
	for _, v := range p.Vdevs {
		devices = append(devices, getLeafDevicesRecursive(v)...)
	}
	return devices
}

func getFaultedRecursive(v VdevHealth) []VdevHealth {
	var faulted []VdevHealth
	if v.State != StateOnline && v.Type == TypeDisk {
		faulted = append(faulted, v)
	}
	for _, child := range v.Children {
		faulted = append(faulted, getFaultedRecursive(child)...)
	}
	return faulted
}

func getLeafDevicesRecursive(v VdevHealth) []VdevHealth {
	if len(v.Children) == 0 && v.Type == TypeDisk {
		return []VdevHealth{v}
	}
	var devices []VdevHealth
	for _, child := range v.Children {
		devices = append(devices, getLeafDevicesRecursive(child)...)
	}
	return devices
}

// parseZpoolStatus parses the output of zpool status -vL
func parseZpoolStatus(output string) []*PoolHealth {
	var pools []*PoolHealth
	var current *PoolHealth
	var inConfig bool
	var configLines []string

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()

		// New pool starts with "  pool:"
		if strings.HasPrefix(line, "  pool:") {
			// Save previous pool
			if current != nil {
				parseConfigSection(current, configLines)
				pools = append(pools, current)
			}

			current = &PoolHealth{
				Name: strings.TrimSpace(strings.TrimPrefix(line, "  pool:")),
			}
			inConfig = false
			configLines = nil
			continue
		}

		if current == nil {
			continue
		}

		// Parse pool properties
		if strings.HasPrefix(line, " state:") {
			current.State = strings.TrimSpace(strings.TrimPrefix(line, " state:"))
		} else if strings.HasPrefix(line, "status:") {
			current.Status = strings.TrimSpace(strings.TrimPrefix(line, "status:"))
		} else if strings.HasPrefix(line, "action:") {
			current.Action = strings.TrimSpace(strings.TrimPrefix(line, "action:"))
		} else if strings.HasPrefix(line, "  scan:") {
			current.ScanMessage = strings.TrimSpace(strings.TrimPrefix(line, "  scan:"))
			parseScanState(current)
		} else if strings.HasPrefix(line, "errors:") {
			current.Errors = strings.TrimSpace(strings.TrimPrefix(line, "errors:"))
		} else if strings.HasPrefix(line, "config:") {
			inConfig = true
		} else if inConfig {
			// Skip header line (NAME STATE READ WRITE CKSUM)
			if strings.Contains(line, "NAME") && strings.Contains(line, "STATE") {
				continue
			}
			// Skip empty lines in config
			if strings.TrimSpace(line) == "" {
				continue
			}
			// End of config section
			if !strings.HasPrefix(line, "\t") && strings.TrimSpace(line) != "" && !strings.HasPrefix(strings.TrimSpace(line), "	") {
				inConfig = false
				continue
			}
			configLines = append(configLines, line)
		}
	}

	// Save last pool
	if current != nil {
		parseConfigSection(current, configLines)
		pools = append(pools, current)
	}

	return pools
}

func parseScanState(p *PoolHealth) {
	msg := p.ScanMessage
	if strings.Contains(msg, "scrub in progress") {
		p.ScanState = "scrub"
		// Try to extract percentage
		re := regexp.MustCompile(`(\d+\.?\d*)%`)
		if matches := re.FindStringSubmatch(msg); len(matches) > 1 {
			p.ScanPercent, _ = strconv.ParseFloat(matches[1], 64)
		}
	} else if strings.Contains(msg, "resilver in progress") {
		p.ScanState = "resilver"
		re := regexp.MustCompile(`(\d+\.?\d*)%`)
		if matches := re.FindStringSubmatch(msg); len(matches) > 1 {
			p.ScanPercent, _ = strconv.ParseFloat(matches[1], 64)
		}
	} else if strings.Contains(msg, "scrub repaired") || strings.Contains(msg, "scrub canceled") {
		p.ScanState = "none"
	} else if strings.Contains(msg, "resilvered") {
		p.ScanState = "none"
	}
}

// parseConfigSection parses the config section lines into vdevs
func parseConfigSection(p *PoolHealth, lines []string) {
	if len(lines) == 0 {
		return
	}

	// Parse each line to get vdev hierarchy
	// Lines are tab-indented to show hierarchy
	var vdevStack []*VdevHealth

	for _, line := range lines {
		// Count leading tabs to determine depth
		depth := 0
		for _, c := range line {
			if c == '\t' {
				depth++
			} else {
				break
			}
		}

		// Parse the line: NAME STATE READ WRITE CKSUM
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}

		name := fields[0]
		state := fields[1]
		readErrs, _ := strconv.ParseInt(fields[2], 10, 64)
		writeErrs, _ := strconv.ParseInt(fields[3], 10, 64)
		cksumErrs, _ := strconv.ParseInt(fields[4], 10, 64)

		vdev := VdevHealth{
			Name:      name,
			State:     state,
			ReadErrs:  readErrs,
			WriteErrs: writeErrs,
			CksumErrs: cksumErrs,
			Depth:     depth,
			Type:      determineVdevType(name),
		}

		// Set device path for leaf devices
		if vdev.Type == TypeDisk {
			vdev.DevicePath = "/dev/" + strings.TrimSuffix(name, "1") // Remove partition suffix
			// Also store full path with partition if present
			if strings.HasSuffix(name, "1") || strings.HasSuffix(name, "2") {
				vdev.DevicePath = "/dev/" + name
			}
		}

		// Add errors to pool total
		p.TotalErrors += readErrs + writeErrs + cksumErrs

		// Build hierarchy based on depth
		if depth == 1 {
			// Top-level vdev (pool name)
			p.Vdevs = append(p.Vdevs, vdev)
			vdevStack = []*VdevHealth{&p.Vdevs[len(p.Vdevs)-1]}
		} else if depth == 2 {
			// Child of pool (raidz, mirror, or disk)
			if len(vdevStack) > 0 {
				parent := vdevStack[0]
				parent.Children = append(parent.Children, vdev)
				if depth+1 > len(vdevStack) {
					vdevStack = append(vdevStack, &parent.Children[len(parent.Children)-1])
				} else {
					vdevStack[1] = &parent.Children[len(parent.Children)-1]
				}
			}
		} else if depth >= 3 {
			// Child of raidz/mirror (disk)
			if len(vdevStack) >= 2 {
				parent := vdevStack[1]
				parent.Children = append(parent.Children, vdev)
			}
		}
	}
}

func determineVdevType(name string) string {
	if strings.HasPrefix(name, "raidz") {
		return TypeRaidz
	}
	if strings.HasPrefix(name, "mirror") {
		return TypeMirror
	}
	if strings.HasPrefix(name, "spare") {
		return TypeSpare
	}
	if strings.HasPrefix(name, "log") || strings.HasPrefix(name, "logs") {
		return TypeLog
	}
	if strings.HasPrefix(name, "cache") {
		return TypeCache
	}
	// If it starts with sd, nvme, or similar, it's a disk
	if strings.HasPrefix(name, "sd") || strings.HasPrefix(name, "nvme") ||
		strings.HasPrefix(name, "hd") || strings.HasPrefix(name, "vd") ||
		strings.HasPrefix(name, "/dev/") {
		return TypeDisk
	}
	// Otherwise, treat as pool root
	return TypePool
}

// ListPools returns the names of all pools
func ListPools() ([]string, error) {
	out, err := exec.Command("zpool", "list", "-H", "-o", "name").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to list pools: %w", err)
	}

	var pools []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			pools = append(pools, line)
		}
	}
	return pools, nil
}

// GetPoolProperty gets a single property from a pool
func GetPoolProperty(poolName, property string) (string, error) {
	out, err := exec.Command("zpool", "get", "-H", "-o", "value", property, poolName).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get pool property: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
