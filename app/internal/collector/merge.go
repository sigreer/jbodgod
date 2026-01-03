package collector

import (
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/sigreer/jbodgod/internal/cache"
)

// GetDriveData collects comprehensive data for a single drive using layered approach
// Layer 1: sysfs + udev (no wake, no process spawn)
// Layer 2: lsblk/lsscsi (cached, no wake)
// Layer 3: zpool/lvm (cached in memory, no wake)
// Layer 4: smartctl (only for active drives, gated on state)
// Layer 5: HBA data (cached 24h)
func GetDriveData(device string, sysData *SystemData) *DriveData {
	data := &DriveData{
		Device: device,
		State:  "unknown",
	}

	devName := strings.TrimPrefix(device, "/dev/")

	// === Layer 1: sysfs (fastest, no wake, no process spawn) ===
	if sysfs, ok := sysData.SysfsDevices[devName]; ok {
		mergeSysfsData(data, sysfs)
	}

	// === Layer 1b: udev database (fast, no wake, no process spawn) ===
	if udev, ok := sysData.UdevDevices[devName]; ok {
		mergeUdevData(data, udev)
	}

	// === Layer 2: lsblk (cached, no wake) ===
	if lsblk, ok := sysData.LsblkDevices[devName]; ok {
		mergeLsblkData(data, lsblk)
	}

	// === Layer 2b: lsscsi (cached, no wake) ===
	if lsscsi, ok := sysData.LsscsiDevices[device]; ok {
		mergeLsscsiData(data, lsscsi)
	}

	// === Layer 2c: by-id symlinks (no wake) ===
	if byID, ok := sysData.ByIDLinks[device]; ok {
		data.ByIDPath = &byID
	}

	// === Determine device state from sysfs (no smartctl needed for basic state) ===
	// sysfs state: "running", "offline", "blocked", "quiesce", etc.
	// Map to our states: active, standby, failed, missing
	deviceState := determineStateFromSysfs(data)
	data.State = deviceState

	// === Layer 3: Storage stack (ZFS/LVM from cached metadata, no wake) ===
	// Only merge if device appears present (not missing)
	if deviceState != "missing" {
		mergeZFSData(data, devName, sysData)
		mergeLVMData(data, device, sysData)
	}

	// === Layer 4: smartctl (state detection + SMART data for active drives) ===
	// This is the only layer that might access the drive
	if deviceState == "active" {
		// Device is active, safe to query SMART data
		mergeSmartData(data, device)
	} else if deviceState == "unknown" {
		// State unknown - use smartctl -n standby to determine state without waking
		smartData := getSmartStateOnly(device)
		data.State = smartData.State
		// Only get more data if drive is active
		if smartData.State == "active" {
			mergeSmartData(data, device)
		}
	}
	// For standby/failed/missing: DO NOT call smartctl - would wake the drive

	// === Layer 5: HBA data (cached 24h) ===
	if data.Serial != nil {
		mergeHBAData(data, *data.Serial, sysData)
	}

	// === Layer 5b: Enclosure from sysfs (no HBA tool needed) ===
	if data.Enclosure == nil && sysData.SysfsDevices != nil {
		if sysfs, ok := sysData.SysfsDevices[devName]; ok {
			if sysfs.EnclosureID != nil {
				data.ControllerID = sysfs.EnclosureID // Use enclosure HCTL as controller ref
			}
			if sysfs.Slot != nil {
				data.Slot = sysfs.Slot
			}
		}
	}

	return data
}

// determineStateFromSysfs maps sysfs device state to our state model
func determineStateFromSysfs(data *DriveData) string {
	// If we have a sysfs state, use it
	if data.State != "" && data.State != "unknown" {
		return data.State
	}

	// If we have basic info from sysfs, device exists
	// The actual running/standby detection needs smartctl -n standby
	// but we can at least know the device is present
	return "unknown"
}

