package drive

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sigreer/jbodgod/internal/cache"
	"github.com/sigreer/jbodgod/internal/collector"
	"github.com/sigreer/jbodgod/internal/config"
	"github.com/sigreer/jbodgod/internal/db"
	"github.com/sigreer/jbodgod/internal/hba"
	"github.com/sigreer/jbodgod/internal/zfs"
)

// DriveInfo represents comprehensive drive information
type DriveInfo struct {
	// === Identifiers ===
	Device     string  `json:"device"`
	Name       string  `json:"name,omitempty"`
	Serial     *string `json:"serial,omitempty"`
	SerialVPD  *string `json:"serial_vpd,omitempty"`
	WWN        *string `json:"wwn,omitempty"`
	LUID       *string `json:"luid,omitempty"`
	SASAddress *string `json:"sas_address,omitempty"`
	ByIDPath   *string `json:"by_id_path,omitempty"`

	// === Hardware ===
	Model      *string `json:"model,omitempty"`
	Vendor     *string `json:"vendor,omitempty"`
	Firmware   *string `json:"firmware,omitempty"`
	SizeBytes  *int64  `json:"size_bytes,omitempty"`
	Protocol   *string `json:"protocol,omitempty"`
	DriveType  *string `json:"drive_type,omitempty"`
	FormFactor *string `json:"form_factor,omitempty"`
	SectorSize *int    `json:"sector_size,omitempty"`
	LinkSpeed  *string `json:"link_speed,omitempty"`

	// === Physical Location ===
	ControllerID *string `json:"controller_id,omitempty"`
	Enclosure    *int    `json:"enclosure,omitempty"`
	Slot         *int    `json:"slot,omitempty"`
	SCSIAddr     *string `json:"scsi_addr,omitempty"`

	// === Runtime State ===
	State       string  `json:"state"`
	Temp        *int    `json:"temp,omitempty"`
	SmartHealth *string `json:"smart_health,omitempty"`

	// === Storage Stack ===
	Zpool     *string           `json:"zpool,omitempty"`
	Vdev      *string           `json:"vdev,omitempty"`
	VdevGUID  *string           `json:"vdev_guid,omitempty"`
	ZfsErrors *collector.ZfsErrors `json:"zfs_errors,omitempty"`
	LvmPV     *string           `json:"lvm_pv,omitempty"`
	LvmVG     *string           `json:"lvm_vg,omitempty"`

	// === Filesystem ===
	FSType    *string `json:"fs_type,omitempty"`
	FSLabel   *string `json:"fs_label,omitempty"`
	FSUUID    *string `json:"fs_uuid,omitempty"`
	PartUUID  *string `json:"part_uuid,omitempty"`
	PartLabel *string `json:"part_label,omitempty"`

	// === SMART Metrics ===
	PowerOnHours   *int `json:"power_on_hours,omitempty"`
	Reallocated    *int `json:"reallocated_sectors,omitempty"`
	PendingSectors *int `json:"pending_sectors,omitempty"`
	MediaErrors    *int `json:"media_errors,omitempty"`
}

type Summary struct {
	Active  int  `json:"active"`
	Standby int  `json:"standby"`
	Missing int  `json:"missing"`
	Failed  int  `json:"failed"`
	TempMin *int `json:"temp_min,omitempty"`
	TempMax *int `json:"temp_max,omitempty"`
	TempAvg *int `json:"temp_avg,omitempty"`
}

// CoreDriveInfo contains essential realtime data (default output)
type CoreDriveInfo struct {
	Device  string  `json:"device"`
	Name    string  `json:"name,omitempty"`
	State   string  `json:"state"`
	Temp    *int    `json:"temp,omitempty"`
	Zpool   *string `json:"zpool,omitempty"`
	Slot    string  `json:"slot,omitempty"` // formatted as "enc:slot"
}

// CoreOutput is the default output structure (realtime/essential data only)
type CoreOutput struct {
	Drives  []CoreDriveInfo `json:"drives"`
	Summary Summary         `json:"summary"`
}

// DetailOutput includes full drive data plus controllers/enclosures
type DetailOutput struct {
	Drives      []DriveInfo          `json:"drives"`
	Summary     Summary              `json:"summary"`
	Controllers []hba.ControllerInfo `json:"controllers,omitempty"`
	Enclosures  []hba.EnclosureInfo  `json:"enclosures,omitempty"`
}

// Output is an alias for DetailOutput for backwards compatibility
type Output = DetailOutput

func GetAll(cfg *config.Config) []DriveInfo {
	drives := cfg.GetAllDrives()

	// Collect device paths
	devices := make([]string, len(drives))
	nameMap := make(map[string]string) // device -> name
	for i, d := range drives {
		devices[i] = d.Device
		nameMap[d.Device] = d.Name
	}

	// Use new collector for bulk data collection
	driveData := collector.GetAllDriveData(devices, false)

	// Convert to DriveInfo
	results := make([]DriveInfo, len(driveData))
	for i, data := range driveData {
		results[i] = driveDataToInfo(data, nameMap[data.Device])
	}

	return results
}

