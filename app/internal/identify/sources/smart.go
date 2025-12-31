package sources

import (
	"os/exec"
	"regexp"
	"strings"
	"sync"
)

// SmartSource collects device information from smartctl
type SmartSource struct{}

// Collect gathers SMART information for physical devices
// This source is slower as it queries each device individually
func (s *SmartSource) Collect() (map[string]*SourceEntity, error) {
	entities := make(map[string]*SourceEntity)

	// Get list of physical devices from lsblk first
	devices := s.getPhysicalDevices()
	if len(devices) == 0 {
		return entities, nil
	}

	// Query devices in parallel
	var wg sync.WaitGroup
	var mu sync.Mutex
	results := make(map[string]*SourceEntity)

	for _, dev := range devices {
		wg.Add(1)
		go func(device string) {
			defer wg.Done()
			entity := s.queryDevice(device)
			if entity != nil {
				mu.Lock()
				results[device] = entity
				mu.Unlock()
			}
		}(dev)
	}
	wg.Wait()

	return results, nil
}

// getPhysicalDevices returns a list of physical disk device paths
func (s *SmartSource) getPhysicalDevices() []string {
	var devices []string

	// Use lsblk to get disk devices only
	out, err := exec.Command("lsblk", "-d", "-n", "-o", "PATH,TYPE").Output()
	if err != nil {
		return devices
	}

	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == "disk" {
			devices = append(devices, fields[0])
		}
	}

	return devices
}

// queryDevice queries a single device with smartctl
func (s *SmartSource) queryDevice(device string) *SourceEntity {
	entity := &SourceEntity{
		DevicePath: device,
	}

	// Get device info (skip if in standby)
	out, err := exec.Command("smartctl", "-i", "-n", "standby", device).CombinedOutput()
	if err != nil {
		// Device might be in standby or not SMART capable
		return nil
	}

	output := string(out)

	// Skip if device is in standby
	if strings.Contains(output, "NOT READY") {
		return nil
	}

	// Extract Serial Number
	reSerial := regexp.MustCompile(`Serial [Nn]umber:\s+(\S+)`)
	if matches := reSerial.FindStringSubmatch(output); len(matches) > 1 {
		entity.Serial = ptr(matches[1])
	}

	// Extract Logical Unit ID (LUID)
	reLUID := regexp.MustCompile(`Logical Unit id:\s+(\S+)`)
	if matches := reLUID.FindStringSubmatch(output); len(matches) > 1 {
		entity.LUID = ptr(matches[1])
	}

	// Extract WWN if not found by lsblk
	reWWN := regexp.MustCompile(`LU WWN Device Id:\s+(\S+(?:\s+\S+)*)`)
	if matches := reWWN.FindStringSubmatch(output); len(matches) > 1 {
		// Normalize WWN format (remove spaces)
		wwn := strings.ReplaceAll(matches[1], " ", "")
		entity.WWN = ptr("0x" + wwn)
	}

	// Extract Model
	reModel := regexp.MustCompile(`Device Model:\s+(.+)`)
	if matches := reModel.FindStringSubmatch(output); len(matches) > 1 {
		entity.Model = ptr(strings.TrimSpace(matches[1]))
	}

	// Also try Product field for SCSI drives
	reProduct := regexp.MustCompile(`Product:\s+(.+)`)
	if entity.Model == nil {
		if matches := reProduct.FindStringSubmatch(output); len(matches) > 1 {
			entity.Model = ptr(strings.TrimSpace(matches[1]))
		}
	}

	// Extract Vendor for SCSI drives
	reVendor := regexp.MustCompile(`Vendor:\s+(.+)`)
	if matches := reVendor.FindStringSubmatch(output); len(matches) > 1 {
		entity.Vendor = ptr(strings.TrimSpace(matches[1]))
	}

	// Check for NVMe specific identifiers
	if strings.Contains(output, "NVMe") {
		s.extractNVMeIdentifiers(device, entity)
	}

	return entity
}

// extractNVMeIdentifiers extracts NVMe-specific identifiers
func (s *SmartSource) extractNVMeIdentifiers(device string, entity *SourceEntity) {
	// Try nvme id-ns command if available
	out, err := exec.Command("nvme", "id-ns", device, "-o", "normal").CombinedOutput()
	if err != nil {
		return
	}

	output := string(out)

	// Extract NGUID
	reNGUID := regexp.MustCompile(`nguid\s*:\s*(\S+)`)
	if matches := reNGUID.FindStringSubmatch(output); len(matches) > 1 {
		nguid := matches[1]
		if nguid != "0000000000000000" && nguid != "" {
			entity.NGUID = ptr(nguid)
		}
	}

	// Extract EUI64
	reEUI := regexp.MustCompile(`eui64\s*:\s*(\S+)`)
	if matches := reEUI.FindStringSubmatch(output); len(matches) > 1 {
		eui := matches[1]
		if eui != "0000000000000000" && eui != "" {
			entity.EUI64 = ptr(eui)
		}
	}
}