// mergeSysfsData merges data from sysfs into DriveData
func mergeSysfsData(data *DriveData, sysfs *SysfsDevice) {
	if sysfs.Serial != nil && data.Serial == nil {
		data.Serial = sysfs.Serial
	}
	if sysfs.WWN != nil && data.WWN == nil {
		data.WWN = sysfs.WWN
	}
	if sysfs.SASAddress != nil && data.SASAddress == nil {
		data.SASAddress = sysfs.SASAddress
	}
	if sysfs.Model != nil && data.Model == nil {
		data.Model = sysfs.Model
	}
	if sysfs.Vendor != nil && data.Vendor == nil {
		data.Vendor = sysfs.Vendor
	}
	if sysfs.Size != nil && data.SizeBytes == nil {
		// sysfs size is in 512-byte sectors
		sizeBytes := *sysfs.Size * 512
		data.SizeBytes = &sizeBytes
	}
	if sysfs.HCTL != nil && data.SCSIAddr == nil {
		data.SCSIAddr = sysfs.HCTL
	}
	if sysfs.Slot != nil && data.Slot == nil {
		data.Slot = sysfs.Slot
	}
	if sysfs.EnclosureID != nil {
		data.ControllerID = sysfs.EnclosureID
	}

	// Map sysfs state to our state model
	if sysfs.State != nil {
		switch *sysfs.State {
		case "running":
			// Could be active or standby - need smartctl to distinguish
			data.State = "unknown"
		case "offline", "blocked":
			data.State = "failed"
		default:
			data.State = "unknown"
		}
	}
}

// mergeUdevData merges data from udev database
func mergeUdevData(data *DriveData, udev *UdevDevice) {
	if udev.IDSCSISerial != "" && data.Serial == nil {
		data.Serial = &udev.IDSCSISerial
	}
	if udev.IDWWN != "" && data.WWN == nil {
		wwn := strings.TrimPrefix(udev.IDWWN, "0x")
		data.WWN = &wwn
	}
	if udev.IDModel != "" && data.Model == nil {
		data.Model = &udev.IDModel
	}
	if udev.IDVendor != "" && data.Vendor == nil {
		data.Vendor = &udev.IDVendor
	}
	if udev.IDRevision != "" && data.Firmware == nil {
		data.Firmware = &udev.IDRevision
	}
	if udev.IDBus != "" && data.Protocol == nil {
		// Map bus type to protocol
		switch udev.IDBus {
		case "scsi":
			proto := "SAS" // Assume SAS for SCSI bus
			data.Protocol = &proto
		case "ata":
			proto := "SATA"
			data.Protocol = &proto
		}
	}
}

// mergeLsblkData merges data from lsblk
func mergeLsblkData(data *DriveData, lsblk *LsblkDevice) {
	if lsblk.Serial != nil && data.Serial == nil {
		data.Serial = lsblk.Serial
	}
	if lsblk.WWN != nil && data.WWN == nil {
		data.WWN = lsblk.WWN
	}
	if lsblk.Model != nil && data.Model == nil {
		data.Model = lsblk.Model
	}
	if lsblk.Vendor != nil && data.Vendor == nil {
		data.Vendor = lsblk.Vendor
	}
	if lsblk.Rev != nil && data.Firmware == nil {
		data.Firmware = lsblk.Rev
	}
	if lsblk.Size != nil && data.SizeBytes == nil {
		data.SizeBytes = lsblk.Size
	}
	if lsblk.Tran != nil && data.Protocol == nil {
		data.Protocol = lsblk.Tran
	}
	if lsblk.HCTL != nil && data.SCSIAddr == nil {
		data.SCSIAddr = lsblk.HCTL
	}
	// FS info from lsblk (no blkid needed!)
	if lsblk.FSType != nil {
		data.FSType = lsblk.FSType
	}
	if lsblk.UUID != nil {
		data.FSUUID = lsblk.UUID
	}
	if lsblk.Label != nil {
		data.FSLabel = lsblk.Label
	}
	if lsblk.PartUUID != nil {
		data.PartUUID = lsblk.PartUUID
	}
	if lsblk.PartLabel != nil {
		data.PartLabel = lsblk.PartLabel
	}
}

