package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/sigreer/jbodgod/internal/config"
	"github.com/sigreer/jbodgod/internal/db"
	"github.com/sigreer/jbodgod/internal/drive"
	"github.com/sigreer/jbodgod/internal/hba"
	"github.com/sigreer/jbodgod/internal/zfs"
	"github.com/spf13/cobra"
)

// HealthcheckResult contains the complete health check output
type HealthcheckResult struct {
	Timestamp     time.Time           `json:"timestamp"`
	Status        string              `json:"status"` // healthy, warning, critical
	Drives        DriveHealthSummary  `json:"drives"`
	Pools         []PoolHealthSummary `json:"pools"`
	Alerts        []HealthAlert       `json:"alerts"`
	ScanDurationMs int64              `json:"scan_duration_ms"`
}

// DriveHealthSummary contains drive health statistics
type DriveHealthSummary struct {
	Expected  int      `json:"expected"`
	Present   int      `json:"present"`
	Active    int      `json:"active"`
	Standby   int      `json:"standby"`
	Missing   []string `json:"missing,omitempty"`
	Failed    []string `json:"failed,omitempty"`
	New       []string `json:"new,omitempty"`
	TempWarn  []string `json:"temp_warn,omitempty"`
}

// PoolHealthSummary contains ZFS pool health
type PoolHealthSummary struct {
	Name         string   `json:"name"`
	State        string   `json:"state"`
	ScanState    string   `json:"scan_state,omitempty"`
	FaultedVdevs []string `json:"faulted_vdevs,omitempty"`
	ErrorCount   int64    `json:"error_count"`
}

// HealthAlert represents a health check alert
type HealthAlert struct {
	Severity string `json:"severity"` // info, warning, critical
	Category string `json:"category"`
	Message  string `json:"message"`
	Details  any    `json:"details,omitempty"`
}

var healthcheckCmd = &cobra.Command{
	Use:   "healthcheck",
	Short: "Check drive and pool health",
	Long: `Perform a comprehensive health check:
  - Verify all expected drives are present
  - Check ZFS pool status for degraded/faulted states
  - Compare HBA roster against inventory
  - Report temperature warnings
  - Update inventory database (with --update)`,
	Run: runHealthcheck,
}

func init() {
	healthcheckCmd.Flags().Bool("json", false, "Output as JSON")
	healthcheckCmd.Flags().Bool("update", false, "Update inventory database with current state")
	healthcheckCmd.Flags().Int("temp-warn", 55, "Temperature warning threshold (°C)")
	healthcheckCmd.Flags().Int("temp-crit", 60, "Temperature critical threshold (°C)")
}

