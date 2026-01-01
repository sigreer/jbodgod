package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/sigreer/jbodgod/internal/config"
	"github.com/sigreer/jbodgod/internal/db"
	"github.com/sigreer/jbodgod/internal/hba"
	"github.com/spf13/cobra"
)

var inventoryCmd = &cobra.Command{
	Use:   "inventory",
	Short: "Manage drive inventory database",
	Long: `Manage the persistent drive inventory database.

The inventory tracks all drives that have been seen, their locations,
and state history. This enables locating failed/missing drives.`,
}

var inventoryListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all known drives",
	Run:   runInventoryList,
}

var inventorySyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync current drive state to inventory",
	Long: `Scan current system state and update the inventory database.

This command:
  - Queries the HBA for all connected drives
  - Gets drive info via smartctl
  - Updates or creates inventory records
  - Records state change events`,
	Run: runInventorySync,
}

var inventoryShowCmd = &cobra.Command{
	Use:   "show <serial>",
	Short: "Show drive details and history",
	Args:  cobra.ExactArgs(1),
	Run:   runInventoryShow,
}

var inventoryEventsCmd = &cobra.Command{
	Use:   "events",
	Short: "Show recent drive events",
	Run:   runInventoryEvents,
}

var inventoryAlertsCmd = &cobra.Command{
	Use:   "alerts",
	Short: "Show and manage alerts",
	Run:   runInventoryAlerts,
}

func init() {
	inventoryCmd.AddCommand(inventoryListCmd)
	inventoryCmd.AddCommand(inventorySyncCmd)
	inventoryCmd.AddCommand(inventoryShowCmd)
	inventoryCmd.AddCommand(inventoryEventsCmd)
	inventoryCmd.AddCommand(inventoryAlertsCmd)

	// Add flags
	inventoryListCmd.Flags().Bool("json", false, "Output as JSON")
	inventoryListCmd.Flags().String("state", "", "Filter by state (active, missing, failed)")
	inventoryListCmd.Flags().String("pool", "", "Filter by ZFS pool name")

	inventorySyncCmd.Flags().Bool("verbose", false, "Show detailed sync progress")

	inventoryEventsCmd.Flags().Int("limit", 50, "Maximum number of events to show")
	inventoryEventsCmd.Flags().String("type", "", "Filter by event type")

	inventoryAlertsCmd.Flags().Bool("ack-all", false, "Acknowledge all alerts")
	inventoryAlertsCmd.Flags().Int64("ack", 0, "Acknowledge specific alert by ID")
}

func openDB() (*db.DB, error) {
	dbPath := db.DefaultPath
	// Could add config option for custom path
	return db.New(dbPath)
}

func runInventoryList(cmd *cobra.Command, args []string) {
	database, err := openDB()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening database: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	jsonOut, _ := cmd.Flags().GetBool("json")
	stateFilter, _ := cmd.Flags().GetString("state")
	poolFilter, _ := cmd.Flags().GetString("pool")

	var drives []*db.DriveRecord

	if stateFilter != "" {
		drives, err = database.GetDrivesByState(stateFilter)
	} else if poolFilter != "" {
		drives, err = database.GetDrivesByPool(poolFilter)
	} else {
		drives, err = database.GetAllDrives()
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error querying drives: %v\n", err)
		os.Exit(1)
	}

	if len(drives) == 0 {
		fmt.Println("No drives in inventory. Run 'jbodgod inventory sync' to populate.")
		return
	}

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(drives)
		return
	}

	// Table output
	fmt.Printf("%-20s %-8s %-10s %-12s %-15s %s\n", "SERIAL", "ENC:SLOT", "STATE", "DEVICE", "ZPOOL", "MODEL")
	fmt.Println(strings.Repeat("-", 85))

	for _, d := range drives {
		slot := "-"
		if d.EnclosureID != nil && d.Slot != nil {
			slot = fmt.Sprintf("%d:%d", *d.EnclosureID, *d.Slot)
		}

		device := d.DevicePath
		if device == "" {
			device = "-"
		} else if len(device) > 12 {
			device = device[len(device)-12:]
		}

		pool := d.ZpoolName
		if pool == "" {
			pool = "-"
		}

		model := d.Model
		if len(model) > 20 {
			model = model[:20] + "..."
		}

		fmt.Printf("%-20s %-8s %-10s %-12s %-15s %s\n",
			d.Serial, slot, strings.ToUpper(d.CurrentState), device, pool, model)
	}

	// Summary
	total, active, missing, failed, _ := database.DriveCount()
	fmt.Println(strings.Repeat("-", 85))
	fmt.Printf("Total: %d | Active: %d | Missing: %d | Failed: %d\n", total, active, missing, failed)
}

