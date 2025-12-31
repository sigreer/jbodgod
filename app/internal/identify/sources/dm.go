package sources

import (
	"os/exec"
	"path/filepath"
	"strings"
)

// DMSource collects device-mapper information
type DMSource struct{}

// dmInfo holds parsed device-mapper data
type dmInfo struct {
	Name   string
	UUID   string
	MajMin string
}

// Collect gathers device-mapper information
func (s *DMSource) Collect() (map[string]*SourceEntity, error) {
	entities := make(map[string]*SourceEntity)

	// Check if dmsetup is available
	if _, err := exec.LookPath("dmsetup"); err != nil {
		return entities, nil
	}

	// Get DM device information
	devices := s.getDevices()

	for _, dm := range devices {
		// Construct device path from name
		devPath := "/dev/mapper/" + dm.Name
		resolved := s.resolveDevice(devPath)

		entity := &SourceEntity{
			Type:       "dm_device",
			DevicePath: resolved,
			DMName:     ptr(dm.Name),
		}

		if dm.UUID != "" {
			entity.DMUUID = ptr(dm.UUID)
		}

		if dm.MajMin != "" {
			entity.MajMin = ptr(dm.MajMin)
		}

		entities[resolved] = entity
	}

	return entities, nil
}

// getDevices returns device-mapper device information
func (s *DMSource) getDevices() []dmInfo {
	var devices []dmInfo

	// Get name,uuid,major,minor
	out, err := exec.Command("dmsetup", "info", "-c", "--noheadings", "-o", "name,uuid,major,minor").Output()
	if err != nil {
		return devices
	}

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Parse colon-separated fields
		fields := strings.Split(line, ":")
		if len(fields) < 4 {
			continue
		}

		dm := dmInfo{
			Name: fields[0],
			UUID: fields[1],
		}

		// Construct maj:min from major and minor
		if fields[2] != "" && fields[3] != "" {
			dm.MajMin = fields[2] + ":" + fields[3]
		}

		devices = append(devices, dm)
	}

	return devices
}

// resolveDevice resolves a device path to its canonical form
func (s *DMSource) resolveDevice(device string) string {
	if device == "" {
		return ""
	}

	resolved, err := filepath.EvalSymlinks(device)
	if err != nil {
		return device
	}
	return resolved
}
