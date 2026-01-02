package collector

import (
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/sigreer/jbodgod/internal/cache"
)

// GetDriveData collects comprehensive data for a single drive
func GetDriveData(device string, sysData *SystemData) *DriveData {
	// Start with device path
	data := &DriveData{
		Device: device,
		State:  "unknown",
	}

	// Get device name (sda, sdb, etc.)
	devName := strings.TrimPrefix(device, "/dev/")

	// === Merge from lsblk ===
	if lsblk, ok := sysData.LsblkDevices[devName]; ok {
		data.Serial = lsblk.Serial
		data.WWN = lsblk.WWN
		data.Model = lsblk.Model
		data.Vendor = lsblk.Vendor
		data.Firmware = lsblk.Rev
		data.SizeBytes = lsblk.Size
		data.Protocol = lsblk.Tran
		data.SCSIAddr = lsblk.HCTL
	}

	// === Merge from lsscsi ===
	if lsscsi, ok := sysData.LsscsiDevices[device]; ok {
		if data.SCSIAddr == nil {
			data.SCSIAddr = &lsscsi.HCTL
		}
		if data.Vendor == nil {
			data.Vendor = lsscsi.Vendor
		}
		if data.Model == nil {
			data.Model = lsscsi.Model
		}
		if data.Firmware == nil {
			data.Firmware = lsscsi.Rev
		}
	}

	// === Merge from by-id ===
	if byID, ok := sysData.ByIDLinks[device]; ok {
		data.ByIDPath = &byID
	}

	// === Get serial from smartctl if not available (works in standby) ===
	if data.Serial == nil {
		smartData := getSmartInfo(device)
		if smartData != nil {
			if smartData.Serial != nil {
				data.Serial = smartData.Serial
			}
			if smartData.LUID != nil {
				data.LUID = smartData.LUID
			}
			if smartData.WWN != nil && data.WWN == nil {
				data.WWN = smartData.WWN
			}
			if smartData.Model != nil && data.Model == nil {
				data.Model = smartData.Model
			}
			if smartData.Vendor != nil && data.Vendor == nil {
				data.Vendor = smartData.Vendor
			}
			if smartData.Firmware != nil && data.Firmware == nil {
				data.Firmware = smartData.Firmware
			}
			if smartData.SizeBytes != nil && data.SizeBytes == nil {
				data.SizeBytes = smartData.SizeBytes
			}
			if smartData.FormFactor != nil {
				data.FormFactor = smartData.FormFactor
			}
			if smartData.Protocol != nil && data.Protocol == nil {
				data.Protocol = smartData.Protocol
			}
			// State and temp
			data.State = smartData.State
			data.Temp = smartData.Temp
			data.SmartHealth = smartData.SmartHealth
			data.PowerOnHours = smartData.PowerOnHours
			data.Reallocated = smartData.Reallocated
			data.PendingSectors = smartData.PendingSectors
		}
	} else {
		// We have serial from lsblk, just get state
		smartData := getSmartState(device)
		data.State = smartData.State
		data.Temp = smartData.Temp
	}

	// === Merge from HBA (by serial) ===
	if data.Serial != nil {
		serialUpper := strings.ToUpper(*data.Serial)
		if hba, ok := sysData.HBADevices[serialUpper]; ok {
			data.ControllerID = &hba.ControllerID
			data.Enclosure = &hba.EnclosureID
			data.Slot = &hba.Slot
			data.DeviceID = hba.DeviceID
			data.PhyNum = hba.PhyNum

			if hba.SASAddress != nil {
				data.SASAddress = hba.SASAddress
			}
			if hba.SerialVPD != nil {
				data.SerialVPD = hba.SerialVPD
			}
			if hba.WWN != nil && data.WWN == nil {
				data.WWN = hba.WWN
			}
			if hba.Model != nil && data.Model == nil {
				data.Model = hba.Model
			}
			if hba.Vendor != nil && data.Vendor == nil {
				data.Vendor = hba.Vendor
			}
			if hba.Firmware != nil && data.Firmware == nil {
				data.Firmware = hba.Firmware
			}
			if hba.SizeBytes != nil && data.SizeBytes == nil {
				data.SizeBytes = hba.SizeBytes
			}
			if hba.SectorSize != nil {
				data.SectorSize = hba.SectorSize
			}
			if hba.Protocol != nil && data.Protocol == nil {
				data.Protocol = hba.Protocol
			}
			if hba.MediaType != nil {
				data.DriveType = hba.MediaType
			}
			if hba.LinkSpeed != nil {
				data.LinkSpeed = hba.LinkSpeed
			}
			if hba.MediaErrors != nil {
				data.MediaErrors = hba.MediaErrors
			}
		}
	}

	// === Merge from blkid (check partitions) ===
	// Check main device and first partition
	for _, devPath := range []string{device, device + "1"} {
		if blkid, ok := sysData.BlkidDevices[devPath]; ok {
			if blkid.Type != nil {
				data.FSType = blkid.Type
			}
			if blkid.Label != nil {
				data.FSLabel = blkid.Label
			}
			if blkid.UUID != nil {
				data.FSUUID = blkid.UUID
			}
			if blkid.PartUUID != nil {
				data.PartUUID = blkid.PartUUID
			}
			if blkid.PartLabel != nil {
				data.PartLabel = blkid.PartLabel
			}

			// Check for ZFS membership via blkid UUID_SUB
			if blkid.Type != nil && *blkid.Type == "zfs_member" && blkid.UUIDSub != nil {
				// UUID_SUB is the vdev GUID
				if vdev, ok := sysData.ZpoolVdevs[*blkid.UUIDSub]; ok {
					data.Zpool = &vdev.PoolName
					if vdev.VdevType != "" {
						data.Vdev = &vdev.VdevType
					}
					data.VdevGUID = blkid.UUIDSub
					data.ZfsErrors = &ZfsErrors{
						Read:  vdev.ReadErrors,
						Write: vdev.WriteErrors,
						Cksum: vdev.CksumErrors,
					}
				} else if blkid.Label != nil {
					// Fallback: use label as pool name
					data.Zpool = blkid.Label
					data.VdevGUID = blkid.UUIDSub
				}
			}
			break // Only need first match
		}
	}

	// === Merge from LVM ===
	if pv, ok := sysData.LvmPVs[device]; ok {
		data.LvmPV = &pv.PVName
		data.LvmVG = pv.VGName
		data.LvmPVUUID = &pv.PVUUID
	}

	return data
}

