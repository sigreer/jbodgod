package hba

import (
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/sigreer/jbodgod/internal/cache"
)

// parseStorcliOutput parses output from 'storcli /cX show all'
func parseStorcliOutput(output string, controllerID string) *ControllerInfo {
	ctrl := &ControllerInfo{
		ID: controllerID,
	}

	lines := strings.Split(output, "\n")
	section := ""

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Detect section headers
		if strings.HasPrefix(line, "Basics :") {
			section = "basics"
			continue
		} else if strings.HasPrefix(line, "Version :") {
			section = "version"
			continue
		} else if strings.HasPrefix(line, "PCI Version :") || strings.HasPrefix(line, "PCI :") {
			section = "pci"
			continue
		} else if strings.HasPrefix(line, "HwCfg :") {
			section = "hwcfg"
			continue
		} else if strings.HasPrefix(line, "Capabilities :") {
			section = "capabilities"
			continue
		} else if strings.HasPrefix(line, "Status :") && section != "" {
			section = "status"
			continue
		} else if strings.HasPrefix(line, "===") || strings.HasPrefix(line, "---") {
			continue
		}

		// Parse key = value lines
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		switch section {
		case "basics":
			parseStorcliBasics(key, val, ctrl)
		case "version":
			parseStorcliVersion(key, val, ctrl)
		case "pci":
			parseStorcliPCI(key, val, ctrl)
		case "hwcfg":
			parseStorcliHwCfg(key, val, ctrl)
		case "capabilities":
			parseStorcliCapabilities(key, val, ctrl)
		}
	}

	return ctrl
}

func parseStorcliBasics(key, val string, ctrl *ControllerInfo) {
	switch key {
	case "Adapter Type":
		ctrl.Type = val
	case "Model":
		ctrl.Model = val
	case "Serial Number":
		ctrl.Serial = val
	case "Concurrent commands supported":
		ctrl.ConcurrentCommands, _ = strconv.Atoi(val)
	case "SAS Address":
		ctrl.SASAddress = val
	case "PCI Address":
		ctrl.PCIAddress = val
	}
}

func parseStorcliVersion(key, val string, ctrl *ControllerInfo) {
	switch key {
	case "Firmware Version":
		ctrl.FirmwareVersion = val
	case "Bios Version":
		ctrl.BIOSVersion = val
	case "Driver Name":
		ctrl.DriverName = val
	case "Driver Version":
		ctrl.DriverVersion = val
	case "NVDATA Version":
		ctrl.NVDataVersion = val
	}
}

func parseStorcliPCI(key, val string, ctrl *ControllerInfo) {
	switch key {
	case "Vendor Id":
		ctrl.PCIVendorID = val
	case "Device Id":
		ctrl.PCIDeviceID = val
	case "Bus Number":
		ctrl.PCIBus, _ = strconv.Atoi(val)
	case "Device Number":
		ctrl.PCIDevice, _ = strconv.Atoi(val)
	case "Function Number":
		ctrl.PCIFunction, _ = strconv.Atoi(val)
	}
}

func parseStorcliHwCfg(key, val string, ctrl *ControllerInfo) {
	switch key {
	case "ROC temperature(Degree Celsius)":
		if temp, err := strconv.Atoi(val); err == nil {
			ctrl.Temperature = &temp
		}
	case "Backend Port Count":
		ctrl.PhyCount, _ = strconv.Atoi(val)
	}
}

func parseStorcliCapabilities(key, val string, ctrl *ControllerInfo) {
	switch key {
	case "Supported Drives":
		ctrl.SupportedDrives = val
	case "Max Parallel Commands":
		ctrl.ConcurrentCommands, _ = strconv.Atoi(val)
	}
}