// driveDataToInfo converts collector.DriveData to DriveInfo
func driveDataToInfo(data *collector.DriveData, name string) DriveInfo {
	info := DriveInfo{
		Device:         data.Device,
		Name:           name,
		Serial:         data.Serial,
		SerialVPD:      data.SerialVPD,
		WWN:            data.WWN,
		LUID:           data.LUID,
		SASAddress:     data.SASAddress,
		ByIDPath:       data.ByIDPath,
		Model:          data.Model,
		Vendor:         data.Vendor,
		Firmware:       data.Firmware,
		SizeBytes:      data.SizeBytes,
		Protocol:       data.Protocol,
		DriveType:      data.DriveType,
		FormFactor:     data.FormFactor,
		SectorSize:     data.SectorSize,
		LinkSpeed:      data.LinkSpeed,
		ControllerID:   data.ControllerID,
		Enclosure:      data.Enclosure,
		Slot:           data.Slot,
		SCSIAddr:       data.SCSIAddr,
		State:          data.State,
		Temp:           data.Temp,
		SmartHealth:    data.SmartHealth,
		Zpool:          data.Zpool,
		Vdev:           data.Vdev,
		VdevGUID:       data.VdevGUID,
		ZfsErrors:      data.ZfsErrors,
		LvmPV:          data.LvmPV,
		LvmVG:          data.LvmVG,
		FSType:         data.FSType,
		FSLabel:        data.FSLabel,
		FSUUID:         data.FSUUID,
		PartUUID:       data.PartUUID,
		PartLabel:      data.PartLabel,
		PowerOnHours:   data.PowerOnHours,
		Reallocated:    data.Reallocated,
		PendingSectors: data.PendingSectors,
		MediaErrors:    data.MediaErrors,
	}
	return info
}

func getInfo(d config.Drive) DriveInfo {
	info := DriveInfo{
		Device: d.Device,
		Name:   d.Name,
	}

	// Check state
	out, err := exec.Command("smartctl", "-i", "-n", "standby", d.Device).CombinedOutput()
	output := string(out)

	// Check for standby FIRST - smartctl returns non-zero exit code for standby drives
	// but this is not an error condition
	if strings.Contains(output, "STANDBY") || strings.Contains(output, "NOT READY") {
		info.State = "standby"
		return info
	}

	// Check for device access failures
	if err != nil {
		// Device doesn't exist or can't be opened
		if strings.Contains(output, "No such device") ||
			strings.Contains(output, "No such file") {
			info.State = "missing"
			return info
		}
		// Device exists but failed to respond (I/O error, etc.)
		if strings.Contains(output, "I/O error") {
			info.State = "failed"
			return info
		}
		// Other errors - mark as failed
		info.State = "failed"
		return info
	}

	info.State = "active"

	// Get SMART attributes
	smartOut, _ := exec.Command("smartctl", "-A", d.Device).CombinedOutput()
	smartStr := string(smartOut)

	// Temperature
	re := regexp.MustCompile(`Current Drive Temperature:\s+(\d+)`)
	if matches := re.FindStringSubmatch(smartStr); len(matches) > 1 {
		if temp, err := strconv.Atoi(matches[1]); err == nil {
			info.Temp = &temp
		}
	}

	// Get info
	infoOut, _ := exec.Command("smartctl", "-i", d.Device).CombinedOutput()
	infoStr := string(infoOut)

	// Serial
	re = regexp.MustCompile(`Serial number:\s+(\S+)`)
	if matches := re.FindStringSubmatch(infoStr); len(matches) > 1 {
		info.Serial = &matches[1]
	}

	// LUID
	re = regexp.MustCompile(`Logical Unit id:\s+(\S+)`)
	if matches := re.FindStringSubmatch(infoStr); len(matches) > 1 {
		info.LUID = &matches[1]
	}

	// SCSI address
	lsscsiOut, _ := exec.Command("lsscsi").CombinedOutput()
	deviceName := strings.TrimPrefix(d.Device, "/dev/")
	re = regexp.MustCompile(`\[([^\]]+)\].*` + deviceName + `\s*$`)
	for _, line := range strings.Split(string(lsscsiOut), "\n") {
		if matches := re.FindStringSubmatch(line); len(matches) > 1 {
			info.SCSIAddr = &matches[1]
			break
		}
	}

	// Model
	lsblkOut, _ := exec.Command("lsblk", "-d", "-o", "MODEL", d.Device).CombinedOutput()
	lines := strings.Split(strings.TrimSpace(string(lsblkOut)), "\n")
	if len(lines) > 1 {
		model := strings.TrimSpace(lines[1])
		if model != "" {
			info.Model = &model
		}
	}

	// Zpool info
	pool, vdev := getZpoolInfo(deviceName)
	if pool != "" {
		info.Zpool = &pool
	}
	if vdev != "" {
		info.Vdev = &vdev
	}

	return info
}

func getZpoolInfo(device string) (pool, vdev string) {
	out, err := exec.Command("zpool", "status", "-L").CombinedOutput()
	if err != nil {
		return "", ""
	}

	lines := strings.Split(string(out), "\n")
	var currentPool, currentVdev string

	for _, line := range lines {
		if strings.HasPrefix(line, "  pool:") {
			currentPool = strings.TrimSpace(strings.TrimPrefix(line, "  pool:"))
			currentVdev = ""
		} else if strings.Contains(line, "raidz") || strings.Contains(line, "mirror") {
			fields := strings.Fields(line)
			if len(fields) > 0 {
				currentVdev = fields[0]
			}
		} else if strings.Contains(line, device) {
			return currentPool, currentVdev
		}
	}

	return "", ""
}

