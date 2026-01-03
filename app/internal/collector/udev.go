package collector

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/sigreer/jbodgod/internal/cache"
)

// UdevDevice represents device data from udev database (no process spawning needed)
type UdevDevice struct {
	DevPath     string
	DevName     string // /dev/sdg
	DevType     string // disk, partition
	Subsystem   string // block, scsi
	IDVendor    string
	IDModel     string
	IDRevision  string
	IDSerial    string // full serial (e.g., 35000c500a6e7b82b)
	IDSerialShort string
	IDWWN       string
	IDWWNExt    string
	IDSCSISerial string // SCSI serial (from inquiry)
	IDBus       string // scsi, ata, usb
	IDType      string // disk
	IDPath      string // pci-0000:0d:00.0-sas-exp0x5003048020b3fe7f-phy0-lun-0
	DevLinks    []string
}

// CollectUdevDevices reads udev database directly (no udevadm process)
// Falls back to parsing /dev/disk/by-* symlinks if udev db unavailable
func CollectUdevDevices() map[string]*UdevDevice {
	c := cache.Global()
	cacheKey := "udev:devices"

	if cached := c.Get(cacheKey); cached != nil {
		return cached.(map[string]*UdevDevice)
	}

	devices := make(map[string]*UdevDevice)

	// Try reading udev database directly
	// Located at /run/udev/data/b<major>:<minor>
	blockDevs, err := os.ReadDir("/sys/block")
	if err != nil {
		return devices
	}

	for _, entry := range blockDevs {
		name := entry.Name()
		if !strings.HasPrefix(name, "sd") {
			continue
		}

		dev := collectUdevDevice(name)
		if dev != nil {
			devices[name] = dev
		}
	}

	// If we got data, cache it
	if len(devices) > 0 {
		c.SetSlow(cacheKey, devices)
	}

	return devices
}

// collectUdevDevice reads udev data for a single device
func collectUdevDevice(name string) *UdevDevice {
	// Read major:minor from sysfs
	devPath := filepath.Join("/sys/block", name, "dev")
	data, err := os.ReadFile(devPath)
	if err != nil {
		return nil
	}
	majMin := strings.TrimSpace(string(data))

	// Read udev database file
	udevPath := filepath.Join("/run/udev/data", "b"+majMin)
	file, err := os.Open(udevPath)
	if err != nil {
		// Fallback to symlink-based detection
		return collectFromSymlinks(name)
	}
	defer file.Close()

	dev := &UdevDevice{
		DevName: "/dev/" + name,
	}

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()

		// Lines starting with E: are environment variables
		if !strings.HasPrefix(line, "E:") {
			continue
		}

		line = strings.TrimPrefix(line, "E:")
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key, value := parts[0], parts[1]
		switch key {
		case "DEVPATH":
			dev.DevPath = value
		case "DEVTYPE":
			dev.DevType = value
		case "SUBSYSTEM":
			dev.Subsystem = value
		case "ID_VENDOR":
			dev.IDVendor = value
		case "ID_MODEL":
			dev.IDModel = value
		case "ID_REVISION":
			dev.IDRevision = value
		case "ID_SERIAL":
			dev.IDSerial = value
		case "ID_SERIAL_SHORT":
			dev.IDSerialShort = value
		case "ID_WWN":
			dev.IDWWN = strings.TrimPrefix(value, "0x")
		case "ID_WWN_WITH_EXTENSION":
			dev.IDWWNExt = strings.TrimPrefix(value, "0x")
		case "ID_SCSI_SERIAL":
			dev.IDSCSISerial = value
		case "ID_BUS":
			dev.IDBus = value
		case "ID_TYPE":
			dev.IDType = value
		case "ID_PATH":
			dev.IDPath = value
		case "DEVLINKS":
			dev.DevLinks = strings.Fields(value)
		}
	}

	return dev
}

// collectFromSymlinks extracts device info from /dev/disk/by-* symlinks
func collectFromSymlinks(name string) *UdevDevice {
	devPath := "/dev/" + name
	dev := &UdevDevice{
		DevName: devPath,
	}

	// Check by-id
	byIDPath := "/dev/disk/by-id"
	entries, err := os.ReadDir(byIDPath)
	if err != nil {
		return dev
	}

	for _, entry := range entries {
		linkPath := filepath.Join(byIDPath, entry.Name())
		target, err := filepath.EvalSymlinks(linkPath)
		if err != nil {
			continue
		}

		if target != devPath {
			continue
		}

		dev.DevLinks = append(dev.DevLinks, linkPath)

		// Parse the symlink name for info
		linkName := entry.Name()

		// WWN link: wwn-0x5000c500a6e7b82b
		if strings.HasPrefix(linkName, "wwn-") {
			wwn := strings.TrimPrefix(linkName, "wwn-")
			wwn = strings.TrimPrefix(wwn, "0x")
			dev.IDWWN = wwn
		}

		// SCSI link: scsi-35000c500a6e7b82b (3 = NAA designator)
		if strings.HasPrefix(linkName, "scsi-") {
			serial := strings.TrimPrefix(linkName, "scsi-")
			dev.IDSerial = serial
		}

		// ATA link: ata-ST8000NM0075_ZA1DKJT70000C907B6FF
		if strings.HasPrefix(linkName, "ata-") {
			parts := strings.SplitN(strings.TrimPrefix(linkName, "ata-"), "_", 2)
			if len(parts) >= 1 {
				dev.IDModel = parts[0]
			}
			if len(parts) >= 2 {
				dev.IDSCSISerial = parts[1]
			}
		}
	}

	// Check by-path
	byPathPath := "/dev/disk/by-path"
	entries, err = os.ReadDir(byPathPath)
	if err != nil {
		return dev
	}

	for _, entry := range entries {
		linkPath := filepath.Join(byPathPath, entry.Name())
		target, err := filepath.EvalSymlinks(linkPath)
		if err != nil {
			continue
		}

		if target == devPath {
			dev.IDPath = entry.Name()
			dev.DevLinks = append(dev.DevLinks, linkPath)
			break
		}
	}

	return dev
}

// ParseIDPath extracts useful info from ID_PATH
// Example: pci-0000:0d:00.0-sas-exp0x5003048020b3fe7f-phy0-lun-0
// Returns: pciAddress, sasExpander, phyNum
func ParseIDPath(idPath string) (pciAddr, sasExpander string, phyNum int) {
	parts := strings.Split(idPath, "-")

	for i, part := range parts {
		if part == "pci" && i+1 < len(parts) {
			pciAddr = parts[i+1]
		}
		if strings.HasPrefix(part, "exp") {
			sasExpander = strings.TrimPrefix(part, "exp")
		}
		if strings.HasPrefix(part, "phy") {
			phyStr := strings.TrimPrefix(part, "phy")
			phyNum, _ = parseInt(phyStr)
		}
	}

	return
}

func parseInt(s string) (int, error) {
	var result int
	for _, c := range s {
		if c >= '0' && c <= '9' {
			result = result*10 + int(c-'0')
		} else {
			break
		}
	}
	return result, nil
}