// FetchStorcliData fetches controller data from storcli with caching
func FetchStorcliData(controllerID string, forceRefresh bool) (*ControllerInfo, error) {
	c := cache.Global()
	cacheKey := "storcli:" + controllerID

	// Check cache unless force refresh
	if !forceRefresh {
		if cached := c.Get(cacheKey); cached != nil {
			return cached.(*ControllerInfo), nil
		}
	}

	// Fetch fresh data
	storcliPath := "/" + controllerID
	out, err := exec.Command("sudo", "storcli", storcliPath, "show", "all").CombinedOutput()
	if err != nil {
		return nil, err
	}

	ctrl := parseStorcliOutput(string(out), controllerID)

	// Cache with slow TTL (static hardware info)
	c.SetSlow(cacheKey, ctrl)

	return ctrl, nil
}

// FetchControllerTemperature fetches just the temperature (fast refresh)
func FetchControllerTemperature(controllerID string) (*int, error) {
	c := cache.Global()
	cacheKey := "storcli:temp:" + controllerID

	// Check cache (short TTL for temperature)
	if cached := c.Get(cacheKey); cached != nil {
		temp := cached.(int)
		return &temp, nil
	}

	// Fetch temperature
	storcliPath := "/" + controllerID
	out, err := exec.Command("sudo", "storcli", storcliPath, "show", "temperature").CombinedOutput()
	if err != nil {
		return nil, err
	}

	// Parse temperature
	re := regexp.MustCompile(`ROC temperature\(Degree Celsius\)\s+(\d+)`)
	if matches := re.FindStringSubmatch(string(out)); len(matches) > 1 {
		if temp, err := strconv.Atoi(matches[1]); err == nil {
			c.SetDynamic(cacheKey, temp)
			return &temp, nil
		}
	}

	return nil, nil
}

// MergeControllerInfo merges storcli data into sas3ircu data
// storcli typically has better model/serial info
func MergeControllerInfo(sas3ircu, storcli *ControllerInfo) *ControllerInfo {
	if sas3ircu == nil {
		return storcli
	}
	if storcli == nil {
		return sas3ircu
	}

	// Start with sas3ircu as base
	merged := *sas3ircu

	// Overlay storcli data where better
	if storcli.Model != "" {
		merged.Model = storcli.Model
	}
	if storcli.Serial != "" {
		merged.Serial = storcli.Serial
	}
	if storcli.SASAddress != "" {
		merged.SASAddress = storcli.SASAddress
	}
	if storcli.DriverName != "" {
		merged.DriverName = storcli.DriverName
	}
	if storcli.DriverVersion != "" {
		merged.DriverVersion = storcli.DriverVersion
	}
	if storcli.NVDataVersion != "" {
		merged.NVDataVersion = storcli.NVDataVersion
	}
	if storcli.PCIAddress != "" {
		merged.PCIAddress = storcli.PCIAddress
	}
	if storcli.PCIVendorID != "" {
		merged.PCIVendorID = storcli.PCIVendorID
	}
	if storcli.PCIDeviceID != "" {
		merged.PCIDeviceID = storcli.PCIDeviceID
	}
	if storcli.SupportedDrives != "" {
		merged.SupportedDrives = storcli.SupportedDrives
	}
	if storcli.Temperature != nil {
		merged.Temperature = storcli.Temperature
	}
	if storcli.PhyCount > 0 {
		merged.PhyCount = storcli.PhyCount
	}

	return &merged
}

// GetFullControllerInfo gets merged data from all sources
func GetFullControllerInfo(controllerID string, forceRefresh bool) (*ControllerInfo, []EnclosureInfo, []PhysicalDevice, error) {
	// Extract controller number
	ctrlNum := 0
	if strings.HasPrefix(controllerID, "c") {
		ctrlNum, _ = strconv.Atoi(controllerID[1:])
	}

	// Get sas3ircu data
	sas3ctrl, enclosures, devices, err := FetchSas3ircuData(ctrlNum, forceRefresh)
	if err != nil {
		// Try storcli alone
		storcliCtrl, err2 := FetchStorcliData(controllerID, forceRefresh)
		if err2 != nil {
			return nil, nil, nil, err
		}
		return storcliCtrl, nil, nil, nil
	}

	// Get storcli data
	storcliCtrl, _ := FetchStorcliData(controllerID, forceRefresh)

	// Merge
	merged := MergeControllerInfo(sas3ctrl, storcliCtrl)

	return merged, enclosures, devices, nil
}
