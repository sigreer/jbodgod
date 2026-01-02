package collector

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/sigreer/jbodgod/internal/cache"
)

// CollectSystemData gathers data from all bulk sources
func CollectSystemData(forceRefresh bool) *SystemData {
	c := cache.Global()
	cacheKey := "system:bulk"

	if !forceRefresh {
		if cached := c.Get(cacheKey); cached != nil {
			return cached.(*SystemData)
		}
	}

	data := &SystemData{
		LsblkDevices:  make(map[string]*LsblkDevice),
		BlkidDevices:  make(map[string]*BlkidDevice),
		LsscsiDevices: make(map[string]*LsscsiDevice),
		ZpoolVdevs:    make(map[string]*ZpoolVdev),
		LvmPVs:        make(map[string]*LvmPV),
		ByIDLinks:     make(map[string]string),
		Controllers:   make(map[string]*ControllerData),
		HBADevices:    make(map[string]*HBADevice),
	}

	// Collect from all sources in parallel would be ideal,
	// but for simplicity we do sequential with individual caching
	collectLsblk(data)
	collectBlkid(data)
	collectLsscsi(data)
	collectZpool(data)
	collectLVM(data)
	collectByID(data)
	collectHBA(data)

	c.SetFast(cacheKey, data)
	return data
}

// collectLsblk parses lsblk JSON output
func collectLsblk(data *SystemData) {
	c := cache.Global()
	cacheKey := "system:lsblk"

	if cached := c.Get(cacheKey); cached != nil {
		for k, v := range cached.(map[string]*LsblkDevice) {
			data.LsblkDevices[k] = v
		}
		return
	}

	out, err := exec.Command("lsblk", "-d", "-b", "-o",
		"NAME,PATH,SIZE,SERIAL,WWN,MODEL,VENDOR,REV,HCTL,TRAN,TYPE,MAJ:MIN,FSTYPE,UUID,LABEL,PARTUUID,PARTLABEL",
		"-J").CombinedOutput()
	if err != nil {
		return
	}

	var result struct {
		Blockdevices []struct {
			Name      string  `json:"name"`
			Path      string  `json:"path"`
			Size      *string `json:"size"`
			Serial    *string `json:"serial"`
			WWN       *string `json:"wwn"`
			Model     *string `json:"model"`
			Vendor    *string `json:"vendor"`
			Rev       *string `json:"rev"`
			HCTL      *string `json:"hctl"`
			Tran      *string `json:"tran"`
			Type      string  `json:"type"`
			MajMin    *string `json:"maj:min"`
			FSType    *string `json:"fstype"`
			UUID      *string `json:"uuid"`
			Label     *string `json:"label"`
			PartUUID  *string `json:"partuuid"`
			PartLabel *string `json:"partlabel"`
		} `json:"blockdevices"`
	}

	if err := json.Unmarshal(out, &result); err != nil {
		return
	}

	devices := make(map[string]*LsblkDevice)
	for _, bd := range result.Blockdevices {
		dev := &LsblkDevice{
			Name:      bd.Name,
			Path:      bd.Path,
			Serial:    trimPtr(bd.Serial),
			WWN:       trimPtr(bd.WWN),
			Model:     trimPtr(bd.Model),
			Vendor:    trimPtr(bd.Vendor),
			Rev:       trimPtr(bd.Rev),
			HCTL:      trimPtr(bd.HCTL),
			Tran:      trimPtr(bd.Tran),
			Type:      bd.Type,
			MajMin:    trimPtr(bd.MajMin),
			FSType:    trimPtr(bd.FSType),
			UUID:      trimPtr(bd.UUID),
			Label:     trimPtr(bd.Label),
			PartUUID:  trimPtr(bd.PartUUID),
			PartLabel: trimPtr(bd.PartLabel),
		}
		if bd.Size != nil {
			if size, err := strconv.ParseInt(*bd.Size, 10, 64); err == nil {
				dev.Size = &size
			}
		}
		devices[bd.Name] = dev
		data.LsblkDevices[bd.Name] = dev
	}

	c.SetFast(cacheKey, devices)
}

