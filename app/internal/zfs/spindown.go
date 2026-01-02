package zfs

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/sigreer/jbodgod/internal/collector"
)

// PoolDriveMapping maps a pool to the drives that are part of it
type PoolDriveMapping struct {
	PoolName string
	Devices  []string // /dev/sdX paths
	Serials  []string // Drive serials for tracking
}

// AnalyzeSpindownTargets examines drives and groups them by ZFS pool membership
// Returns: ZFS pools with their drives, non-ZFS drives, and any error
func AnalyzeSpindownTargets(devices []string) ([]PoolDriveMapping, []string, error) {
	// Collect system data to get ZFS membership info
	sysData := collector.CollectSystemData(false)

	poolDrives := make(map[string]*PoolDriveMapping)
	var nonZfsDrives []string

	for _, device := range devices {
		// Get comprehensive drive data including ZFS membership
		driveData := collector.GetDriveData(device, sysData)

		if driveData.Zpool != nil && *driveData.Zpool != "" {
			poolName := *driveData.Zpool
			if poolDrives[poolName] == nil {
				poolDrives[poolName] = &PoolDriveMapping{
					PoolName: poolName,
				}
			}
			poolDrives[poolName].Devices = append(poolDrives[poolName].Devices, device)

			// Track serial for database recording
			if driveData.Serial != nil {
				poolDrives[poolName].Serials = append(poolDrives[poolName].Serials, *driveData.Serial)
			}
		} else {
			nonZfsDrives = append(nonZfsDrives, device)
		}
	}

	// Convert map to slice
	var result []PoolDriveMapping
	for _, pm := range poolDrives {
		result = append(result, *pm)
	}

	return result, nonZfsDrives, nil
}

// PromptForPoolExport prompts the user to confirm exporting a pool
// Returns true if user confirms, false otherwise
func PromptForPoolExport(reader *bufio.Reader, pool PoolDriveMapping) bool {
	deviceList := strings.Join(pool.Devices, ", ")
	fmt.Printf("\nPool '%s' uses drives: %s\n", pool.PoolName, deviceList)
	fmt.Print("Export pool before spindown? [y/n]: ")

	response, err := reader.ReadString('\n')
	if err != nil {
		return false
	}

	response = strings.TrimSpace(strings.ToLower(response))
	return response == "y" || response == "yes"
}

// GetDriveSerial returns the serial number for a device
func GetDriveSerial(device string) string {
	sysData := collector.CollectSystemData(false)
	driveData := collector.GetDriveData(device, sysData)
	if driveData.Serial != nil {
		return *driveData.Serial
	}
	return ""
}
