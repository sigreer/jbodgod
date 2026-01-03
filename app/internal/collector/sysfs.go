package collector

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sigreer/jbodgod/internal/cache"
)

// SysfsDevice represents device data collected from sysfs (no process spawning, no drive wake)
type SysfsDevice struct {
	// Identification
	Name       string  // sdg, sda, etc.
	Path       string  // /dev/sdg
	Serial     *string // from vpd_pg80
	WWN        *string // from wwid
	SASAddress *string // from sas_address

	// Hardware
	Model    *string // from model
	Vendor   *string // from vendor
	Firmware *string // from rev (if available)
	Size     *int64  // from size (in 512-byte sectors)

	// Location
	HCTL          *string // derived from scsi_device path
	EnclosureID   *string // from enclosure symlink
	Slot          *int    // from enclosure_device:SlotXX symlink
	EnclosurePath *string // sysfs path to enclosure

	// State
	State *string // from state (running, offline, etc.)
}

// SysfsEnclosure represents enclosure data from sysfs
type SysfsEnclosure struct {
	Path       string // sysfs path
	ID         string // enclosure SAS address/ID
	HCTL       string // H:C:T:L of enclosure device
	NumSlots   int
	Slots      []SysfsSlot
	Components int
}

// SysfsSlot represents a slot in an enclosure
type SysfsSlot struct {
	Number      int
	Status      string // OK, not installed, etc.
	Locate      bool   // LED state
	Fault       bool
	Active      bool
	DeviceHCTL  *string // linked device HCTL if populated
	PowerStatus *string
}

// CollectSysfsDevices gathers device info purely from sysfs (no process spawning)
// This does NOT wake sleeping drives
func CollectSysfsDevices() map[string]*SysfsDevice {
	c := cache.Global()
	cacheKey := "sysfs:devices"

	if cached := c.Get(cacheKey); cached != nil {
		return cached.(map[string]*SysfsDevice)
	}

	devices := make(map[string]*SysfsDevice)

	// Read /sys/block/ for all block devices
	entries, err := os.ReadDir("/sys/block")
	if err != nil {
		return devices
	}

	for _, entry := range entries {
		name := entry.Name()

		// Skip non-disk devices (loop, dm, nvme for now, etc.)
		if !strings.HasPrefix(name, "sd") {
			continue
		}

		dev := collectSysfsDevice(name)
		if dev != nil {
			devices[name] = dev
		}
	}

	c.SetSlow(cacheKey, devices)
	return devices
}

// collectSysfsDevice gathers data for a single device from sysfs
func collectSysfsDevice(name string) *SysfsDevice {
	blockPath := filepath.Join("/sys/block", name)
	devicePath := filepath.Join(blockPath, "device")

	// Check device exists
	if _, err := os.Stat(devicePath); os.IsNotExist(err) {
		return nil
	}

	dev := &SysfsDevice{
		Name: name,
		Path: "/dev/" + name,
	}

	// Model
	if data, err := os.ReadFile(filepath.Join(devicePath, "model")); err == nil {
		model := strings.TrimSpace(string(data))
		if model != "" {
			dev.Model = &model
		}
	}

	// Vendor
	if data, err := os.ReadFile(filepath.Join(devicePath, "vendor")); err == nil {
		vendor := strings.TrimSpace(string(data))
		if vendor != "" {
			dev.Vendor = &vendor
		}
	}

	// State
	if data, err := os.ReadFile(filepath.Join(devicePath, "state")); err == nil {
		state := strings.TrimSpace(string(data))
		if state != "" {
			dev.State = &state
		}
	}

	// WWN/WWID
	if data, err := os.ReadFile(filepath.Join(devicePath, "wwid")); err == nil {
		wwid := strings.TrimSpace(string(data))
		// Format: naa.XXXXXXXX or t10.XXXXX etc
		wwid = strings.TrimPrefix(wwid, "naa.")
		wwid = strings.TrimPrefix(wwid, "t10.")
		if wwid != "" {
			dev.WWN = &wwid
		}
	}

	// SAS Address (for SAS drives)
	if data, err := os.ReadFile(filepath.Join(devicePath, "sas_address")); err == nil {
		sasAddr := strings.TrimSpace(string(data))
		sasAddr = strings.TrimPrefix(sasAddr, "0x")
		if sasAddr != "" {
			dev.SASAddress = &sasAddr
		}
	}

	// Serial from VPD page 80
	if data, err := os.ReadFile(filepath.Join(devicePath, "vpd_pg80")); err == nil {
		// VPD page 80 is binary, serial starts after 4-byte header
		if len(data) > 4 {
			serial := strings.TrimSpace(string(data[4:]))
			// Remove non-printable characters
			serial = strings.Map(func(r rune) rune {
				if r >= 32 && r < 127 {
					return r
				}
				return -1
			}, serial)
			if serial != "" {
				dev.Serial = &serial
			}
		}
	}

	// Size (in 512-byte sectors)
	if data, err := os.ReadFile(filepath.Join(blockPath, "size")); err == nil {
		if size, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64); err == nil {
			dev.Size = &size
		}
	}

	// HCTL from scsi_device path
	scsiDevPath := filepath.Join(devicePath, "scsi_device")
	if entries, err := os.ReadDir(scsiDevPath); err == nil && len(entries) > 0 {
		hctl := entries[0].Name()
		dev.HCTL = &hctl
	}

	// Enclosure and Slot from enclosure_device symlink
	entries, _ := os.ReadDir(devicePath)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "enclosure_device:") {
			// Format: enclosure_device:SlotXX
			parts := strings.Split(entry.Name(), ":")
			if len(parts) == 2 {
				slotStr := strings.TrimPrefix(parts[1], "Slot")
				if slot, err := strconv.Atoi(slotStr); err == nil {
					dev.Slot = &slot
				}
			}

			// Follow symlink to get enclosure path
			linkPath := filepath.Join(devicePath, entry.Name())
			if target, err := os.Readlink(linkPath); err == nil {
				// Resolve to absolute path and extract enclosure HCTL
				encPath := filepath.Clean(filepath.Join(devicePath, target))
				dev.EnclosurePath = &encPath

				// Extract enclosure ID from path (the enclosure HCTL)
				// Path ends like: .../10:0:12:0/enclosure/10:0:12:0/Slot00
				parts := strings.Split(encPath, "/")
				for i, p := range parts {
					if p == "enclosure" && i+1 < len(parts) {
						encID := parts[i+1]
						dev.EnclosureID = &encID
						break
					}
				}
			}
			break
		}
	}

	return dev
}