// collectBlkid parses blkid output
func collectBlkid(data *SystemData) {
	c := cache.Global()
	cacheKey := "system:blkid"

	if cached := c.Get(cacheKey); cached != nil {
		for k, v := range cached.(map[string]*BlkidDevice) {
			data.BlkidDevices[k] = v
		}
		return
	}

	out, err := exec.Command("sudo", "blkid", "-o", "export").CombinedOutput()
	if err != nil {
		return
	}

	devices := make(map[string]*BlkidDevice)
	var current *BlkidDevice

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			if current != nil && current.Device != "" {
				devices[current.Device] = current
				data.BlkidDevices[current.Device] = current
			}
			current = nil
			continue
		}

		if current == nil {
			current = &BlkidDevice{}
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key, val := parts[0], parts[1]

		switch key {
		case "DEVNAME":
			current.Device = val
		case "UUID":
			current.UUID = &val
		case "UUID_SUB":
			current.UUIDSub = &val
		case "TYPE":
			current.Type = &val
		case "LABEL":
			current.Label = &val
		case "PARTUUID":
			current.PartUUID = &val
		case "PARTLABEL":
			current.PartLabel = &val
		}
	}
	// Don't forget last device
	if current != nil && current.Device != "" {
		devices[current.Device] = current
		data.BlkidDevices[current.Device] = current
	}

	c.SetFast(cacheKey, devices)
}

// collectLsscsi parses lsscsi -g output
func collectLsscsi(data *SystemData) {
	c := cache.Global()
	cacheKey := "system:lsscsi"

	if cached := c.Get(cacheKey); cached != nil {
		for k, v := range cached.(map[string]*LsscsiDevice) {
			data.LsscsiDevices[k] = v
		}
		return
	}

	out, err := exec.Command("lsscsi", "-g").CombinedOutput()
	if err != nil {
		return
	}

	devices := make(map[string]*LsscsiDevice)
	// Format: [H:C:T:L]  type    vendor   model            rev   device   sgdev
	re := regexp.MustCompile(`\[([^\]]+)\]\s+(\S+)\s+(\S+)\s+(\S+)\s+(\S+)\s+(\S+)\s*(\S*)`)

	for _, line := range strings.Split(string(out), "\n") {
		matches := re.FindStringSubmatch(line)
		if len(matches) < 7 {
			continue
		}

		hctl := matches[1]
		devType := matches[2]
		vendor := strings.TrimSpace(matches[3])
		model := strings.TrimSpace(matches[4])
		rev := strings.TrimSpace(matches[5])
		device := matches[6]
		sgDev := ""
		if len(matches) > 7 {
			sgDev = matches[7]
		}

		if device == "-" {
			continue
		}

		dev := &LsscsiDevice{
			HCTL:   hctl,
			Type:   devType,
			Device: device,
		}
		if vendor != "-" {
			dev.Vendor = &vendor
		}
		if model != "-" {
			dev.Model = &model
		}
		if rev != "-" {
			dev.Rev = &rev
		}
		if sgDev != "" && sgDev != "-" {
			dev.SGDevice = &sgDev
		}

		devices[device] = dev
		data.LsscsiDevices[device] = dev
	}

	c.SetFast(cacheKey, devices)
}