func runHealthcheck(cmd *cobra.Command, args []string) {
	start := time.Now()
	jsonOut, _ := cmd.Flags().GetBool("json")
	updateDB, _ := cmd.Flags().GetBool("update")
	tempWarn, _ := cmd.Flags().GetInt("temp-warn")
	tempCrit, _ := cmd.Flags().GetInt("temp-crit")

	result := &HealthcheckResult{
		Timestamp: start,
		Status:    "healthy",
	}

	// Open database (optional - we still run checks without it)
	database, dbErr := db.New(db.DefaultPath)
	if dbErr != nil && updateDB {
		fmt.Fprintf(os.Stderr, "Warning: could not open database: %v\n", dbErr)
	}
	if database != nil {
		defer database.Close()
	}

	// Load config
	cfg, err := config.Load(cfgFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not load config: %v\n", err)
	}

	// Get expected drives from config
	var expectedDrives []config.Drive
	if cfg != nil {
		expectedDrives = cfg.GetAllDrives()
	}
	result.Drives.Expected = len(expectedDrives)

	// Get current drive states
	var driveInfos []drive.DriveInfo
	if cfg != nil {
		driveInfos = drive.GetAll(cfg)
	}

	// Get HBA data
	var hbaDevices []hba.PhysicalDevice
	controllers := hba.ListControllers()
	for _, ctrlNum := range controllers {
		_, _, devices, err := hba.GetFullControllerInfo(fmt.Sprintf("c%d", ctrlNum), false)
		if err == nil {
			hbaDevices = append(hbaDevices, devices...)
		}
	}

	// Analyze drives
	hbaSerials := make(map[string]hba.PhysicalDevice)
	for _, dev := range hbaDevices {
		serial := dev.Serial
		if serial == "" {
			serial = dev.SerialVPD
		}
		if serial != "" {
			hbaSerials[serial] = dev
		}
	}

	// Track known serials from inventory
	var inventorySerials map[string]bool
	if database != nil {
		inventorySerials = make(map[string]bool)
		allDrives, _ := database.GetAllDrives()
		for _, d := range allDrives {
			inventorySerials[d.Serial] = true
		}
	}

	// Analyze drive states
	for _, d := range driveInfos {
		switch d.State {
		case "active":
			result.Drives.Active++
			result.Drives.Present++

			// Check temperature
			if d.Temp != nil {
				if *d.Temp >= tempCrit {
					result.Alerts = append(result.Alerts, HealthAlert{
						Severity: "critical",
						Category: "temperature",
						Message:  fmt.Sprintf("Drive %s temperature critical: %d°C", d.Device, *d.Temp),
						Details:  map[string]any{"device": d.Device, "temp": *d.Temp},
					})
					result.Drives.TempWarn = append(result.Drives.TempWarn, d.Device)
					result.Status = "critical"
				} else if *d.Temp >= tempWarn {
					result.Alerts = append(result.Alerts, HealthAlert{
						Severity: "warning",
						Category: "temperature",
						Message:  fmt.Sprintf("Drive %s temperature warning: %d°C", d.Device, *d.Temp),
						Details:  map[string]any{"device": d.Device, "temp": *d.Temp},
					})
					result.Drives.TempWarn = append(result.Drives.TempWarn, d.Device)
					if result.Status == "healthy" {
						result.Status = "warning"
					}
				}
			}

		case "standby":
			result.Drives.Standby++
			result.Drives.Present++

		case "missing":
			serial := "unknown"
			if d.Serial != nil {
				serial = *d.Serial
			}
			result.Drives.Missing = append(result.Drives.Missing, d.Device)
			result.Alerts = append(result.Alerts, HealthAlert{
				Severity: "critical",
				Category: "drive_missing",
				Message:  fmt.Sprintf("Drive %s is missing (serial: %s)", d.Device, serial),
				Details:  map[string]any{"device": d.Device, "serial": serial},
			})
			result.Status = "critical"

		case "failed":
			serial := "unknown"
			if d.Serial != nil {
				serial = *d.Serial
			}
			result.Drives.Failed = append(result.Drives.Failed, d.Device)
			result.Alerts = append(result.Alerts, HealthAlert{
				Severity: "critical",
				Category: "drive_failed",
				Message:  fmt.Sprintf("Drive %s has failed (serial: %s)", d.Device, serial),
				Details:  map[string]any{"device": d.Device, "serial": serial},
			})
			result.Status = "critical"
		}
	}

	// Check for new drives (in HBA but not in inventory)
	if database != nil && inventorySerials != nil {
		for serial := range hbaSerials {
			if !inventorySerials[serial] {
				result.Drives.New = append(result.Drives.New, serial)
				result.Alerts = append(result.Alerts, HealthAlert{
					Severity: "info",
					Category: "drive_new",
					Message:  fmt.Sprintf("New drive detected: %s", serial),
					Details:  map[string]any{"serial": serial},
				})
			}
		}
	}

	// Check ZFS pools
	poolHealths, err := zfs.GetAllPoolHealth()
	if err == nil {
		for _, pool := range poolHealths {
			summary := PoolHealthSummary{
				Name:       pool.Name,
				State:      pool.State,
				ScanState:  pool.ScanState,
				ErrorCount: pool.TotalErrors,
			}

			// Get faulted devices
			for _, faulted := range pool.GetFaultedDevices() {
				summary.FaultedVdevs = append(summary.FaultedVdevs, faulted.Name)
			}

			result.Pools = append(result.Pools, summary)

			// Generate alerts for pool issues
			if pool.State != zfs.StateOnline {
				result.Alerts = append(result.Alerts, HealthAlert{
					Severity: "critical",
					Category: "pool_degraded",
					Message:  fmt.Sprintf("ZFS pool %s is %s", pool.Name, pool.State),
					Details: map[string]any{
						"pool":    pool.Name,
						"state":   pool.State,
						"faulted": summary.FaultedVdevs,
					},
				})
				result.Status = "critical"
			} else if pool.TotalErrors > 0 {
				result.Alerts = append(result.Alerts, HealthAlert{
					Severity: "warning",
					Category: "pool_errors",
					Message:  fmt.Sprintf("ZFS pool %s has %d errors", pool.Name, pool.TotalErrors),
					Details:  map[string]any{"pool": pool.Name, "errors": pool.TotalErrors},
				})
				if result.Status == "healthy" {
					result.Status = "warning"
				}
			}
		}
	}

	result.ScanDurationMs = time.Since(start).Milliseconds()

	// Update database if requested
	if updateDB && database != nil {
		updateInventoryFromHealthcheck(database, hbaDevices, driveInfos)
	}

	// Save alerts to database
	if database != nil {
		for _, alert := range result.Alerts {
			database.CreateAlertWithDetails(alert.Severity, alert.Category, alert.Message, nil)
		}
	}

	// Output
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(result)
		return
	}

	// Text output
	printHealthcheckText(result)
}