// CollectSysfsEnclosures gathers enclosure info from sysfs
func CollectSysfsEnclosures() map[string]*SysfsEnclosure {
	c := cache.Global()
	cacheKey := "sysfs:enclosures"

	if cached := c.Get(cacheKey); cached != nil {
		return cached.(map[string]*SysfsEnclosure)
	}

	enclosures := make(map[string]*SysfsEnclosure)

	// Read /sys/class/enclosure/
	enclosureBase := "/sys/class/enclosure"
	entries, err := os.ReadDir(enclosureBase)
	if err != nil {
		return enclosures
	}

	for _, entry := range entries {
		hctl := entry.Name()
		encPath := filepath.Join(enclosureBase, hctl)

		enc := &SysfsEnclosure{
			Path: encPath,
			HCTL: hctl,
		}

		// Get enclosure ID (SAS address)
		if data, err := os.ReadFile(filepath.Join(encPath, "id")); err == nil {
			enc.ID = strings.TrimSpace(string(data))
		}

		// Get component count
		if data, err := os.ReadFile(filepath.Join(encPath, "components")); err == nil {
			enc.Components, _ = strconv.Atoi(strings.TrimSpace(string(data)))
			enc.NumSlots = enc.Components
		}

		// Collect slot info
		slotEntries, _ := os.ReadDir(encPath)
		for _, slotEntry := range slotEntries {
			if !strings.HasPrefix(slotEntry.Name(), "Slot") {
				continue
			}

			slotPath := filepath.Join(encPath, slotEntry.Name())
			slotNumStr := strings.TrimPrefix(slotEntry.Name(), "Slot")
			slotNum, _ := strconv.Atoi(slotNumStr)

			slot := SysfsSlot{Number: slotNum}

			// Status
			if data, err := os.ReadFile(filepath.Join(slotPath, "status")); err == nil {
				slot.Status = strings.TrimSpace(string(data))
			}

			// Locate LED
			if data, err := os.ReadFile(filepath.Join(slotPath, "locate")); err == nil {
				slot.Locate = strings.TrimSpace(string(data)) == "1"
			}

			// Fault LED
			if data, err := os.ReadFile(filepath.Join(slotPath, "fault")); err == nil {
				slot.Fault = strings.TrimSpace(string(data)) == "1"
			}

			// Active
			if data, err := os.ReadFile(filepath.Join(slotPath, "active")); err == nil {
				slot.Active = strings.TrimSpace(string(data)) == "1"
			}

			// Power status
			if data, err := os.ReadFile(filepath.Join(slotPath, "power_status")); err == nil {
				ps := strings.TrimSpace(string(data))
				slot.PowerStatus = &ps
			}

			// Device link - extract HCTL
			deviceLink := filepath.Join(slotPath, "device")
			if target, err := os.Readlink(deviceLink); err == nil {
				// Extract HCTL from path like .../10:0:0:0
				parts := strings.Split(target, "/")
				for _, p := range parts {
					if strings.Count(p, ":") == 3 {
						slot.DeviceHCTL = &p
						break
					}
				}
			}

			enc.Slots = append(enc.Slots, slot)
		}

		enclosures[hctl] = enc
	}

	c.SetSlow(cacheKey, enclosures)
	return enclosures
}

// SetSlotLocateLED sets the locate LED for a slot via sysfs (no sg_ses needed)
// Returns nil on success, error otherwise
func SetSlotLocateLED(enclosureHCTL string, slotNum int, on bool) error {
	slotPath := filepath.Join("/sys/class/enclosure", enclosureHCTL,
		"Slot"+strconv.Itoa(slotNum), "locate")

	value := "0"
	if on {
		value = "1"
	}

	return os.WriteFile(slotPath, []byte(value), 0644)
}

// SetSlotFaultLED sets the fault LED for a slot via sysfs
func SetSlotFaultLED(enclosureHCTL string, slotNum int, on bool) error {
	slotPath := filepath.Join("/sys/class/enclosure", enclosureHCTL,
		"Slot"+strconv.Itoa(slotNum), "fault")

	value := "0"
	if on {
		value = "1"
	}

	return os.WriteFile(slotPath, []byte(value), 0644)
}