// collectZpool parses zpool status -gLP output
func collectZpool(data *SystemData) {
	c := cache.Global()
	cacheKey := "system:zpool"

	if cached := c.Get(cacheKey); cached != nil {
		for k, v := range cached.(map[string]*ZpoolVdev) {
			data.ZpoolVdevs[k] = v
		}
		return
	}

	out, err := exec.Command("sudo", "zpool", "status", "-gLP").CombinedOutput()
	if err != nil {
		return
	}

	vdevs := make(map[string]*ZpoolVdev)
	var currentPool string
	var poolState string
	var currentVdevType string

	lines := strings.Split(string(out), "\n")
	inConfig := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Pool name
		if strings.HasPrefix(trimmed, "pool:") {
			currentPool = strings.TrimSpace(strings.TrimPrefix(trimmed, "pool:"))
			currentVdevType = ""
			continue
		}

		// Pool state
		if strings.HasPrefix(trimmed, "state:") {
			poolState = strings.TrimSpace(strings.TrimPrefix(trimmed, "state:"))
			continue
		}

		// Config section start
		if strings.HasPrefix(trimmed, "config:") {
			inConfig = true
			continue
		}

		// End of config
		if strings.HasPrefix(trimmed, "errors:") {
			inConfig = false
			continue
		}

		if !inConfig || currentPool == "" {
			continue
		}

		// Skip header
		if strings.HasPrefix(trimmed, "NAME") {
			continue
		}

		// Parse vdev lines
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}

		name := fields[0]
		state := fields[1]
		read, _ := strconv.Atoi(fields[2])
		write, _ := strconv.Atoi(fields[3])
		cksum, _ := strconv.Atoi(fields[4])

		// Determine if this is a vdev type or leaf device
		indent := len(line) - len(strings.TrimLeft(line, " \t"))

		// Pool name line (indent ~2)
		if indent <= 2 && name == currentPool {
			continue
		}

		// Vdev type line (mirror, raidz, etc.) - typically indent 4
		if indent <= 4 && (strings.HasPrefix(name, "mirror") ||
			strings.HasPrefix(name, "raidz") ||
			strings.HasPrefix(name, "spare") ||
			strings.HasPrefix(name, "cache") ||
			strings.HasPrefix(name, "log")) {
			currentVdevType = name
			continue
		}

		// This is a leaf device (GUID or path)
		vdev := &ZpoolVdev{
			PoolName:    currentPool,
			PoolState:   poolState,
			VdevGUID:    name, // Could be GUID or path
			VdevType:    currentVdevType,
			State:       state,
			ReadErrors:  read,
			WriteErrors: write,
			CksumErrors: cksum,
		}

		// If name starts with / it's a path
		if strings.HasPrefix(name, "/") {
			vdev.DevicePath = &name
			// Extract base device for GUID lookup
		}

		vdevs[name] = vdev
		data.ZpoolVdevs[name] = vdev
	}

	c.SetFast(cacheKey, vdevs)
}

// collectLVM parses pvs output
func collectLVM(data *SystemData) {
	c := cache.Global()
	cacheKey := "system:lvm"

	if cached := c.Get(cacheKey); cached != nil {
		for k, v := range cached.(map[string]*LvmPV) {
			data.LvmPVs[k] = v
		}
		return
	}

	// Use pvs with specific output format
	out, err := exec.Command("sudo", "pvs", "--noheadings", "--nosuffix", "--units", "b",
		"-o", "pv_name,pv_uuid,vg_name,pv_size,pv_free", "--separator", "|").CombinedOutput()
	if err != nil {
		return
	}

	pvs := make(map[string]*LvmPV)

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Split(line, "|")
		if len(parts) < 5 {
			continue
		}

		pvName := strings.TrimSpace(parts[0])
		pvUUID := strings.TrimSpace(parts[1])
		vgName := strings.TrimSpace(parts[2])

		pv := &LvmPV{
			PVName: pvName,
			PVUUID: pvUUID,
		}

		if vgName != "" {
			pv.VGName = &vgName
		}

		if size, err := strconv.ParseInt(strings.TrimSpace(parts[3]), 10, 64); err == nil {
			pv.Size = &size
		}
		if free, err := strconv.ParseInt(strings.TrimSpace(parts[4]), 10, 64); err == nil {
			pv.Free = &free
		}

		pvs[pvName] = pv
		data.LvmPVs[pvName] = pv
	}

	c.SetFast(cacheKey, pvs)
}

