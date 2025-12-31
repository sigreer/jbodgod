package sources

import (
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// MDRaidSource collects MD RAID array information
type MDRaidSource struct{}

// arrayInfo holds parsed MD array data
type arrayInfo struct {
	Device string
	Name   string
	UUID   string
}

// Collect gathers MD RAID information
func (s *MDRaidSource) Collect() (map[string]*SourceEntity, error) {
	entities := make(map[string]*SourceEntity)

	// Check if mdadm is available
	if _, err := exec.LookPath("mdadm"); err != nil {
		return entities, nil
	}

	// Get array information from mdadm --detail --scan
	arrays := s.getArrays()

	for _, arr := range arrays {
		devPath := s.resolveDevice(arr.Device)

		entity := &SourceEntity{
			Type:       "md_array",
			DevicePath: devPath,
			MDArrUUID:  ptr(arr.UUID),
		}

		if arr.Name != "" {
			entity.MDName = ptr(arr.Name)
		}

		entities[devPath] = entity
	}

	return entities, nil
}

// getArrays returns MD array information from mdadm --detail --scan
func (s *MDRaidSource) getArrays() []arrayInfo {
	var arrays []arrayInfo

	out, err := exec.Command("mdadm", "--detail", "--scan").Output()
	if err != nil {
		return arrays
	}

	// Parse output like:
	// ARRAY /dev/md/array1 metadata=1.2 UUID=12345678:90abcdef:12345678:90abcdef name=hostname:array1 ...
	reArray := regexp.MustCompile(`ARRAY\s+(\S+)`)
	reUUID := regexp.MustCompile(`UUID=([0-9a-fA-F:]+)`)
	reName := regexp.MustCompile(`name=\S+:(\S+)`)

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "ARRAY") {
			continue
		}

		var arr arrayInfo

		// Extract device path
		if matches := reArray.FindStringSubmatch(line); len(matches) > 1 {
			arr.Device = matches[1]
		}

		// Extract UUID
		if matches := reUUID.FindStringSubmatch(line); len(matches) > 1 {
			arr.UUID = matches[1]
		}

		// Extract name
		if matches := reName.FindStringSubmatch(line); len(matches) > 1 {
			arr.Name = matches[1]
		}

		if arr.Device != "" {
			arrays = append(arrays, arr)
		}
	}

	return arrays
}

// resolveDevice resolves a device path to its canonical form
func (s *MDRaidSource) resolveDevice(device string) string {
	if device == "" {
		return ""
	}

	resolved, err := filepath.EvalSymlinks(device)
	if err != nil {
		return device
	}
	return resolved
}
