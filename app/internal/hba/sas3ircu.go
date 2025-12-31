package hba

import (
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/sigreer/jbodgod/internal/cache"
)

// parseSas3ircuDisplay parses output from 'sas3ircu <n> display'
func parseSas3ircuDisplay(output string, controllerID int) (*ControllerInfo, []EnclosureInfo, []PhysicalDevice) {
	ctrl := &ControllerInfo{
		ID: "c" + strconv.Itoa(controllerID),
	}
	var enclosures []EnclosureInfo
	var devices []PhysicalDevice

	lines := strings.Split(output, "\n")
	section := ""
	var currentDevice *PhysicalDevice

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Detect section headers
		if strings.HasPrefix(line, "Controller information") {
			section = "controller"
			continue
		} else if strings.HasPrefix(line, "Physical device information") {
			section = "devices"
			continue
		} else if strings.HasPrefix(line, "Enclosure information") {
			section = "enclosures"
			continue
		} else if strings.HasPrefix(line, "IR Volume information") {
			section = "volumes"
			continue
		} else if strings.HasPrefix(line, "---") {
			continue
		}

		// Parse based on section
		switch section {
		case "controller":
			parseControllerLine(line, ctrl)
		case "devices":
			if strings.HasPrefix(line, "Device is a") {
				// Save previous device
				if currentDevice != nil && currentDevice.Serial != "" {
					devices = append(devices, *currentDevice)
				}
				currentDevice = &PhysicalDevice{}
				if strings.Contains(line, "Enclosure services device") {
					currentDevice.DriveType = "Enclosure"
				}
			} else if currentDevice != nil {
				parseDeviceLine(line, currentDevice)
			}
		case "enclosures":
			parseEnclosureLine(line, &enclosures)
		}
	}

	// Don't forget last device
	if currentDevice != nil && currentDevice.Serial != "" && currentDevice.DriveType != "Enclosure" {
		devices = append(devices, *currentDevice)
	}

	return ctrl, enclosures, devices
}

func parseControllerLine(line string, ctrl *ControllerInfo) {
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return
	}
	key := strings.TrimSpace(parts[0])
	val := strings.TrimSpace(parts[1])

	switch key {
	case "Controller type":
		ctrl.Type = val
	case "BIOS version":
		ctrl.BIOSVersion = val
	case "Firmware version":
		ctrl.FirmwareVersion = val
	case "Channel description":
		ctrl.ChannelDesc = val
	case "Maximum physical devices":
		ctrl.MaxPhysicalDevices, _ = strconv.Atoi(val)
	case "Concurrent commands supported":
		ctrl.ConcurrentCommands, _ = strconv.Atoi(val)
	case "Bus":
		ctrl.PCIBus, _ = strconv.Atoi(val)
	case "Device":
		ctrl.PCIDevice, _ = strconv.Atoi(val)
	case "Function":
		ctrl.PCIFunction, _ = strconv.Atoi(val)
	case "RAID Support":
		ctrl.RAIDSupport = val == "Yes"
	}
}

func parseDeviceLine(line string, dev *PhysicalDevice) {
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return
	}
	key := strings.TrimSpace(parts[0])
	val := strings.TrimSpace(parts[1])

	switch key {
	case "Enclosure #":
		dev.EnclosureID, _ = strconv.Atoi(val)
	case "Slot #":
		dev.Slot, _ = strconv.Atoi(val)
	case "SAS Address":
		dev.SASAddress = strings.ReplaceAll(val, "-", "")
	case "State":
		// Extract state code from "Ready (RDY)"
		if idx := strings.Index(val, "("); idx > 0 {
			dev.State = strings.TrimSpace(val[:idx])
		} else {
			dev.State = val
		}
	case "Size (in MB)/(in sectors)":
		// Parse "7501160/15362376263"
		sizeParts := strings.Split(val, "/")
		if len(sizeParts) >= 1 {
			dev.SizeMB, _ = strconv.ParseInt(sizeParts[0], 10, 64)
		}
		if len(sizeParts) >= 2 {
			dev.Sectors, _ = strconv.ParseInt(sizeParts[1], 10, 64)
		}
	case "Manufacturer":
		dev.Manufacturer = val
	case "Model Number":
		dev.Model = val
	case "Firmware Revision":
		dev.Firmware = val
	case "Serial No":
		dev.Serial = val
	case "Unit Serial No(VPD)":
		if val != "N/A" {
			dev.SerialVPD = val
		}
	case "GUID":
		if val != "N/A" {
			dev.GUID = val
		}
	case "Protocol":
		dev.Protocol = val
	case "Drive Type":
		dev.DriveType = val
	}
}

func parseEnclosureLine(line string, enclosures *[]EnclosureInfo) {
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return
	}
	key := strings.TrimSpace(parts[0])
	val := strings.TrimSpace(parts[1])

	// Get or create current enclosure
	var enc *EnclosureInfo
	if len(*enclosures) == 0 || key == "Enclosure#" {
		*enclosures = append(*enclosures, EnclosureInfo{})
	}
	enc = &(*enclosures)[len(*enclosures)-1]

	switch key {
	case "Enclosure#":
		enc.ID, _ = strconv.Atoi(val)
	case "Logical ID":
		enc.LogicalID = val
	case "Numslots":
		enc.NumSlots, _ = strconv.Atoi(val)
	case "StartSlot":
		enc.StartSlot, _ = strconv.Atoi(val)
	}
}