// BuildSummary calculates summary statistics from drive data
func BuildSummary(drives []DriveInfo) Summary {
	var active, standby, missing, failed int
	var temps []int

	for _, d := range drives {
		switch d.State {
		case "active":
			active++
			if d.Temp != nil {
				temps = append(temps, *d.Temp)
			}
		case "standby":
			standby++
		case "missing":
			missing++
		case "failed":
			failed++
		default:
			failed++
		}
	}

	summary := Summary{
		Active:  active,
		Standby: standby,
		Missing: missing,
		Failed:  failed,
	}

	if len(temps) > 0 {
		min, max, sum := temps[0], temps[0], 0
		for _, t := range temps {
			if t < min {
				min = t
			}
			if t > max {
				max = t
			}
			sum += t
		}
		avg := sum / len(temps)
		summary.TempMin = &min
		summary.TempMax = &max
		summary.TempAvg = &avg
	}

	return summary
}

// DriveInfoToCore converts full DriveInfo to core (essential) data
func DriveInfoToCore(d DriveInfo) CoreDriveInfo {
	core := CoreDriveInfo{
		Device: d.Device,
		Name:   d.Name,
		State:  d.State,
		Temp:   d.Temp,
		Zpool:  d.Zpool,
	}
	if d.Enclosure != nil && d.Slot != nil {
		core.Slot = fmt.Sprintf("%d:%d", *d.Enclosure, *d.Slot)
	}
	return core
}

// PrintStatus prints drive status in table format
// If detail is true, shows additional columns (model, serial, etc.)
func PrintStatus(drives []DriveInfo, detail bool) {
	if detail {
		printDetailTable(drives)
	} else {
		printCoreTable(drives)
	}

	// Print summary
	summary := BuildSummary(drives)
	fmt.Println()
	printSummary(summary)
}

func printCoreTable(drives []DriveInfo) {
	fmt.Printf("%-10s %-8s %-10s %-6s %-12s\n", "DEVICE", "SLOT", "STATE", "TEMP", "ZPOOL")
	fmt.Println(strings.Repeat("-", 52))

	for _, d := range drives {
		slot := "-"
		if d.Enclosure != nil && d.Slot != nil {
			slot = fmt.Sprintf("%d:%d", *d.Enclosure, *d.Slot)
		}
		temp := "-"
		if d.Temp != nil {
			temp = fmt.Sprintf("%d°C", *d.Temp)
		}
		zpool := "-"
		if d.Zpool != nil {
			zpool = *d.Zpool
		}
		fmt.Printf("%-10s %-8s %-10s %-6s %-12s\n",
			d.Device, slot, strings.ToUpper(d.State), temp, zpool)
	}
}

func printDetailTable(drives []DriveInfo) {
	fmt.Printf("%-10s %-8s %-10s %-6s %-12s %-20s %-15s\n",
		"DEVICE", "SLOT", "STATE", "TEMP", "ZPOOL", "MODEL", "SERIAL")
	fmt.Println(strings.Repeat("-", 90))

	for _, d := range drives {
		slot := "-"
		if d.Enclosure != nil && d.Slot != nil {
			slot = fmt.Sprintf("%d:%d", *d.Enclosure, *d.Slot)
		}
		temp := "-"
		if d.Temp != nil {
			temp = fmt.Sprintf("%d°C", *d.Temp)
		}
		zpool := "-"
		if d.Zpool != nil {
			zpool = *d.Zpool
		}
		model := "-"
		if d.Model != nil {
			model = truncate(*d.Model, 18)
		}
		serial := "-"
		if d.Serial != nil {
			serial = truncate(*d.Serial, 13)
		}
		fmt.Printf("%-10s %-8s %-10s %-6s %-12s %-20s %-15s\n",
			d.Device, slot, strings.ToUpper(d.State), temp, zpool, model, serial)
	}
}

func printSummary(summary Summary) {
	parts := []string{
		fmt.Sprintf("Active: %d", summary.Active),
		fmt.Sprintf("Standby: %d", summary.Standby),
	}
	if summary.Missing > 0 {
		parts = append(parts, fmt.Sprintf("Missing: %d", summary.Missing))
	}
	if summary.Failed > 0 {
		parts = append(parts, fmt.Sprintf("Failed: %d", summary.Failed))
	}
	fmt.Println(strings.Join(parts, " | "))

	if summary.TempMin != nil && summary.TempMax != nil && summary.TempAvg != nil {
		fmt.Printf("Temps: Min %d°C | Max %d°C | Avg %d°C\n",
			*summary.TempMin, *summary.TempMax, *summary.TempAvg)
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-2] + ".."
}

// PrintJSON outputs drive data as JSON
// If detail is true, includes full DriveInfo plus controllers/enclosures
// If detail is false, outputs only core data
func PrintJSON(drives []DriveInfo, controllers []hba.ControllerInfo, enclosures []hba.EnclosureInfo, detail bool) {
	summary := BuildSummary(drives)

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")

	if detail {
		output := DetailOutput{
			Drives:      drives,
			Summary:     summary,
			Controllers: controllers,
			Enclosures:  enclosures,
		}
		enc.Encode(output)
	} else {
		coreDrives := make([]CoreDriveInfo, len(drives))
		for i, d := range drives {
			coreDrives[i] = DriveInfoToCore(d)
		}
		output := CoreOutput{
			Drives:  coreDrives,
			Summary: summary,
		}
		enc.Encode(output)
	}
}