// collectByID reads /dev/disk/by-id symlinks
func collectByID(data *SystemData) {
	c := cache.Global()
	cacheKey := "system:byid"

	if cached := c.Get(cacheKey); cached != nil {
		for k, v := range cached.(map[string]string) {
			data.ByIDLinks[k] = v
		}
		return
	}

	links := make(map[string]string)

	entries, err := filepath.Glob("/dev/disk/by-id/*")
	if err != nil {
		return
	}

	for _, entry := range entries {
		// Skip partition entries
		if strings.Contains(entry, "-part") {
			continue
		}

		target, err := filepath.EvalSymlinks(entry)
		if err != nil {
			continue
		}

		// Store device path -> by-id path
		links[target] = entry
		data.ByIDLinks[target] = entry
	}

	c.SetSlow(cacheKey, links)
}

// collectHBA collects data from HBA tools
func collectHBA(data *SystemData) {
	// Try storcli first (more detailed), fall back to sas3ircu
	collectStorcli(data)
	if len(data.HBADevices) == 0 {
		collectSas3ircu(data)
	}
}

// collectStorcli parses storcli output
func collectStorcli(data *SystemData) {
	c := cache.Global()
	cacheKey := "system:storcli"

	if cached := c.Get(cacheKey); cached != nil {
		cachedData := cached.(*storcliCache)
		for k, v := range cachedData.Devices {
			data.HBADevices[k] = v
		}
		for k, v := range cachedData.Controllers {
			data.Controllers[k] = v
		}
		return
	}

	// First get controller list
	out, err := exec.Command("sudo", "storcli", "show").CombinedOutput()
	if err != nil {
		return
	}

	// Parse controller count
	controllerIDs := parseStorcliControllers(string(out))

	cachedData := &storcliCache{
		Devices:     make(map[string]*HBADevice),
		Controllers: make(map[string]*ControllerData),
	}

	for _, ctrlID := range controllerIDs {
		// Get controller info
		ctrl := collectStorcliController(ctrlID)
		if ctrl != nil {
			data.Controllers[ctrlID] = ctrl
			cachedData.Controllers[ctrlID] = ctrl
		}

		// Get drive details
		devices := collectStorcliDrives(ctrlID)
		for serial, dev := range devices {
			data.HBADevices[serial] = dev
			cachedData.Devices[serial] = dev
		}
	}

	c.SetSlow(cacheKey, cachedData)
}

type storcliCache struct {
	Devices     map[string]*HBADevice
	Controllers map[string]*ControllerData
}

func parseStorcliControllers(output string) []string {
	var controllers []string
	// Look for controller lines in the output
	re := regexp.MustCompile(`^(\d+)\s+`)
	for _, line := range strings.Split(output, "\n") {
		if matches := re.FindStringSubmatch(strings.TrimSpace(line)); len(matches) > 1 {
			controllers = append(controllers, "c"+matches[1])
		}
	}
	if len(controllers) == 0 {
		controllers = []string{"c0"} // Default
	}
	return controllers
}

