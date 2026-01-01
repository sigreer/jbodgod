package drive

import (
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
	"github.com/sigreer/jbodgod/internal/config"
	"github.com/sigreer/jbodgod/internal/hba"
)

type DriveInfo struct {
	Device    string  `json:"device"`
	Name      string  `json:"name,omitempty"`
	State     string  `json:"state"`
	Temp      *int    `json:"temp"`
	Serial    *string `json:"serial"`
	LUID      *string `json:"luid"`
	SCSIAddr  *string `json:"scsi_addr"`
	Zpool     *string `json:"zpool"`
	Vdev      *string `json:"vdev"`
	Model     *string `json:"model"`
	Enclosure *int    `json:"enclosure,omitempty"`
	Slot      *int    `json:"slot,omitempty"`
}

type Summary struct {
	Active  int  `json:"active"`
	Standby int  `json:"standby"`
	Missing int  `json:"missing"`
	Failed  int  `json:"failed"`
	TempMin *int `json:"temp_min"`
	TempMax *int `json:"temp_max"`
	TempAvg *int `json:"temp_avg"`
}

type Output struct {
	Drives      []DriveInfo         `json:"drives"`
	Summary     Summary             `json:"summary"`
	Controllers []hba.ControllerInfo `json:"controllers,omitempty"`
	Enclosures  []hba.EnclosureInfo  `json:"enclosures,omitempty"`
}

func GetAll(cfg *config.Config) []DriveInfo {
	drives := cfg.GetAllDrives()
	results := make([]DriveInfo, len(drives))
	var wg sync.WaitGroup

	for i, d := range drives {
		wg.Add(1)
		go func(idx int, drv config.Drive) {
			defer wg.Done()
			results[idx] = getInfo(drv)
		}(i, d)
	}

	wg.Wait()
	return results
}

func getInfo(d config.Drive) DriveInfo {
	info := DriveInfo{
		Device: d.Device,
		Name:   d.Name,
	}

	// Check state
	out, err := exec.Command("smartctl", "-i", "-n", "standby", d.Device).CombinedOutput()
	output := string(out)

	// Check for device access failures first
	if err != nil {
		// Device doesn't exist or can't be opened
		if strings.Contains(output, "No such device") ||
			strings.Contains(output, "No such file") {
			info.State = "missing"
			return info
		}
		// Device exists but failed to respond (I/O error, etc.)
		if strings.Contains(output, "I/O error") ||
			strings.Contains(output, "failed") {
			info.State = "failed"
			return info
		}
	}

	if strings.Contains(output, "NOT READY") {
		info.State = "standby"
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

func PrintStatus(drives []DriveInfo) {
	fmt.Printf("%-10s %-10s %s\n", "DRIVE", "STATE", "TEMP")
	fmt.Println("------------------------------")

	for _, d := range drives {
		temp := "-"
		if d.Temp != nil {
			temp = fmt.Sprintf("%d°C", *d.Temp)
		}
		state := strings.ToUpper(d.State)
		fmt.Printf("%-10s %-10s %s\n", d.Device, state, temp)
	}
}

func PrintJSON(drives []DriveInfo, controllers []hba.ControllerInfo, enclosures []hba.EnclosureInfo) {
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
			// Unknown state, count as failed
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

	output := Output{
		Drives:      drives,
		Summary:     summary,
		Controllers: controllers,
		Enclosures:  enclosures,
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(output)
}

func Spindown(cfg *config.Config) {
	drives := cfg.GetAllDrives()
	fmt.Println("Spinning down drives...")

	var wg sync.WaitGroup
	for _, d := range drives {
		wg.Add(1)
		go func(device string) {
			defer wg.Done()
			exec.Command("sdparm", "--command=stop", device).Run()
		}(d.Device)
	}
	wg.Wait()

	// Monitor progress
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
		if stopped == len(drives) {
			break
		}
	}
	fmt.Println("\nAll drives in standby.")
}

func Spinup(cfg *config.Config) {
	drives := cfg.GetAllDrives()
	fmt.Println("Spinning up drives...")

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

	// Check for device access failures first
	if err != nil {
		if strings.Contains(output, "No such device") ||
			strings.Contains(output, "No such file") {
			state = "missing"
		} else if strings.Contains(output, "I/O error") ||
			strings.Contains(output, "failed") {
			state = "failed"
		} else {
			// Unknown error, mark as failed
			state = "failed"
		}
	} else if strings.Contains(output, "NOT READY") {
		state = "standby"
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