// filterDrivesByController returns only drives attached to the specified controller.
// If controller is empty, returns all drives.
// Uses serial number matching between smartctl output and HBA device data.
func filterDrivesByController(drives []config.Drive, controller string) []config.Drive {
	if controller == "" {
		return drives
	}

	// Get controller number from ID (e.g., "c0" -> 0)
	ctrlNum := 0
	if strings.HasPrefix(controller, "c") {
		ctrlNum, _ = strconv.Atoi(controller[1:])
	}

	// Fetch devices from this controller
	_, _, hbaDevices, err := hba.FetchSas3ircuData(ctrlNum, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not fetch HBA data for %s: %v\n", controller, err)
		return nil
	}

	// Build a set of serials attached to this controller
	hbaSerials := make(map[string]bool)
	for _, dev := range hbaDevices {
		if dev.Serial != "" {
			hbaSerials[strings.ToUpper(dev.Serial)] = true
		}
		if dev.SerialVPD != "" {
			hbaSerials[strings.ToUpper(dev.SerialVPD)] = true
		}
	}

	// Match config drives to HBA devices by serial
	var filtered []config.Drive
	for _, d := range drives {
		serial := getSerialForDevice(d.Device)
		if serial != "" && hbaSerials[strings.ToUpper(serial)] {
			filtered = append(filtered, d)
		}
	}

	return filtered
}

func Spindown(cfg *config.Config, controller string, devices []string) {
	var drives []config.Drive

	if len(devices) > 0 {
		// Specific devices provided - use those directly
		for _, dev := range devices {
			drives = append(drives, config.Drive{Device: dev, Name: dev})
		}
		fmt.Printf("Spinning down %d specified drive(s)...\n", len(drives))
	} else {
		// Filter by controller
		allDrives := cfg.GetAllDrives()
		drives = filterDrivesByController(allDrives, controller)

		if len(drives) == 0 {
			if controller != "" {
				fmt.Printf("No drives found for controller %s\n", controller)
			} else {
				fmt.Println("No drives found")
			}
			return
		}

		if controller != "" {
			fmt.Printf("Spinning down %d drives on controller %s...\n", len(drives), controller)
		} else {
			fmt.Printf("Spinning down %d drives...\n", len(drives))
		}
	}

	// Use the common spindown logic
	spindownDrives(drives)
}

func Spinup(cfg *config.Config, controller string, devices []string) {
	var drives []config.Drive

	if len(devices) > 0 {
		// Specific devices provided - use those directly
		for _, dev := range devices {
			drives = append(drives, config.Drive{Device: dev, Name: dev})
		}
		fmt.Printf("Spinning up %d specified drive(s)...\n", len(drives))
	} else {
		// Filter by controller
		allDrives := cfg.GetAllDrives()
		drives = filterDrivesByController(allDrives, controller)

		if len(drives) == 0 {
			if controller != "" {
				fmt.Printf("No drives found for controller %s\n", controller)
			} else {
				fmt.Println("No drives found")
			}
			return
		}

		if controller != "" {
			fmt.Printf("Spinning up %d drives on controller %s...\n", len(drives), controller)
		} else {
			fmt.Printf("Spinning up %d drives...\n", len(drives))
		}
	}

	// Use the common spinup logic
	spinupDrives(drives)
}

// SpindownOptions controls spindown behavior
type SpindownOptions struct {
	Force    bool // Skip all ZFS handling
	ForceAll bool // Export all pools without prompts
}

// SpinupOptions controls spinup behavior
type SpinupOptions struct {
	NoImport bool // Skip automatic pool import
}

