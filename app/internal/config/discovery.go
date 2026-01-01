package config

import (
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// DiscoverDrives dynamically discovers disk drives on the system.
// It uses lsscsi to find SCSI/SAS/SATA drives, filtering out:
//   - NVMe devices (nvme*)
//   - Virtual devices (loop*, dm-*, sr*)
//   - USB drives (when possible to detect)
//
// If lsscsi is unavailable, it falls back to lsblk.
// Returns a list of Drive structs with Device paths populated.
func DiscoverDrives() ([]Drive, error) {
	// Try lsscsi first (preferred for SAS/SATA drives in JBOD enclosures)
	drives, err := discoverViaLsscsi()
	if err == nil && len(drives) > 0 {
		return drives, nil
	}

	// Fall back to lsblk
	return discoverViaLsblk()
}

// discoverViaLsscsi uses lsscsi to find disk drives.
// lsscsi output format: [H:C:T:L] type vendor model rev device
// Example: [0:0:0:0] disk SEAGATE ST8000NM0055 SN02 /dev/sda
func discoverViaLsscsi() ([]Drive, error) {
	out, err := exec.Command("lsscsi").CombinedOutput()
	if err != nil {
		return nil, err
	}

	var drives []Drive
	lines := strings.Split(string(out), "\n")

	// Match lines like: [H:C:T:L] disk ... /dev/sdX
	// We want only disk devices, not cd, tape, etc.
	deviceRe := regexp.MustCompile(`\[([^\]]+)\]\s+disk\s+.*\s+(/dev/sd[a-z]+)\s*$`)

	for _, line := range lines {
		matches := deviceRe.FindStringSubmatch(line)
		if len(matches) < 3 {
			continue
		}

		scsiAddr := matches[1]
		device := matches[2]

		// Skip if device doesn't exist
		if _, err := exec.Command("test", "-b", device).CombinedOutput(); err != nil {
			continue
		}

		// Generate a bay name from SCSI address (H:C:T:L)
		// For JBOD drives, the Target ID often corresponds to the bay
		parts := strings.Split(scsiAddr, ":")
		bayName := "drive-" + strings.ReplaceAll(scsiAddr, ":", "-")
		if len(parts) >= 3 {
			bayName = "bay" + parts[2] // Use Target ID as bay number
		}

		drives = append(drives, Drive{
			Name:   bayName,
			Device: device,
		})
	}

	return drives, nil
}

// discoverViaLsblk uses lsblk to find disk drives as a fallback.
// This is less accurate for JBOD scenarios but works universally.
func discoverViaLsblk() ([]Drive, error) {
	// lsblk -d -o NAME,TYPE -n outputs: "sda disk", "nvme0n1 disk", etc.
	out, err := exec.Command("lsblk", "-d", "-o", "NAME,TYPE", "-n").CombinedOutput()
	if err != nil {
		return nil, err
	}

	var drives []Drive
	lines := strings.Split(string(out), "\n")

	for i, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		name := fields[0]
		devType := fields[1]

		// Only include disk type devices
		if devType != "disk" {
			continue
		}

		// Skip virtual and non-rotational storage we don't want
		if isExcludedDevice(name) {
			continue
		}

		device := filepath.Join("/dev", name)

		// Verify device exists
		if _, err := exec.Command("test", "-b", device).CombinedOutput(); err != nil {
			continue
		}

		drives = append(drives, Drive{
			Name:   "bay" + strings.TrimPrefix(name, "sd"),
			Device: device,
		})
		_ = i // silence unused variable warning
	}

	return drives, nil
}

// isExcludedDevice returns true for device names we should skip
func isExcludedDevice(name string) bool {
	// Exclude common virtual/unwanted devices
	excludePrefixes := []string{
		"loop",   // Loop devices
		"dm-",    // Device mapper
		"sr",     // CD/DVD
		"nvme",   // NVMe (handled separately, not JBOD)
		"zram",   // ZRAM swap
		"ram",    // RAM disks
		"md",     // MD RAID (we want underlying devices)
		"nbd",    // Network block devices
		"xvd",    // Xen virtual disks
		"vd",     // VirtIO disks
		"fd",     // Floppy
	}

	for _, prefix := range excludePrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}

	return false
}

// DiscoverDrivesFromHBA discovers drives via the HBA controller.
// This requires sas3ircu or storcli to be available.
// Returns drives with enclosure/slot information populated.
func DiscoverDrivesFromHBA() ([]Drive, error) {
	// Try sas3ircu first
	out, err := exec.Command("sudo", "sas3ircu", "0", "display").CombinedOutput()
	if err != nil {
		return nil, err
	}

	return parseHBADrives(string(out))
}

// parseHBADrives extracts drive information from sas3ircu display output
func parseHBADrives(output string) ([]Drive, error) {
	var drives []Drive

	lines := strings.Split(output, "\n")
	inDeviceSection := false
	var currentSlot int
	var currentSerial string

	for _, line := range lines {
		line = strings.TrimSpace(line)

		if strings.HasPrefix(line, "Physical device information") {
			inDeviceSection = true
			continue
		}

		if !inDeviceSection {
			continue
		}

		// Skip enclosure services devices
		if strings.Contains(line, "Enclosure services device") {
			currentSerial = ""
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		switch key {
		case "Slot #":
			currentSlot = parseInt(val)
		case "Serial No":
			currentSerial = val
		}

		// When we have a complete device entry with serial, find its device path
		if currentSerial != "" && key == "Protocol" {
			devicePath := findDeviceBySerial(currentSerial)
			if devicePath != "" {
				drives = append(drives, Drive{
					Name:   "bay" + strconv.Itoa(currentSlot),
					Device: devicePath,
				})
			}
			currentSerial = ""
		}
	}

	return drives, nil
}

// findDeviceBySerial finds a /dev/sdX device by serial number
func findDeviceBySerial(serial string) string {
	// Check /dev/disk/by-id/ for matching serial
	out, err := exec.Command("ls", "-la", "/dev/disk/by-id/").CombinedOutput()
	if err != nil {
		return ""
	}

	serial = strings.ToLower(serial)
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.ToLower(line)
		if strings.Contains(line, serial) {
			// Extract the target device from symlink
			parts := strings.Split(line, "->")
			if len(parts) < 2 {
				continue
			}
			target := strings.TrimSpace(parts[1])
			// Convert ../../sda to /dev/sda
			if strings.HasPrefix(target, "../../") {
				return filepath.Join("/dev", strings.TrimPrefix(target, "../../"))
			}
		}
	}

	return ""
}

func parseInt(s string) int {
	var result int
	for _, c := range s {
		if c >= '0' && c <= '9' {
			result = result*10 + int(c-'0')
		}
	}
	return result
}