// GetAllDriveData collects data for all drives
func GetAllDriveData(devices []string, forceRefresh bool) []*DriveData {
	sysData := CollectSystemData(forceRefresh)

	results := make([]*DriveData, len(devices))
	var wg sync.WaitGroup

	for i, dev := range devices {
		wg.Add(1)
		go func(idx int, device string) {
			defer wg.Done()
			results[idx] = GetDriveData(device, sysData)
		}(i, dev)
	}

	wg.Wait()
	return results
}

// smartInfo holds data extracted from smartctl
type smartInfo struct {
	Serial       *string
	WWN          *string
	LUID         *string
	Model        *string
	Vendor       *string
	Firmware     *string
	SizeBytes    *int64
	FormFactor   *string
	Protocol     *string
	State        string
	Temp         *int
	SmartHealth  *string
	PowerOnHours *int
	Reallocated  *int
	PendingSectors *int
}

// getSmartInfo gets comprehensive info from smartctl (used when serial not available from lsblk)
func getSmartInfo(device string) *smartInfo {
	c := cache.Global()
	cacheKey := "smart:info:" + device

	if cached := c.Get(cacheKey); cached != nil {
		return cached.(*smartInfo)
	}

	// Single smartctl call with all info
	out, err := exec.Command("smartctl", "-i", "-A", "-H", "-n", "standby", device).CombinedOutput()
	output := string(out)

	info := &smartInfo{State: "unknown"}

	// Check state first
	if strings.Contains(output, "STANDBY") || strings.Contains(output, "NOT READY") {
		info.State = "standby"
	} else if err != nil {
		if strings.Contains(output, "No such device") || strings.Contains(output, "No such file") {
			info.State = "missing"
		} else if strings.Contains(output, "I/O error") {
			info.State = "failed"
		} else {
			info.State = "failed"
		}
	} else {
		info.State = "active"
	}

	// Parse info section (works even in standby)
	patterns := map[string]func(string){
		`Serial [Nn]umber:\s+(\S+)`:        func(v string) { info.Serial = &v },
		`LU WWN Device Id:\s+(\S.+)`:       func(v string) { v = strings.ReplaceAll(v, " ", ""); info.WWN = &v },
		`Logical Unit id:\s+(\S+)`:         func(v string) { info.LUID = &v },
		`(?:Product|Device Model):\s+(.+)`: func(v string) { v = strings.TrimSpace(v); info.Model = &v },
		`Vendor:\s+(\S+)`:                  func(v string) { info.Vendor = &v },
		`(?:Revision|Firmware Version):\s+(\S+)`: func(v string) { info.Firmware = &v },
		`User Capacity:\s+([\d,]+)\s+bytes`: func(v string) {
			v = strings.ReplaceAll(v, ",", "")
			if size, err := strconv.ParseInt(v, 10, 64); err == nil {
				info.SizeBytes = &size
			}
		},
		`Form Factor:\s+(.+)`:              func(v string) { v = strings.TrimSpace(v); info.FormFactor = &v },
		`Transport protocol:\s+(\S+)`:      func(v string) { info.Protocol = &v },
		`Rotation Rate:\s+(\d+)`:           func(v string) {
			// If rotation rate is present and > 0, it's HDD
		},
	}

	for pattern, setter := range patterns {
		re := regexp.MustCompile(pattern)
		if matches := re.FindStringSubmatch(output); len(matches) > 1 {
			setter(matches[1])
		}
	}

	// Parse SMART health (if active)
	if info.State == "active" {
		if strings.Contains(output, "SMART Health Status: OK") ||
			strings.Contains(output, "SMART overall-health self-assessment test result: PASSED") {
			health := "PASSED"
			info.SmartHealth = &health
		} else if strings.Contains(output, "FAILED") {
			health := "FAILED"
			info.SmartHealth = &health
		}

		// Temperature
		tempPatterns := []string{
			`Current Drive Temperature:\s+(\d+)`,
			`Temperature_Celsius\s+\S+\s+(\d+)`,
			`Temperature:\s+(\d+)\s+Celsius`,
		}
		for _, pattern := range tempPatterns {
			re := regexp.MustCompile(pattern)
			if matches := re.FindStringSubmatch(output); len(matches) > 1 {
				if temp, err := strconv.Atoi(matches[1]); err == nil {
					info.Temp = &temp
					break
				}
			}
		}

		// SMART attributes
		if re := regexp.MustCompile(`Power_On_Hours\s+\S+\s+\S+\s+\S+\s+\S+\s+\S+\s+\S+\s+\S+\s+\S+\s+(\d+)`); true {
			if matches := re.FindStringSubmatch(output); len(matches) > 1 {
				if hours, err := strconv.Atoi(matches[1]); err == nil {
					info.PowerOnHours = &hours
				}
			}
		}
		// Simpler pattern for SCSI drives
		if re := regexp.MustCompile(`Accumulated power on time[^:]*:\s+(\d+)`); info.PowerOnHours == nil {
			if matches := re.FindStringSubmatch(output); len(matches) > 1 {
				if hours, err := strconv.Atoi(matches[1]); err == nil {
					info.PowerOnHours = &hours
				}
			}
		}

		if re := regexp.MustCompile(`Reallocated_Sector_Ct\s+\S+\s+\S+\s+\S+\s+\S+\s+\S+\s+\S+\s+\S+\s+\S+\s+(\d+)`); true {
			if matches := re.FindStringSubmatch(output); len(matches) > 1 {
				if count, err := strconv.Atoi(matches[1]); err == nil && count > 0 {
					info.Reallocated = &count
				}
			}
		}

		if re := regexp.MustCompile(`Current_Pending_Sector\s+\S+\s+\S+\s+\S+\s+\S+\s+\S+\s+\S+\s+\S+\s+\S+\s+(\d+)`); true {
			if matches := re.FindStringSubmatch(output); len(matches) > 1 {
				if count, err := strconv.Atoi(matches[1]); err == nil && count > 0 {
					info.PendingSectors = &count
				}
			}
		}
	}

	// Cache based on state
	if info.State == "active" {
		c.SetFast(cacheKey, info)
	} else {
		c.SetSlow(cacheKey, info) // Static info doesn't change in standby
	}

	return info
}