// SpindownWithZFS performs ZFS-aware spindown
func SpindownWithZFS(cfg *config.Config, controller string, devices []string, opts SpindownOptions) {
	// 1. Resolve target drives (same logic as existing Spindown)
	var drives []config.Drive

	if len(devices) > 0 {
		for _, dev := range devices {
			drives = append(drives, config.Drive{Device: dev, Name: dev})
		}
	} else {
		allDrives := cfg.GetAllDrives()
		drives = filterDrivesByController(allDrives, controller)
	}

	if len(drives) == 0 {
		if controller != "" {
			fmt.Printf("No drives found for controller %s\n", controller)
		} else {
			fmt.Println("No drives found")
		}
		return
	}

	// 2. If --force, skip ZFS handling entirely
	if opts.Force {
		fmt.Println("--force specified: skipping ZFS pool checks")
		spindownDrives(drives)
		return
	}

	// 3. Get device paths for analysis
	devicePaths := make([]string, len(drives))
	for i, d := range drives {
		devicePaths[i] = d.Device
	}

	// 4. Analyze ZFS membership
	zfsPools, nonZfsDrives, err := zfs.AnalyzeSpindownTargets(devicePaths)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not analyze ZFS membership: %v\n", err)
		// Continue without ZFS handling
		spindownDrives(drives)
		return
	}

	// 5. Handle ZFS pools
	var exportedPools []string
	var skippedDevices []string

	if len(zfsPools) > 0 {
		// Open database for tracking (optional)
		database, dbErr := db.New("")
		if dbErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: database unavailable, cannot track pool exports: %v\n", dbErr)
		}
		if database != nil {
			defer database.Close()
		}

		reader := bufio.NewReader(os.Stdin)

		for _, pool := range zfsPools {
			shouldExport := opts.ForceAll

			if !shouldExport {
				shouldExport = zfs.PromptForPoolExport(reader, pool)
			}

			if shouldExport {
				fmt.Printf("Exporting pool '%s'...\n", pool.PoolName)
				if err := zfs.ExportPool(pool.PoolName); err != nil {
					fmt.Fprintf(os.Stderr, "Error: failed to export pool '%s': %v\n", pool.PoolName, err)
					fmt.Fprintln(os.Stderr, "Aborting spindown to prevent data loss.")
					os.Exit(1)
				}

				// Record in database
				if database != nil {
					if err := database.RecordPoolExport(pool.PoolName, pool.Serials, "spindown"); err != nil {
						fmt.Fprintf(os.Stderr, "Warning: failed to record pool export: %v\n", err)
					}
				}

				exportedPools = append(exportedPools, pool.PoolName)
				fmt.Printf("Pool '%s' exported successfully\n", pool.PoolName)
			} else {
				fmt.Printf("Skipping drives in pool '%s' (pool not exported)\n", pool.PoolName)
				skippedDevices = append(skippedDevices, pool.Devices...)
			}
		}
	}

	// 6. Build list of drives to actually spindown
	skipSet := make(map[string]bool)
	for _, dev := range skippedDevices {
		skipSet[dev] = true
	}

	var drivesToSpindown []config.Drive
	for _, d := range drives {
		if !skipSet[d.Device] {
			drivesToSpindown = append(drivesToSpindown, d)
		}
	}

	// 7. Spindown remaining drives
	if len(drivesToSpindown) > 0 {
		spindownDrives(drivesToSpindown)
	} else {
		fmt.Println("No drives to spin down after ZFS handling")
	}

	// 8. Summary
	if len(exportedPools) > 0 {
		fmt.Printf("\nExported pools: %s\n", strings.Join(exportedPools, ", "))
		fmt.Println("Use 'jbodgod spinup' to re-import these pools automatically")
	}

	_ = nonZfsDrives // non-ZFS drives are included in drivesToSpindown
}

// spindownDrives is the core spindown logic
func spindownDrives(drives []config.Drive) {
	fmt.Printf("Spinning down %d drives...\n", len(drives))

	// Track sdparm command results
	var wg sync.WaitGroup
	spindownErrors := make([]string, len(drives))
	var errorMu sync.Mutex

	for i, d := range drives {
		wg.Add(1)
		go func(idx int, device string) {
			defer wg.Done()
			cmd := exec.Command("sdparm", "--command=stop", device)
			if err := cmd.Run(); err != nil {
				errorMu.Lock()
				spindownErrors[idx] = fmt.Sprintf("%s: %v", device, err)
				errorMu.Unlock()
			}
		}(i, d.Device)
	}
	wg.Wait()

	// Report any sdparm errors
	var failedCmds []string
	for _, errMsg := range spindownErrors {
		if errMsg != "" {
			failedCmds = append(failedCmds, errMsg)
		}
	}
	if len(failedCmds) > 0 {
		fmt.Fprintf(os.Stderr, "Warning: sdparm failed for %d drive(s):\n", len(failedCmds))
		for _, e := range failedCmds {
			fmt.Fprintf(os.Stderr, "  %s\n", e)
		}
	}

	// Monitor progress
	var finalStopped int
	for i := 0; i < 30; i++ {
		time.Sleep(time.Second)
		stopped := 0
		for _, d := range drives {
			out, _ := exec.Command("smartctl", "-i", "-n", "standby", d.Device).CombinedOutput()
			if strings.Contains(string(out), "NOT READY") {
				stopped++
			}
		}
		fmt.Printf("\r  Progress: %d/%d drives in standby...", stopped, len(drives))
		finalStopped = stopped
		if stopped == len(drives) {
			break
		}
	}

	// Report actual result
	if finalStopped == len(drives) {
		fmt.Println("\nAll drives in standby.")
	} else {
		fmt.Printf("\nWarning: Only %d/%d drives entered standby.\n", finalStopped, len(drives))
		fmt.Println("Some drives may not support spindown or may have failed to respond.")
	}
}