// mergeLsscsiData merges data from lsscsi
func mergeLsscsiData(data *DriveData, lsscsi *LsscsiDevice) {
	if data.SCSIAddr == nil {
		data.SCSIAddr = &lsscsi.HCTL
	}
	if lsscsi.Vendor != nil && data.Vendor == nil {
		data.Vendor = lsscsi.Vendor
	}
	if lsscsi.Model != nil && data.Model == nil {
		data.Model = lsscsi.Model
	}
	if lsscsi.Rev != nil && data.Firmware == nil {
		data.Firmware = lsscsi.Rev
	}
}

// mergeZFSData merges ZFS pool membership from zpool status
// Uses vdev GUID matching against imported pools only
func mergeZFSData(data *DriveData, devName string, sysData *SystemData) {
	// Try to find this device in zpool vdevs
	// zpool status -gLP shows device paths, we can match
	for _, vdev := range sysData.ZpoolVdevs {
		if vdev.DevicePath != nil {
			vdevDev := strings.TrimPrefix(*vdev.DevicePath, "/dev/")
			// Handle partition suffix (e.g., sda1 -> sda)
			vdevDev = strings.TrimRight(vdevDev, "0123456789")
			if vdevDev == devName {
				data.Zpool = &vdev.PoolName
				if vdev.VdevType != "" {
					data.Vdev = &vdev.VdevType
				}
				data.VdevGUID = &vdev.VdevGUID
				data.ZfsErrors = &ZfsErrors{
					Read:  vdev.ReadErrors,
					Write: vdev.WriteErrors,
					Cksum: vdev.CksumErrors,
				}
				break
			}
		}
	}
}

// mergeLVMData merges LVM PV membership
func mergeLVMData(data *DriveData, device string, sysData *SystemData) {
	if pv, ok := sysData.LvmPVs[device]; ok {
		data.LvmPV = &pv.PVName
		data.LvmVG = pv.VGName
		data.LvmPVUUID = &pv.PVUUID
	}
}

// mergeSmartData gets SMART data for an active drive
func mergeSmartData(data *DriveData, device string) {
	smartData := getSmartInfo(device)
	if smartData == nil {
		return
	}

	data.State = smartData.State
	data.Temp = smartData.Temp
	data.SmartHealth = smartData.SmartHealth
	data.PowerOnHours = smartData.PowerOnHours
	data.Reallocated = smartData.Reallocated
	data.PendingSectors = smartData.PendingSectors

	// Fill in any missing identity data
	if smartData.Serial != nil && data.Serial == nil {
		data.Serial = smartData.Serial
	}
	if smartData.LUID != nil {
		data.LUID = smartData.LUID
	}
	if smartData.WWN != nil && data.WWN == nil {
		data.WWN = smartData.WWN
	}
	if smartData.FormFactor != nil {
		data.FormFactor = smartData.FormFactor
	}
}

