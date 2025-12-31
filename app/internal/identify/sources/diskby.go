package sources

import (
	"os"
	"path/filepath"
)

// DiskBySource collects device symlinks from /dev/disk/by-*
type DiskBySource struct{}

// DiskByResult holds symlink mappings for a device
type DiskByResult struct {
	ByID        []string
	ByPath      []string
	ByUUID      *string
	ByPartUUID  *string
	ByLabel     *string
	ByPartLabel *string
}

// Collect gathers /dev/disk/by-* symlink information
func (s *DiskBySource) Collect() (map[string]*SourceEntity, error) {
	entities := make(map[string]*SourceEntity)

	// Collect symlinks from each by-* directory
	byID := s.readSymlinks("/dev/disk/by-id")
	byPath := s.readSymlinks("/dev/disk/by-path")
	byUUID := s.readSymlinks("/dev/disk/by-uuid")
	byPartUUID := s.readSymlinks("/dev/disk/by-partuuid")
	byLabel := s.readSymlinks("/dev/disk/by-label")
	byPartLabel := s.readSymlinks("/dev/disk/by-partlabel")

	// Aggregate by device path
	deviceLinks := make(map[string]*DiskByResult)

	// Process by-id (multiple per device possible)
	for linkName, devPath := range byID {
		if _, ok := deviceLinks[devPath]; !ok {
			deviceLinks[devPath] = &DiskByResult{}
		}
		deviceLinks[devPath].ByID = append(deviceLinks[devPath].ByID, linkName)
	}

	// Process by-path (multiple per device possible)
	for linkName, devPath := range byPath {
		if _, ok := deviceLinks[devPath]; !ok {
			deviceLinks[devPath] = &DiskByResult{}
		}
		deviceLinks[devPath].ByPath = append(deviceLinks[devPath].ByPath, linkName)
	}

	// Process by-uuid (one per device)
	for linkName, devPath := range byUUID {
		if _, ok := deviceLinks[devPath]; !ok {
			deviceLinks[devPath] = &DiskByResult{}
		}
		deviceLinks[devPath].ByUUID = ptr(linkName)
	}

	// Process by-partuuid (one per device)
	for linkName, devPath := range byPartUUID {
		if _, ok := deviceLinks[devPath]; !ok {
			deviceLinks[devPath] = &DiskByResult{}
		}
		deviceLinks[devPath].ByPartUUID = ptr(linkName)
	}

	// Process by-label (one per device)
	for linkName, devPath := range byLabel {
		if _, ok := deviceLinks[devPath]; !ok {
			deviceLinks[devPath] = &DiskByResult{}
		}
		deviceLinks[devPath].ByLabel = ptr(linkName)
	}

	// Process by-partlabel (one per device)
	for linkName, devPath := range byPartLabel {
		if _, ok := deviceLinks[devPath]; !ok {
			deviceLinks[devPath] = &DiskByResult{}
		}
		deviceLinks[devPath].ByPartLabel = ptr(linkName)
	}

	// Convert to entities
	for devPath, links := range deviceLinks {
		entity := &SourceEntity{
			DevicePath:  devPath,
			ByID:        links.ByID,
			ByPath:      links.ByPath,
			ByUUID:      links.ByUUID,
			ByPartUUID:  links.ByPartUUID,
			ByLabel:     links.ByLabel,
			ByPartLabel: links.ByPartLabel,
		}
		entities[devPath] = entity
	}

	return entities, nil
}

// readSymlinks reads all symlinks in a directory and returns a map of link name -> resolved path
func (s *DiskBySource) readSymlinks(dir string) map[string]string {
	result := make(map[string]string)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return result
	}

	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink == 0 {
			continue
		}

		linkPath := filepath.Join(dir, entry.Name())
		target, err := filepath.EvalSymlinks(linkPath)
		if err != nil {
			continue
		}

		result[entry.Name()] = target
	}

	return result
}

// GetSymlinkMappings returns a map of full symlink paths to their targets
// This is used by the index for reverse lookups
func (s *DiskBySource) GetSymlinkMappings() map[string]string {
	mappings := make(map[string]string)

	dirs := []string{
		"/dev/disk/by-id",
		"/dev/disk/by-path",
		"/dev/disk/by-uuid",
		"/dev/disk/by-partuuid",
		"/dev/disk/by-label",
		"/dev/disk/by-partlabel",
	}

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if entry.Type()&os.ModeSymlink == 0 {
				continue
			}

			linkPath := filepath.Join(dir, entry.Name())
			target, err := filepath.EvalSymlinks(linkPath)
			if err != nil {
				continue
			}

			mappings[linkPath] = target
		}
	}

	return mappings
}