// SpinupWithZFS performs ZFS-aware spinup
func SpinupWithZFS(cfg *config.Config, controller string, devices []string, opts SpinupOptions) {
	// 1. Resolve target drives (same logic as existing Spinup)
	var drives []config.Drive

	if len(devices) > 0 {
		for _, dev := range devices {
			drives = append(drives, config.Drive{Device: dev, Name: dev})
		}
	} else {
		allDrives := cfg.GetAllDrives()
		drives = filterDrivesByController(allDrives, controller)
	}

	if len(drives) == 0 {
		if controller != "" {
			fmt.Printf("No drives found for controller %s\n", controller)
		} else {
			fmt.Println("No drives found")
		}
		return
	}

	// 2. Spinup the drives first
	spinupDrives(drives)

	// 3. Skip import if requested
	if opts.NoImport {
		fmt.Println("--no-import specified: skipping pool re-import")
		return
	}

	// 4. Wait for drives to stabilize
	fmt.Println("Waiting for drives to stabilize...")
	time.Sleep(3 * time.Second)

	// 5. Check database for pools to import
	database, dbErr := db.New("")
	if dbErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: database unavailable, cannot auto-import pools: %v\n", dbErr)
		return
	}
	defer database.Close()

	// 6. Get serials of spun-up drives
	var driveSerials []string
	for _, d := range drives {
		serial := zfs.GetDriveSerial(d.Device)
		if serial != "" {
			driveSerials = append(driveSerials, serial)
		}
	}

	// 7. Find pools that need import based on spun-up drives
	pendingPools, err := database.GetPendingImportsForDrives(driveSerials)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not check pending imports: %v\n", err)
		return
	}

	if len(pendingPools) == 0 {
		return
	}

	// 8. Import each pending pool
	fmt.Printf("\nRe-importing %d pool(s)...\n", len(pendingPools))

	for _, pool := range pendingPools {
		fmt.Printf("Importing pool '%s'...\n", pool.PoolName)
		if err := zfs.ImportPool(pool.PoolName); err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to import pool '%s': %v\n", pool.PoolName, err)
			database.MarkPoolImported(pool.PoolName, "failed")
			continue
		}

		database.MarkPoolImported(pool.PoolName, "success")
		fmt.Printf("Pool '%s' imported successfully\n", pool.PoolName)
	}
}

// spinupDrives is the core spinup logic
func spinupDrives(drives []config.Drive) {
	fmt.Printf("Spinning up %d drives...\n", len(drives))

	var wg sync.WaitGroup
	for _, d := range drives {
		wg.Add(1)
		go func(device string) {
			defer wg.Done()
			exec.Command("sdparm", "--command=start", device).Run()
		}(d.Device)
	}
	wg.Wait()

	// Monitor progress
	for i := 0; i < 60; i++ {
		time.Sleep(time.Second)
		active := 0
		for _, d := range drives {
			out, _ := exec.Command("smartctl", "-i", "-n", "standby", d.Device).CombinedOutput()
			if !strings.Contains(string(out), "NOT READY") {
				active++
			}
		}
		fmt.Printf("\r  Progress: %d/%d drives active...", active, len(drives))
		if active == len(drives) {
			break
		}
	}
	fmt.Println("\nAll drives active.")
}

// MonitorState holds cached state for efficient updates
type MonitorState struct {
	drives         []DriveInfo
	controllers    []hba.ControllerInfo
	enclosures     []hba.EnclosureInfo
	controllerTemp *int
	lastTempUpdate time.Time
	lastCtrlUpdate time.Time
	lastHBAUpdate  time.Time
	hbaLoaded      bool
}

// FetchHBAData retrieves controller and enclosure information from HBA tools
// Returns controllers, enclosures, and any error encountered
func FetchHBAData(forceRefresh bool) ([]hba.ControllerInfo, []hba.EnclosureInfo, error) {
	var controllers []hba.ControllerInfo
	var enclosures []hba.EnclosureInfo

	// Get list of available controllers
	controllerNums := hba.ListControllers()

	for _, ctrlNum := range controllerNums {
		ctrl, encs, _, err := hba.GetFullControllerInfo("c"+strconv.Itoa(ctrlNum), forceRefresh)
		if err != nil {
			continue
		}
		if ctrl != nil {
			controllers = append(controllers, *ctrl)
		}
		enclosures = append(enclosures, encs...)
	}

	return controllers, enclosures, nil
}

// getSerialForDevice gets the serial number for a device (cached)
func getSerialForDevice(device string) string {
	c := cache.Global()
	cacheKey := "drive:serial:" + device

	// Check cache first
	if cached := c.Get(cacheKey); cached != nil {
		return cached.(string)
	}

	// Fetch serial
	out, _ := exec.Command("smartctl", "-i", device).CombinedOutput()
	re := regexp.MustCompile(`Serial number:\s+(\S+)`)
	if matches := re.FindStringSubmatch(string(out)); len(matches) > 1 {
		c.SetStatic(cacheKey, matches[1])
		return matches[1]
	}
	return ""
}

// checkDriveState does a lightweight check of drive state only (no temp/serial)
// Uses cache with fast TTL to avoid hammering the drives
func checkDriveState(device string) string {
	c := cache.Global()
	cacheKey := "drive:state:" + device

	// Check cache first
	if cached := c.Get(cacheKey); cached != nil {
		return cached.(string)
	}

	// Fetch fresh state
	out, err := exec.Command("smartctl", "-i", "-n", "standby", device).CombinedOutput()
	output := string(out)

	var state string

	// Check for standby FIRST - smartctl returns non-zero exit code for standby
	// but this is not an error condition
	if strings.Contains(output, "STANDBY") || strings.Contains(output, "NOT READY") {
		state = "standby"
	} else if err != nil {
		// Check for device access failures
		if strings.Contains(output, "No such device") ||
			strings.Contains(output, "No such file") {
			state = "missing"
		} else if strings.Contains(output, "I/O error") {
			state = "failed"
		} else {
			// Other errors - mark as failed
			state = "failed"
		}
	} else {
		state = "active"
	}

	// Cache with fast TTL
	c.SetFast(cacheKey, state)
	return state
}