func runInventorySync(cmd *cobra.Command, args []string) {
	verbose, _ := cmd.Flags().GetBool("verbose")

	database, err := openDB()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening database: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	cfg, err := config.Load(cfgFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not load config: %v\n", err)
	}

	if verbose {
		fmt.Println("Scanning HBA controllers...")
	}

	// Get HBA data
	controllers := hba.ListControllers()
	var allDevices []hba.PhysicalDevice

	for _, ctrlNum := range controllers {
		_, _, devices, err := hba.GetFullControllerInfo(fmt.Sprintf("c%d", ctrlNum), true)
		if err != nil {
			if verbose {
				fmt.Printf("  Warning: controller %d: %v\n", ctrlNum, err)
			}
			continue
		}
		allDevices = append(allDevices, devices...)
	}

	if verbose {
		fmt.Printf("Found %d devices from HBA\n", len(allDevices))
		fmt.Println("Syncing to database...")
	}

	// Also get config drives for state checking
	configDrives := make(map[string]config.Drive)
	if cfg != nil {
		for _, d := range cfg.GetAllDrives() {
			configDrives[d.Device] = d
		}
	}

	// Sync each device (sequential to avoid SQLite lock issues)
	var updated, created int

	for _, device := range allDevices {
		serial := device.Serial
		if serial == "" {
			serial = device.SerialVPD
		}
		if serial == "" {
			continue // Skip devices without serial
		}

		// Check if exists
		existing, _ := database.GetDriveBySerial(serial)
		isNew := existing == nil

		// Build record
		record := &db.DriveRecord{
			Serial:       serial,
			SerialVPD:    device.SerialVPD,
			Model:        device.Model,
			Manufacturer: device.Manufacturer,
			Firmware:     device.Firmware,
			Protocol:     device.Protocol,
			DriveType:    device.DriveType,
			SASAddress:   device.SASAddress,
			CurrentState: db.StateActive, // Device is present in HBA
		}

		if device.EnclosureID >= 0 {
			enc := device.EnclosureID
			record.EnclosureID = &enc
		}
		if device.Slot >= 0 {
			sl := device.Slot
			record.Slot = &sl
		}

		// Upsert
		if err := database.UpsertDrive(record); err != nil {
			if verbose {
				fmt.Printf("  Error syncing %s: %v\n", serial, err)
			}
			continue
		}

		if isNew {
			created++
			// Record discovery event
			database.RecordEvent(record.ID, db.EventDiscovered, "", db.StateActive, "", nil)
		} else {
			updated++
			// Check for state change
			if existing.CurrentState != db.StateActive {
				database.RecordEvent(record.ID, db.EventOnline, existing.CurrentState, db.StateActive, "", nil)
			}
		}

		if verbose {
			action := "updated"
			if isNew {
				action = "created"
			}
			fmt.Printf("  %s: %s (enc:%d slot:%d)\n", action, serial, device.EnclosureID, device.Slot)
		}
	}

	// Check for missing drives (in DB but not in HBA)
	allDrives, _ := database.GetAllDrives()
	hbaSerials := make(map[string]bool)
	for _, dev := range allDevices {
		serial := dev.Serial
		if serial == "" {
			serial = dev.SerialVPD
		}
		if serial != "" {
			hbaSerials[serial] = true
		}
	}

	var missing int
	for _, drive := range allDrives {
		if !hbaSerials[drive.Serial] && drive.CurrentState == db.StateActive {
			// Drive was active but no longer in HBA - mark as missing
			database.UpdateDriveState(drive.Serial, db.StateMissing, true)
			missing++
			if verbose {
				fmt.Printf("  marked missing: %s\n", drive.Serial)
			}
		}
	}

	fmt.Printf("Sync complete: %d created, %d updated, %d marked missing\n", created, updated, missing)
}