func collectStorcliController(ctrlID string) *ControllerData {
	out, err := exec.Command("sudo", "storcli", "/"+ctrlID, "show").CombinedOutput()
	if err != nil {
		return nil
	}

	ctrl := &ControllerData{ID: ctrlID}
	output := string(out)

	// Parse key fields
	patterns := map[string]*string{
		`Product Name = (.+)`:    nil,
		`Serial Number = (.+)`:   nil,
		`SAS Address = (.+)`:     nil,
		`FW Version = (.+)`:      nil,
		`BIOS Version = (.+)`:    nil,
		`Driver Version = (.+)`:  nil,
		`PCI Address = (.+)`:     nil,
	}

	for pattern := range patterns {
		re := regexp.MustCompile(pattern)
		if matches := re.FindStringSubmatch(output); len(matches) > 1 {
			val := strings.TrimSpace(matches[1])
			switch {
			case strings.Contains(pattern, "Product Name"):
				ctrl.Model = &val
			case strings.Contains(pattern, "Serial Number"):
				ctrl.Serial = &val
			case strings.Contains(pattern, "SAS Address"):
				ctrl.SASAddress = &val
			case strings.Contains(pattern, "FW Version"):
				ctrl.FirmwareVer = &val
			case strings.Contains(pattern, "BIOS Version"):
				ctrl.BIOSVer = &val
			case strings.Contains(pattern, "Driver Version"):
				ctrl.DriverVer = &val
			case strings.Contains(pattern, "PCI Address"):
				ctrl.PCIAddress = &val
			}
		}
	}

	// Physical drives count
	re := regexp.MustCompile(`Physical Drives = (\d+)`)
	if matches := re.FindStringSubmatch(output); len(matches) > 1 {
		ctrl.PhysicalDrives, _ = strconv.Atoi(matches[1])
	}

	return ctrl
}

func collectStorcliDrives(ctrlID string) map[string]*HBADevice {
	devices := make(map[string]*HBADevice)

	out, err := exec.Command("sudo", "storcli", "/"+ctrlID+"/eall/sall", "show", "all").CombinedOutput()
	if err != nil {
		return devices
	}

	output := string(out)
	// Split by drive sections
	driveSections := strings.Split(output, "Drive /"+ctrlID+"/e")

	for _, section := range driveSections[1:] { // Skip first empty section
		dev := parseStorcliDriveSection(ctrlID, section)
		if dev != nil && dev.Serial != "" {
			devices[strings.ToUpper(dev.Serial)] = dev
		}
	}

	return devices
}

func parseStorcliDriveSection(ctrlID, section string) *HBADevice {
	dev := &HBADevice{ControllerID: ctrlID}

	// Parse enclosure and slot from section header (e.g., "12/s5 :")
	re := regexp.MustCompile(`^(\d+)/s(\d+)`)
	if matches := re.FindStringSubmatch(section); len(matches) > 2 {
		dev.EnclosureID, _ = strconv.Atoi(matches[1])
		dev.Slot, _ = strconv.Atoi(matches[2])
	}

	// Parse device attributes
	patterns := map[string]func(string){
		`SN = (\S+)`:                    func(v string) { dev.Serial = v },
		`WWN = (\S+)`:                   func(v string) { dev.WWN = &v },
		`Model Number = (.+)`:           func(v string) { v = strings.TrimSpace(v); dev.Model = &v },
		`Manufacturer Id = (.+)`:        func(v string) { v = strings.TrimSpace(v); dev.Vendor = &v },
		`Firmware Revision = (\S+)`:     func(v string) { dev.Firmware = &v },
		`Raw size = ([0-9.]+) TB`:       func(v string) {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				size := int64(f * 1024 * 1024 * 1024 * 1024)
				dev.SizeBytes = &size
			}
		},
		`Sector Size = (\d+)`:           func(v string) {
			if i, err := strconv.Atoi(v); err == nil {
				dev.SectorSize = &i
			}
		},
		`Link Speed = (.+)`:             func(v string) { dev.LinkSpeed = &v },
		`Media Error Count = (\d+)`:     func(v string) {
			if i, err := strconv.Atoi(v); err == nil && i > 0 {
				dev.MediaErrors = &i
			}
		},
		`Other Error Count = (\d+)`:     func(v string) {
			if i, err := strconv.Atoi(v); err == nil && i > 0 {
				dev.OtherErrors = &i
			}
		},
		`Predictive Failure Count = (\d+)`: func(v string) {
			if i, err := strconv.Atoi(v); err == nil && i > 0 {
				dev.PredFailure = &i
			}
		},
	}

	for pattern, setter := range patterns {
		re := regexp.MustCompile(pattern)
		if matches := re.FindStringSubmatch(section); len(matches) > 1 {
			setter(matches[1])
		}
	}

	// Parse interface and media type from summary line
	// Format: EID:Slt DID State DG Size Intf Med ...
	summaryRe := regexp.MustCompile(`\d+:\d+\s+(\d+)\s+\S+\s+\S+\s+[\d.]+\s+\S+\s+(SAS|SATA)\s+(HDD|SSD)`)
	if matches := summaryRe.FindStringSubmatch(section); len(matches) > 3 {
		if did, err := strconv.Atoi(matches[1]); err == nil {
			dev.DeviceID = &did
		}
		dev.Protocol = &matches[2]
		dev.MediaType = &matches[3]
	}

	// Parse SAS address from port info
	sasRe := regexp.MustCompile(`0\s+Active\s+[\d.]+Gb/s\s+(0x[0-9a-fA-F]+)`)
	if matches := sasRe.FindStringSubmatch(section); len(matches) > 1 {
		dev.SASAddress = &matches[1]
	}

	return dev
}