// FetchSas3ircuData fetches data from sas3ircu with caching
func FetchSas3ircuData(controllerNum int, forceRefresh bool) (*ControllerInfo, []EnclosureInfo, []PhysicalDevice, error) {
	c := cache.Global()
	cacheKey := "sas3ircu:" + strconv.Itoa(controllerNum)

	// Check cache unless force refresh
	if !forceRefresh {
		if cached := c.Get(cacheKey); cached != nil {
			data := cached.(*sas3ircuCached)
			return data.ctrl, data.enclosures, data.devices, nil
		}
	}

	// Fetch fresh data
	out, err := exec.Command("sudo", "sas3ircu", strconv.Itoa(controllerNum), "display").CombinedOutput()
	if err != nil {
		return nil, nil, nil, err
	}

	ctrl, enclosures, devices := parseSas3ircuDisplay(string(out), controllerNum)

	// Cache with slow TTL (static hardware info)
	c.SetSlow(cacheKey, &sas3ircuCached{
		ctrl:       ctrl,
		enclosures: enclosures,
		devices:    devices,
	})

	return ctrl, enclosures, devices, nil
}

type sas3ircuCached struct {
	ctrl       *ControllerInfo
	enclosures []EnclosureInfo
	devices    []PhysicalDevice
}

// GetDeviceBySASAddress looks up a device by SAS address
func GetDeviceBySASAddress(sasAddr string) *PhysicalDevice {
	// Normalize address (remove dashes)
	sasAddr = strings.ReplaceAll(sasAddr, "-", "")
	sasAddr = strings.ToLower(sasAddr)

	// Try controller 0 first
	_, _, devices, err := FetchSas3ircuData(0, false)
	if err != nil {
		return nil
	}

	for _, d := range devices {
		if strings.ToLower(d.SASAddress) == sasAddr {
			return &d
		}
	}
	return nil
}

// GetDeviceBySerial looks up a device by serial number
// Matches against both Serial (short form) and SerialVPD (full form)
func GetDeviceBySerial(serial string) *PhysicalDevice {
	serial = strings.ToUpper(strings.TrimSpace(serial))

	_, _, devices, err := FetchSas3ircuData(0, false)
	if err != nil {
		return nil
	}

	for _, d := range devices {
		// Check exact match on Serial (short form)
		if strings.ToUpper(d.Serial) == serial {
			return &d
		}
		// Check exact match on SerialVPD (full form from smartctl)
		if strings.ToUpper(d.SerialVPD) == serial {
			return &d
		}
		// Check if input starts with short serial (prefix match)
		if d.Serial != "" && strings.HasPrefix(serial, strings.ToUpper(d.Serial)) {
			return &d
		}
	}
	return nil
}

// GetDeviceBySlot looks up a device by enclosure and slot
func GetDeviceBySlot(enclosure, slot int) *PhysicalDevice {
	_, _, devices, err := FetchSas3ircuData(0, false)
	if err != nil {
		return nil
	}

	for _, d := range devices {
		if d.EnclosureID == enclosure && d.Slot == slot {
			return &d
		}
	}
	return nil
}

// BuildSlotToDeviceMap creates a mapping from "enclosure:slot" to device path
func BuildSlotToDeviceMap() map[string]string {
	result := make(map[string]string)

	_, _, devices, err := FetchSas3ircuData(0, false)
	if err != nil {
		return result
	}

	// Get device paths by matching serial numbers
	for _, dev := range devices {
		key := strconv.Itoa(dev.EnclosureID) + ":" + strconv.Itoa(dev.Slot)
		// The actual device path mapping would need to come from
		// matching serial numbers with lsblk/smartctl output
		result[key] = dev.Serial
	}

	return result
}

// EnrichWithSas3ircu adds sas3ircu data to a device path lookup
func EnrichWithSas3ircu(serial string) map[string]string {
	result := make(map[string]string)

	dev := GetDeviceBySerial(serial)
	if dev == nil {
		return result
	}

	result["enclosure"] = strconv.Itoa(dev.EnclosureID)
	result["slot"] = strconv.Itoa(dev.Slot)
	result["sas_address"] = dev.SASAddress
	result["guid"] = dev.GUID
	result["protocol"] = dev.Protocol
	result["drive_type"] = dev.DriveType
	result["manufacturer"] = dev.Manufacturer
	result["firmware"] = dev.Firmware

	// Format size nicely
	if dev.SizeMB > 0 {
		sizeGB := dev.SizeMB / 1024
		if sizeGB >= 1000 {
			sizeTB := float64(sizeGB) / 1024
			result["size"] = strconv.FormatFloat(sizeTB, 'f', 2, 64) + " TB"
		} else {
			result["size"] = strconv.Itoa(int(sizeGB)) + " GB"
		}
	}

	return result
}

// ListControllers returns available controller numbers
func ListControllers() []int {
	// Try sas3ircu list to enumerate controllers
	out, err := exec.Command("sudo", "sas3ircu", "list").CombinedOutput()
	if err != nil {
		return []int{0} // Default to controller 0
	}

	var controllers []int
	re := regexp.MustCompile(`^\s*(\d+)\s+`)
	for _, line := range strings.Split(string(out), "\n") {
		if matches := re.FindStringSubmatch(line); len(matches) > 1 {
			if num, err := strconv.Atoi(matches[1]); err == nil {
				controllers = append(controllers, num)
			}
		}
	}

	if len(controllers) == 0 {
		return []int{0}
	}
	return controllers
}