// getDriveTemp gets temperature for a single drive (only if active)
// Uses cache with dynamic TTL
func getDriveTemp(device string) *int {
	c := cache.Global()
	cacheKey := "drive:temp:" + device

	// Check cache first
	if cached := c.Get(cacheKey); cached != nil {
		temp := cached.(int)
		return &temp
	}

	// Fetch fresh temp
	out, _ := exec.Command("smartctl", "-A", device).CombinedOutput()
	re := regexp.MustCompile(`Current Drive Temperature:\s+(\d+)`)
	if matches := re.FindStringSubmatch(string(out)); len(matches) > 1 {
		if temp, err := strconv.Atoi(matches[1]); err == nil {
			c.SetDynamic(cacheKey, temp)
			return &temp
		}
	}
	return nil
}

// getControllerTemp fetches controller temperature via HBA package
func getControllerTemp(controller string) *int {
	temp, _ := hba.FetchControllerTemperature(controller)
	return temp
}

// getDeviceHBAInfo fetches enclosure/slot info from HBA
// Uses cache with static TTL (hardware doesn't change)
func getDeviceHBAInfo(serial string) (enclosure, slot *int) {
	if serial == "" {
		return nil, nil
	}

	c := cache.Global()
	cacheKey := "drive:hba:" + serial

	// Check cache first
	if cached := c.Get(cacheKey); cached != nil {
		info := cached.([2]*int)
		return info[0], info[1]
	}

	// Look up device
	dev := hba.GetDeviceBySerial(serial)
	if dev != nil {
		enc := dev.EnclosureID
		sl := dev.Slot
		c.SetStatic(cacheKey, [2]*int{&enc, &sl})
		return &enc, &sl
	}

	return nil, nil
}

// ANSI escape sequences for cursor control
const (
	cursorHome    = "\033[H"
	clearToEnd    = "\033[J"
	cursorSave    = "\033[s"
	cursorRestore = "\033[u"
	hideCursor    = "\033[?25l"
	showCursor    = "\033[?25h"
)

// moveCursor moves cursor to row, col (1-indexed)
func moveCursor(row, col int) {
	fmt.Printf("\033[%d;%dH", row, col)
}

// clearLine clears current line from cursor position
func clearLine() {
	fmt.Print("\033[K")
}

