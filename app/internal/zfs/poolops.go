package zfs

import (
	"fmt"
	"os/exec"
	"strings"
)

// ExportPool safely exports a ZFS pool with sync
func ExportPool(poolName string) error {
	// 1. Sync filesystem buffers
	if err := exec.Command("sync").Run(); err != nil {
		return fmt.Errorf("sync failed: %w", err)
	}

	// 2. Sync the specific pool
	if out, err := exec.Command("zpool", "sync", poolName).CombinedOutput(); err != nil {
		return fmt.Errorf("zpool sync failed: %s: %w", strings.TrimSpace(string(out)), err)
	}

	// 3. Export the pool
	if out, err := exec.Command("zpool", "export", poolName).CombinedOutput(); err != nil {
		return fmt.Errorf("zpool export failed: %s: %w", strings.TrimSpace(string(out)), err)
	}

	return nil
}

// ImportPool imports a previously exported ZFS pool
func ImportPool(poolName string) error {
	out, err := exec.Command("zpool", "import", poolName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("zpool import failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// IsPoolImported checks if a pool is currently imported
func IsPoolImported(poolName string) bool {
	out, err := exec.Command("zpool", "list", "-H", "-o", "name").CombinedOutput()
	if err != nil {
		return false
	}

	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) == poolName {
			return true
		}
	}
	return false
}

// GetPoolDevices returns device paths for all drives in a pool
func GetPoolDevices(poolName string) ([]string, error) {
	health, err := GetPoolHealth(poolName)
	if err != nil {
		return nil, err
	}

	var devices []string
	seen := make(map[string]bool)

	for _, vdev := range health.GetAllDevices() {
		if vdev.DevicePath != "" {
			// Normalize: strip partition suffix to get base device
			devPath := normalizeDevicePath(vdev.DevicePath)
			if !seen[devPath] {
				seen[devPath] = true
				devices = append(devices, devPath)
			}
		}
	}

	return devices, nil
}

// normalizeDevicePath strips partition suffix from device path
func normalizeDevicePath(path string) string {
	// /dev/sda1 -> /dev/sda
	// /dev/nvme0n1p1 -> /dev/nvme0n1
	if strings.HasPrefix(path, "/dev/nvme") {
		// NVMe: strip pN suffix
		if idx := strings.LastIndex(path, "p"); idx > 0 {
			base := path[:idx]
			// Verify there's a number after 'p'
			if len(path) > idx+1 && path[idx+1] >= '0' && path[idx+1] <= '9' {
				return base
			}
		}
	} else if strings.HasPrefix(path, "/dev/sd") || strings.HasPrefix(path, "/dev/hd") {
		// SATA/SAS: strip trailing digits
		i := len(path) - 1
		for i >= 0 && path[i] >= '0' && path[i] <= '9' {
			i--
		}
		return path[:i+1]
	}
	return path
}