// collectSas3ircu is fallback if storcli isn't available
func collectSas3ircu(data *SystemData) {
	c := cache.Global()
	cacheKey := "system:sas3ircu"

	if cached := c.Get(cacheKey); cached != nil {
		for k, v := range cached.(map[string]*HBADevice) {
			data.HBADevices[k] = v
		}
		return
	}

	out, err := exec.Command("sudo", "sas3ircu", "0", "display").CombinedOutput()
	if err != nil {
		return
	}

	devices := make(map[string]*HBADevice)
	output := string(out)
	lines := strings.Split(output, "\n")

	var current *HBADevice
	inDevices := false

	for _, line := range lines {
		line = strings.TrimSpace(line)

		if strings.HasPrefix(line, "Physical device information") {
			inDevices = true
			continue
		}

		if !inDevices {
			continue
		}

		if strings.HasPrefix(line, "Device is a") {
			if current != nil && current.Serial != "" {
				devices[strings.ToUpper(current.Serial)] = current
				data.HBADevices[strings.ToUpper(current.Serial)] = current
			}
			current = &HBADevice{ControllerID: "c0"}
			if strings.Contains(line, "Enclosure") {
				current = nil // Skip enclosure devices
			}
			continue
		}

		if current == nil {
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		switch key {
		case "Enclosure #":
			current.EnclosureID, _ = strconv.Atoi(val)
		case "Slot #":
			current.Slot, _ = strconv.Atoi(val)
		case "SAS Address":
			addr := strings.ReplaceAll(val, "-", "")
			current.SASAddress = &addr
		case "Serial No":
			current.Serial = val
		case "Model Number":
			current.Model = &val
		case "Manufacturer":
			current.Vendor = &val
		case "Firmware Revision":
			current.Firmware = &val
		case "Protocol":
			current.Protocol = &val
		case "Drive Type":
			current.MediaType = &val
		case "Size (in MB)/(in sectors)":
			sizeParts := strings.Split(val, "/")
			if len(sizeParts) >= 1 {
				if mb, err := strconv.ParseInt(sizeParts[0], 10, 64); err == nil {
					size := mb * 1024 * 1024
					current.SizeBytes = &size
				}
			}
		case "GUID":
			if val != "N/A" {
				// GUID can be used as WWN
			}
		}
	}

	// Don't forget last device
	if current != nil && current.Serial != "" {
		devices[strings.ToUpper(current.Serial)] = current
		data.HBADevices[strings.ToUpper(current.Serial)] = current
	}

	c.SetSlow(cacheKey, devices)
}

// trimPtr returns nil if string is empty or just whitespace, otherwise returns pointer to trimmed string
func trimPtr(s *string) *string {
	if s == nil {
		return nil
	}
	v := strings.TrimSpace(*s)
	if v == "" {
		return nil
	}
	return &v
}
