package sources

import (
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// ZFSSource collects ZFS pool, vdev, and dataset information
type ZFSSource struct{}

// poolInfo holds parsed pool data
type poolInfo struct {
	Name string
	GUID string
}

// vdevInfo holds parsed vdev data
type vdevInfo struct {
	PoolName string
	PoolGUID string
	VdevGUID string
	Device   string
}

// datasetInfo holds parsed dataset data
type datasetInfo struct {
	Name string
	GUID string
}

// Collect gathers ZFS-related device information
func (s *ZFSSource) Collect() (map[string]*SourceEntity, error) {
	entities := make(map[string]*SourceEntity)

	// Check if ZFS is available
	if _, err := exec.LookPath("zpool"); err != nil {
		return entities, nil
	}

	// Get pool GUIDs
	pools := s.getPools()
	if len(pools) == 0 {
		return entities, nil
	}

	// Get vdev -> device mapping with GUIDs
	vdevs := s.getVdevs()

	// Create entities for ZFS pools
	for _, pool := range pools {
		entity := &SourceEntity{
			Type:        "zfs_pool",
			ZFSPoolName: ptr(pool.Name),
			ZFSPoolGUID: ptr(pool.GUID),
		}
		// Use pool name as the key for pool entities
		key := "zfs:pool:" + pool.Name
		entities[key] = entity
	}

	// Create entities for physical devices in pools
	for _, vdev := range vdevs {
		// Resolve device path
		devPath := s.resolveDevice(vdev.Device)
		if devPath == "" {
			continue
		}

		entity := &SourceEntity{
			DevicePath:  devPath,
			ZFSPoolName: ptr(vdev.PoolName),
			ZFSPoolGUID: ptr(vdev.PoolGUID),
			ZFSVdevGUID: ptr(vdev.VdevGUID),
		}
		entities[devPath] = entity
	}

	// Get dataset information
	datasets := s.getDatasets()
	for _, ds := range datasets {
		entity := &SourceEntity{
			Type:           "zfs_dataset",
			ZFSDatasetName: ptr(ds.Name),
			ZFSDatasetGUID: ptr(ds.GUID),
		}
		// Extract pool name from dataset
		parts := strings.SplitN(ds.Name, "/", 2)
		if len(parts) > 0 {
			for _, pool := range pools {
				if pool.Name == parts[0] {
					entity.ZFSPoolName = ptr(pool.Name)
					entity.ZFSPoolGUID = ptr(pool.GUID)
					break
				}
			}
		}
		key := "zfs:dataset:" + ds.Name
		entities[key] = entity
	}

	return entities, nil
}

// getPools returns pool names and GUIDs
func (s *ZFSSource) getPools() []poolInfo {
	var pools []poolInfo

	out, err := exec.Command("zpool", "get", "-H", "-o", "name,value", "guid").Output()
	if err != nil {
		return pools
	}

	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			pools = append(pools, poolInfo{
				Name: fields[0],
				GUID: fields[1],
			})
		}
	}

	return pools
}

// getVdevs parses zpool status -gL to get vdev GUIDs and device mappings
func (s *ZFSSource) getVdevs() []vdevInfo {
	var vdevs []vdevInfo

	out, err := exec.Command("zpool", "status", "-gL").Output()
	if err != nil {
		return vdevs
	}

	lines := strings.Split(string(out), "\n")
	var currentPool string
	var currentPoolGUID string

	// Get pool GUIDs first
	poolGUIDs := make(map[string]string)
	pools := s.getPools()
	for _, p := range pools {
		poolGUIDs[p.Name] = p.GUID
	}

	// Regex to match device lines with GUID
	// Format: /dev/sdX  ONLINE  0  0  0  <guid>
	// or:     sdX       ONLINE  0  0  0  <guid>
	reDevice := regexp.MustCompile(`^\s+(/dev/\S+|\S+)\s+\S+\s+\d+\s+\d+\s+\d+\s*(\d*)`)

	for _, line := range lines {
		// Check for pool name
		if strings.HasPrefix(line, "  pool:") {
			currentPool = strings.TrimSpace(strings.TrimPrefix(line, "  pool:"))
			currentPoolGUID = poolGUIDs[currentPool]
			continue
		}

		// Check for device line
		if matches := reDevice.FindStringSubmatch(line); len(matches) >= 2 {
			device := matches[1]
			vdevGUID := ""
			if len(matches) > 2 {
				vdevGUID = matches[2]
			}

			// Skip non-device entries
			if strings.HasPrefix(device, "raidz") ||
				strings.HasPrefix(device, "mirror") ||
				strings.HasPrefix(device, "spare") ||
				strings.HasPrefix(device, "log") ||
				strings.HasPrefix(device, "cache") {
				continue
			}

			vdevs = append(vdevs, vdevInfo{
				PoolName: currentPool,
				PoolGUID: currentPoolGUID,
				VdevGUID: vdevGUID,
				Device:   device,
			})
		}
	}

	return vdevs
}

// getDatasets returns dataset names and GUIDs
func (s *ZFSSource) getDatasets() []datasetInfo {
	var datasets []datasetInfo

	out, err := exec.Command("zfs", "get", "-H", "-o", "name,value", "guid").Output()
	if err != nil {
		return datasets
	}

	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			datasets = append(datasets, datasetInfo{
				Name: fields[0],
				GUID: fields[1],
			})
		}
	}

	return datasets
}

// resolveDevice resolves a device name to its full path
func (s *ZFSSource) resolveDevice(device string) string {
	// Already a full path
	if strings.HasPrefix(device, "/dev/") {
		// Resolve any symlinks
		resolved, err := filepath.EvalSymlinks(device)
		if err == nil {
			return resolved
		}
		return device
	}

	// Try /dev prefix
	devPath := "/dev/" + device
	resolved, err := filepath.EvalSymlinks(devPath)
	if err == nil {
		return resolved
	}

	return devPath
}