// mergeHBAData merges HBA controller data (cached 24h)
func mergeHBAData(data *DriveData, serial string, sysData *SystemData) {
	serialUpper := strings.ToUpper(serial)
	hba, ok := sysData.HBADevices[serialUpper]
	if !ok {
		return
	}

	if data.ControllerID == nil {
		data.ControllerID = &hba.ControllerID
	}
	if data.Enclosure == nil {
		data.Enclosure = &hba.EnclosureID
	}
	if data.Slot == nil {
		data.Slot = &hba.Slot
	}
	data.DeviceID = hba.DeviceID
	data.PhyNum = hba.PhyNum

	if hba.SASAddress != nil && data.SASAddress == nil {
		data.SASAddress = hba.SASAddress
	}
	if hba.SerialVPD != nil {
		data.SerialVPD = hba.SerialVPD
	}
	if hba.WWN != nil && data.WWN == nil {
		data.WWN = hba.WWN
	}
	if hba.SectorSize != nil {
		data.SectorSize = hba.SectorSize
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
	Serial         *string
	WWN            *string
	LUID           *string
	Model          *string
	Vendor         *string
	Firmware       *string
	SizeBytes      *int64
	FormFactor     *string
	Protocol       *string
	State          string
	Temp           *int
	SmartHealth    *string
	PowerOnHours   *int
	Reallocated    *int
	PendingSectors *int
}

// getSmartStateOnly does minimal smartctl probe to determine state without waking standby drives
func getSmartStateOnly(device string) *smartInfo {
	c := cache.Global()
	cacheKey := "smart:state:" + device

	if cached := c.Get(cacheKey); cached != nil {
		return cached.(*smartInfo)
	}

	// Use -n standby to check state without waking
	out, err := exec.Command("smartctl", "-i", "-n", "standby", device).CombinedOutput()
	output := string(out)

	info := &smartInfo{State: "unknown"}

	// Check for standby FIRST
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

	c.SetFast(cacheKey, info)
	return info
}

// getSmartInfo gets comprehensive info from smartctl (only call for active drives!)
func getSmartInfo(device string) *smartInfo {
	c := cache.Global()
	cacheKey := "smart:info:" + device

	if cached := c.Get(cacheKey); cached != nil {
		return cached.(*smartInfo)
	}

	// Full smartctl call - only for active drives
	out, err := exec.Command("smartctl", "-i", "-A", "-H", device).CombinedOutput()
	output := string(out)

	info := &smartInfo{State: "active"}

	if err != nil {
		// Device might have gone to standby between state check and this call
		if strings.Contains(output, "STANDBY") || strings.Contains(output, "NOT READY") {
			info.State = "standby"
			c.SetFast(cacheKey, info)
			return info
		}
		info.State = "failed"
		c.SetFast(cacheKey, info)
		return info
	}

	// Parse info section
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
		`Form Factor:\s+(.+)`:         func(v string) { v = strings.TrimSpace(v); info.FormFactor = &v },
		`Transport protocol:\s+(\S+)`: func(v string) { info.Protocol = &v },
	}

	for pattern, setter := range patterns {
		re := regexp.MustCompile(pattern)
		if matches := re.FindStringSubmatch(output); len(matches) > 1 {
			setter(matches[1])
		}
	}

	// Parse SMART health
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

	// Power on hours
	pohPatterns := []string{
		`Power_On_Hours\s+\S+\s+\S+\s+\S+\s+\S+\s+\S+\s+\S+\s+\S+\s+\S+\s+(\d+)`,
		`Accumulated power on time[^:]*:\s+(\d+)`,
	}
	for _, pattern := range pohPatterns {
		re := regexp.MustCompile(pattern)
		if matches := re.FindStringSubmatch(output); len(matches) > 1 {
			if hours, err := strconv.Atoi(matches[1]); err == nil {
				info.PowerOnHours = &hours
				break
			}
		}
	}

	// Reallocated sectors
	re := regexp.MustCompile(`Reallocated_Sector_Ct\s+\S+\s+\S+\s+\S+\s+\S+\s+\S+\s+\S+\s+\S+\s+\S+\s+(\d+)`)
	if matches := re.FindStringSubmatch(output); len(matches) > 1 {
		if count, err := strconv.Atoi(matches[1]); err == nil && count > 0 {
			info.Reallocated = &count
		}
	}

	// Pending sectors
	re = regexp.MustCompile(`Current_Pending_Sector\s+\S+\s+\S+\s+\S+\s+\S+\s+\S+\s+\S+\s+\S+\s+\S+\s+(\d+)`)
	if matches := re.FindStringSubmatch(output); len(matches) > 1 {
		if count, err := strconv.Atoi(matches[1]); err == nil && count > 0 {
			info.PendingSectors = &count
		}
	}

	c.SetDynamic(cacheKey, info)
	return info
}