func runInventoryShow(cmd *cobra.Command, args []string) {
	database, err := openDB()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening database: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	serial := args[0]
	drive, err := database.GetDriveBySerial(serial)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if drive == nil {
		fmt.Fprintf(os.Stderr, "Drive not found: %s\n", serial)
		os.Exit(1)
	}

	// Print drive info
	fmt.Printf("Drive: %s\n", drive.Serial)
	fmt.Println(strings.Repeat("-", 40))
	fmt.Printf("  Model:        %s\n", drive.Model)
	fmt.Printf("  Manufacturer: %s\n", drive.Manufacturer)
	fmt.Printf("  Firmware:     %s\n", drive.Firmware)
	fmt.Printf("  Protocol:     %s\n", drive.Protocol)
	fmt.Printf("  Type:         %s\n", drive.DriveType)
	fmt.Println()

	if drive.EnclosureID != nil && drive.Slot != nil {
		fmt.Printf("  Location:     Enclosure %d, Slot %d\n", *drive.EnclosureID, *drive.Slot)
	}
	fmt.Printf("  Device:       %s\n", drive.DevicePath)
	fmt.Printf("  SAS Address:  %s\n", drive.SASAddress)
	fmt.Println()

	if drive.ZpoolName != "" {
		fmt.Printf("  ZFS Pool:     %s\n", drive.ZpoolName)
		fmt.Printf("  Vdev Type:    %s\n", drive.VdevType)
	}
	fmt.Println()

	fmt.Printf("  State:        %s\n", strings.ToUpper(drive.CurrentState))
	fmt.Printf("  First Seen:   %s\n", drive.FirstSeen.Format("2006-01-02 15:04:05"))
	fmt.Printf("  Last Seen:    %s\n", drive.LastSeen.Format("2006-01-02 15:04:05"))

	// Show recent events
	events, err := database.GetDriveEvents(drive.ID, 10)
	if err == nil && len(events) > 0 {
		fmt.Println()
		fmt.Println("Recent Events:")
		fmt.Println(strings.Repeat("-", 40))
		for _, e := range events {
			fmt.Printf("  %s  %-12s  %s -> %s\n",
				e.Timestamp.Format("2006-01-02 15:04"),
				e.EventType,
				e.OldState, e.NewState)
		}
	}
}

func runInventoryEvents(cmd *cobra.Command, args []string) {
	database, err := openDB()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening database: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	limit, _ := cmd.Flags().GetInt("limit")
	eventType, _ := cmd.Flags().GetString("type")

	var events []*db.DriveEvent

	if eventType != "" {
		events, err = database.GetEventsByType(eventType, limit)
	} else {
		events, err = database.GetRecentEvents(limit)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if len(events) == 0 {
		fmt.Println("No events found.")
		return
	}

	fmt.Printf("%-20s %-12s %-10s %-10s %-8s %s\n", "TIMESTAMP", "TYPE", "OLD", "NEW", "SLOT", "DEVICE")
	fmt.Println(strings.Repeat("-", 80))

	for _, e := range events {
		slot := "-"
		if e.EnclosureID != nil && e.Slot != nil {
			slot = fmt.Sprintf("%d:%d", *e.EnclosureID, *e.Slot)
		}
		device := e.DevicePath
		if device == "" {
			device = "-"
		}

		fmt.Printf("%-20s %-12s %-10s %-10s %-8s %s\n",
			e.Timestamp.Format("2006-01-02 15:04:05"),
			e.EventType,
			e.OldState, e.NewState,
			slot, device)
	}
}

func runInventoryAlerts(cmd *cobra.Command, args []string) {
	database, err := openDB()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening database: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	// Handle acknowledgment
	ackAll, _ := cmd.Flags().GetBool("ack-all")
	ackID, _ := cmd.Flags().GetInt64("ack")

	if ackAll {
		count, err := database.AcknowledgeAllAlerts()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Acknowledged %d alerts\n", count)
		return
	}

	if ackID > 0 {
		err := database.AcknowledgeAlert(ackID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Acknowledged alert %d\n", ackID)
		return
	}

	// Show alerts
	alerts, err := database.GetUnacknowledgedAlerts()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if len(alerts) == 0 {
		fmt.Println("No unacknowledged alerts.")
		return
	}

	fmt.Printf("%-5s %-10s %-15s %-8s %s\n", "ID", "SEVERITY", "CATEGORY", "SLOT", "MESSAGE")
	fmt.Println(strings.Repeat("-", 80))

	for _, a := range alerts {
		slot := "-"
		if a.EnclosureID != nil && a.Slot != nil {
			slot = fmt.Sprintf("%d:%d", *a.EnclosureID, *a.Slot)
		}

		fmt.Printf("%-5d %-10s %-15s %-8s %s\n",
			a.ID, strings.ToUpper(a.Severity), a.Category, slot, a.Message)
	}
}