func printHealthcheckText(result *HealthcheckResult) {
	statusSymbol := "✓"
	if result.Status == "warning" {
		statusSymbol = "⚠"
	} else if result.Status == "critical" {
		statusSymbol = "✗"
	}

	fmt.Printf("\n%s Health Check: %s\n", statusSymbol, strings.ToUpper(result.Status))
	fmt.Printf("  Timestamp: %s (took %dms)\n", result.Timestamp.Format("2006-01-02 15:04:05"), result.ScanDurationMs)
	fmt.Println()

	// Drives
	fmt.Println("Drives:")
	fmt.Printf("  Expected: %d | Present: %d | Active: %d | Standby: %d\n",
		result.Drives.Expected, result.Drives.Present, result.Drives.Active, result.Drives.Standby)

	if len(result.Drives.Missing) > 0 {
		fmt.Printf("  ✗ Missing: %s\n", strings.Join(result.Drives.Missing, ", "))
	}
	if len(result.Drives.Failed) > 0 {
		fmt.Printf("  ✗ Failed: %s\n", strings.Join(result.Drives.Failed, ", "))
	}
	if len(result.Drives.TempWarn) > 0 {
		fmt.Printf("  ⚠ Temperature warnings: %s\n", strings.Join(result.Drives.TempWarn, ", "))
	}
	if len(result.Drives.New) > 0 {
		fmt.Printf("  + New drives: %s\n", strings.Join(result.Drives.New, ", "))
	}
	fmt.Println()

	// Pools
	if len(result.Pools) > 0 {
		fmt.Println("ZFS Pools:")
		for _, pool := range result.Pools {
			symbol := "✓"
			if pool.State != "ONLINE" {
				symbol = "✗"
			} else if pool.ErrorCount > 0 {
				symbol = "⚠"
			}

			fmt.Printf("  %s %s: %s", symbol, pool.Name, pool.State)
			if pool.ErrorCount > 0 {
				fmt.Printf(" (%d errors)", pool.ErrorCount)
			}
			if pool.ScanState != "" && pool.ScanState != "none" {
				fmt.Printf(" [%s]", pool.ScanState)
			}
			fmt.Println()

			if len(pool.FaultedVdevs) > 0 {
				fmt.Printf("    Faulted: %s\n", strings.Join(pool.FaultedVdevs, ", "))
			}
		}
		fmt.Println()
	}

	// Alerts summary
	if len(result.Alerts) > 0 {
		critCount := 0
		warnCount := 0
		for _, a := range result.Alerts {
			if a.Severity == "critical" {
				critCount++
			} else if a.Severity == "warning" {
				warnCount++
			}
		}
		fmt.Printf("Alerts: %d critical, %d warnings\n", critCount, warnCount)
	}
}

func updateInventoryFromHealthcheck(database *db.DB, hbaDevices []hba.PhysicalDevice, driveInfos []drive.DriveInfo) {
	// Build map of drive info by serial
	driveByDevice := make(map[string]drive.DriveInfo)
	for _, d := range driveInfos {
		driveByDevice[d.Device] = d
	}

	var wg sync.WaitGroup
	for _, dev := range hbaDevices {
		wg.Add(1)
		go func(device hba.PhysicalDevice) {
			defer wg.Done()

			serial := device.Serial
			if serial == "" {
				serial = device.SerialVPD
			}
			if serial == "" {
				return
			}

			record := &db.DriveRecord{
				Serial:       serial,
				SerialVPD:    device.SerialVPD,
				Model:        device.Model,
				Manufacturer: device.Manufacturer,
				Firmware:     device.Firmware,
				Protocol:     device.Protocol,
				DriveType:    device.DriveType,
				SASAddress:   device.SASAddress,
				CurrentState: db.StateActive,
			}

			if device.EnclosureID >= 0 {
				enc := device.EnclosureID
				record.EnclosureID = &enc
			}
			if device.Slot >= 0 {
				sl := device.Slot
				record.Slot = &sl
			}

			database.UpsertDrive(record)
		}(dev)
	}
	wg.Wait()
}
