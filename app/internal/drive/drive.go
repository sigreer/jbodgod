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

	"github.com/sigreer/jbodgod/internal/config"
)

type DriveInfo struct {
	Device   string  `json:"device"`
	Name     string  `json:"name,omitempty"`
	State    string  `json:"state"`
	Temp     *int    `json:"temp"`
	Serial   *string `json:"serial"`
	LUID     *string `json:"luid"`
	SCSIAddr *string `json:"scsi_addr"`
	Zpool    *string `json:"zpool"`
	Vdev     *string `json:"vdev"`
	Model    *string `json:"model"`
}

type Summary struct {
	Active   int  `json:"active"`
	Standby  int  `json:"standby"`
	TempMin  *int `json:"temp_min"`
	TempMax  *int `json:"temp_max"`
	TempAvg  *int `json:"temp_avg"`
}

type Output struct {
	Drives  []DriveInfo `json:"drives"`
	Summary Summary     `json:"summary"`
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
	out, _ := exec.Command("smartctl", "-i", "-n", "standby", d.Device).CombinedOutput()
	output := string(out)

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

func PrintJSON(drives []DriveInfo) {
	var active, standby int
	var temps []int

	for _, d := range drives {
		if d.State == "active" {
			active++
			if d.Temp != nil {
				temps = append(temps, *d.Temp)
			}
		} else {
			standby++
		}
	}

	summary := Summary{
		Active:  active,
		Standby: standby,
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
		Drives:  drives,
		Summary: summary,
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

func Monitor(cfg *config.Config, interval int) {
	for {
		// Clear screen
		fmt.Print("\033[H\033[2J")
		fmt.Println("=== JBOD Drive Monitor === (Ctrl+C to exit)")
		fmt.Printf("Refreshing every %ds | %s\n\n", interval, time.Now().Format("2006-01-02 15:04:05"))

		drives := GetAll(cfg)

		fmt.Printf("%-10s %-10s %-8s %s\n", "DRIVE", "STATE", "TEMP", "STATUS")
		fmt.Println("-------------------------------------------")

		var active, standby int
		var temps []int

		for _, d := range drives {
			temp := "-"
			status := "💤"

			if d.State == "active" {
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
				}
			} else {
				standby++
			}

			fmt.Printf("%-10s %-10s %-8s %s\n", d.Device, strings.ToUpper(d.State), temp, status)
		}

		fmt.Println("\n-------------------------------------------")
		fmt.Printf("Active: %d | Standby: %d\n", active, standby)

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
			fmt.Printf("Temps: Min %d°C | Max %d°C | Avg %d°C\n", min, max, avg)
		}

		time.Sleep(time.Duration(interval) * time.Second)
	}
}