// getSmartState does a quick state check (used when we already have serial from lsblk)
func getSmartState(device string) *smartInfo {
	c := cache.Global()
	cacheKey := "smart:state:" + device

	if cached := c.Get(cacheKey); cached != nil {
		return cached.(*smartInfo)
	}

	out, err := exec.Command("smartctl", "-i", "-n", "standby", device).CombinedOutput()
	output := string(out)

	info := &smartInfo{State: "unknown"}

	// Check state
	if strings.Contains(output, "STANDBY") || strings.Contains(output, "NOT READY") {
		info.State = "standby"
	} else if err != nil {
		if strings.Contains(output, "No such device") || strings.Contains(output, "No such file") {
			info.State = "missing"
		} else if strings.Contains(output, "I/O error") {
			info.State = "failed"
		} else {
			info.State = "failed"
		}
	} else {
		info.State = "active"

		// Get temp for active drives
		tempOut, _ := exec.Command("smartctl", "-A", device).CombinedOutput()
		tempPatterns := []string{
			`Current Drive Temperature:\s+(\d+)`,
			`Temperature_Celsius\s+\S+\s+(\d+)`,
		}
		for _, pattern := range tempPatterns {
			re := regexp.MustCompile(pattern)
			if matches := re.FindStringSubmatch(string(tempOut)); len(matches) > 1 {
				if temp, err := strconv.Atoi(matches[1]); err == nil {
					info.Temp = &temp
					break
				}
			}
		}
	}

	c.SetFast(cacheKey, info)
	return info
}