// Monitor provides live monitoring with efficient in-place updates
func Monitor(cfg *config.Config, interval int, tempInterval int, controller string) {
	drives := cfg.GetAllDrives()
	state := &MonitorState{
		drives: make([]DriveInfo, len(drives)),
	}

	// Initialize drive info with names
	for i, d := range drives {
		state.drives[i] = DriveInfo{
			Device: d.Device,
			Name:   d.Name,
			State:  "unknown",
		}
	}

	// Pre-load HBA data (background, cached)
	go func() {
		// Trigger HBA data fetch to populate cache and store in state
		controllers, enclosures, _ := FetchHBAData(false)
		state.controllers = controllers
		state.enclosures = enclosures
		state.lastHBAUpdate = time.Now()
		state.hbaLoaded = true
	}()

	// Header row positions
	const headerRow = 1
	const infoRow = 2
	const tableHeaderRow = 4
	const tableDataStart = 6

	// Calculate footer row based on drive count
	footerRow := tableDataStart + len(drives) + 1
	summaryRow := footerRow + 1
	tempStatsRow := footerRow + 2
	ctrlTempRow := footerRow + 3

	// Initial screen setup
	fmt.Print("\033[H\033[2J") // Clear screen once
	fmt.Print(hideCursor)

	// Ensure cursor is shown on exit
	defer fmt.Print(showCursor)

	// Draw static header
	moveCursor(headerRow, 1)
	fmt.Print("=== JBOD Drive Monitor === (Ctrl+C to exit)")

	// Draw table header (with SLOT column)
	moveCursor(tableHeaderRow, 1)
	fmt.Printf("%-10s %-8s %-10s %-8s %s", "DRIVE", "SLOT", "STATE", "TEMP", "STATUS")
	moveCursor(tableHeaderRow+1, 1)
	fmt.Print("-----------------------------------------------------")

	tickCount := 0
	tempTicks := tempInterval / interval // How many ticks between temp updates
	ctrlTicks := 30 / interval           // Controller temp every 30 seconds
	hbaTicks := 300 / interval           // HBA data every 5 minutes
	if tempTicks < 1 {
		tempTicks = 1
	}
	if ctrlTicks < 1 {
		ctrlTicks = 1
	}
	if hbaTicks < 1 {
		hbaTicks = 1
	}

	for {
		tickCount++
		shouldUpdateTemps := tickCount == 1 || tickCount%tempTicks == 0
		shouldUpdateCtrl := controller != "" && (tickCount == 1 || tickCount%ctrlTicks == 0)
		shouldUpdateHBA := state.hbaLoaded && tickCount%hbaTicks == 0

		// Update timestamp
		moveCursor(infoRow, 1)
		clearLine()
		fmt.Printf("Refreshing every %ds (temps every %ds) | %s",
			interval, tempInterval, time.Now().Format("2006-01-02 15:04:05"))

		// Update drive states (lightweight, every tick)
		var wg sync.WaitGroup
		stateResults := make([]string, len(drives))

		for i, d := range drives {
			wg.Add(1)
			go func(idx int, device string) {
				defer wg.Done()
				stateResults[idx] = checkDriveState(device)
			}(i, d.Device)
		}
		wg.Wait()

		// Apply state results
		for i, newState := range stateResults {
			state.drives[i].State = newState
		}

		// Update temperatures for active drives (less frequent)
		if shouldUpdateTemps {
			var tempWg sync.WaitGroup
			tempResults := make([]*int, len(drives))

			for i, d := range state.drives {
				if d.State == "active" {
					tempWg.Add(1)
					go func(idx int, device string) {
						defer tempWg.Done()
						tempResults[idx] = getDriveTemp(device)
					}(i, drives[i].Device)
				}
			}
			tempWg.Wait()

			// Apply temp results
			for i, temp := range tempResults {
				if state.drives[i].State == "active" {
					state.drives[i].Temp = temp
				} else {
					state.drives[i].Temp = nil
				}
			}
			state.lastTempUpdate = time.Now()
		}

		// Update controller temperature
		if shouldUpdateCtrl {
			state.controllerTemp = getControllerTemp(controller)
			state.lastCtrlUpdate = time.Now()
		}

		// Refresh HBA data periodically (every 5 minutes)
		if shouldUpdateHBA {
			go func() {
				controllers, enclosures, _ := FetchHBAData(true) // Force refresh
				state.controllers = controllers
				state.enclosures = enclosures
				state.lastHBAUpdate = time.Now()
			}()
		}

		// Render drive rows (in-place updates)
		var active, standby, missing, failed int
		var temps []int

		for i, d := range state.drives {
			row := tableDataStart + i
			moveCursor(row, 1)
			clearLine()

			// Get slot info if HBA data is loaded and we don't have it yet
			if state.hbaLoaded && d.Enclosure == nil && d.State == "active" {
				serial := getSerialForDevice(drives[i].Device)
				if serial != "" {
					enc, slot := getDeviceHBAInfo(serial)
					state.drives[i].Enclosure = enc
					state.drives[i].Slot = slot
					d = state.drives[i] // Refresh local copy
				}
			}

			// Format slot info
			slotStr := "-"
			if d.Enclosure != nil && d.Slot != nil {
				slotStr = fmt.Sprintf("%d:%d", *d.Enclosure, *d.Slot)
			}

			temp := "-"
			var status string

			switch d.State {
			case "active":
				active++
				if d.Temp != nil {
					temp = fmt.Sprintf("%d°C", *d.Temp)
					temps = append(temps, *d.Temp)

					if *d.Temp >= 60 {
						status = "🔴 HOT"
					} else if *d.Temp >= 55 {
						status = "🟡 WARM"
					} else {
						status = "🟢 OK"
					}
				} else {
					status = "⏳" // Active but temp not yet fetched
				}
			case "standby":
				standby++
				status = "💤"
			case "missing":
				missing++
				status = "❌ MISSING"
			case "failed":
				failed++
				status = "⛔ FAILED"
			default:
				failed++
				status = "⚠️  UNKNOWN"
			}

			fmt.Printf("%-10s %-8s %-10s %-8s %s", d.Device, slotStr, strings.ToUpper(d.State), temp, status)
		}

		// Update summary section
		moveCursor(footerRow, 1)
		clearLine()
		fmt.Print("-----------------------------------------------------")

		moveCursor(summaryRow, 1)
		clearLine()
		summaryParts := []string{fmt.Sprintf("Active: %d", active), fmt.Sprintf("Standby: %d", standby)}
		if missing > 0 {
			summaryParts = append(summaryParts, fmt.Sprintf("Missing: %d", missing))
		}
		if failed > 0 {
			summaryParts = append(summaryParts, fmt.Sprintf("Failed: %d", failed))
		}
		fmt.Print(strings.Join(summaryParts, " | "))

		moveCursor(tempStatsRow, 1)
		clearLine()
		if len(temps) > 0 {
			min, max, sum := temps[0], temps[0], 0
			for _, t := range temps {
				if t < min {
					min = t
				}
				if t > max {
					max = t
				}
				sum += t
			}
			avg := sum / len(temps)
			fmt.Printf("Temps: Min %d°C | Max %d°C | Avg %d°C", min, max, avg)
		}

		// Controller temperature
		if controller != "" {
			moveCursor(ctrlTempRow, 1)
			clearLine()
			if state.controllerTemp != nil {
				ctrlStatus := "🟢"
				if *state.controllerTemp >= 80 {
					ctrlStatus = "🔴"
				} else if *state.controllerTemp >= 70 {
					ctrlStatus = "🟡"
				}
				fmt.Printf("Controller %s: %d°C %s", controller, *state.controllerTemp, ctrlStatus)
			} else {
				fmt.Printf("Controller %s: -", controller)
			}
		}

		// Move cursor to a safe spot (below all content)
		moveCursor(ctrlTempRow+2, 1)

		time.Sleep(time.Duration(interval) * time.Second)
	}
}
