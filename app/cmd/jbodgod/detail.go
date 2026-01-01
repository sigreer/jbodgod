package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/sigreer/jbodgod/internal/hba"
	"github.com/spf13/cobra"
)

var detailCmd = &cobra.Command{
	Use:   "detail <item> [query]",
	Short: "Query details about controllers, enclosures, and devices",
	Long: `Query specific details about RAID controllers, enclosures, and devices.

Controller queries:
  detail c0                - Show all controller info
  detail c0 temperature    - Get controller temperature
  detail c0 devices        - List attached devices
  detail c0 enclosures     - List attached enclosures

Device queries:
  detail 2:5               - Show device at enclosure 2, slot 5
  detail e2:5              - Same as above (e prefix optional)
  detail serial:ZA1DKJT7   - Look up device by serial number

Examples:
  jbodgod detail c0
  jbodgod detail c0 temp
  jbodgod detail 2:5
  jbodgod detail c0 --json`,
	Args: cobra.RangeArgs(1, 2),
	Run:  runDetail,
}

func init() {
	detailCmd.Flags().Bool("raw", false, "Output raw value only (no formatting)")
	detailCmd.Flags().Bool("json", false, "Output as JSON")
	detailCmd.Flags().Bool("refresh", false, "Force refresh cached data")
}

func runDetail(cmd *cobra.Command, args []string) {
	item := args[0]
	query := ""
	if len(args) > 1 {
		query = strings.ToLower(args[1])
	}

	raw, _ := cmd.Flags().GetBool("raw")
	jsonOut, _ := cmd.Flags().GetBool("json")
	refresh, _ := cmd.Flags().GetBool("refresh")

	// Parse item type
	if strings.HasPrefix(item, "c") && len(item) >= 2 {
		// Controller query (c0, c1, etc.)
		handleControllerQuery(item, query, raw, jsonOut, refresh)
	} else if strings.Contains(item, ":") {
		// Device by enclosure:slot (e2:5 or 2:5)
		handleDeviceBySlot(item, query, raw, jsonOut, refresh)
	} else if strings.HasPrefix(strings.ToLower(item), "serial:") {
		// Device by serial
		handleDeviceBySerial(item[7:], query, raw, jsonOut, refresh)
	} else {
		fmt.Fprintf(os.Stderr, "Unknown item type '%s'\n", item)
		fmt.Fprintln(os.Stderr, "Supported formats:")
		fmt.Fprintln(os.Stderr, "  c0, c1, ...     - Controllers")
		fmt.Fprintln(os.Stderr, "  2:5, e2:5       - Device by enclosure:slot")
		fmt.Fprintln(os.Stderr, "  serial:ABC123   - Device by serial number")
		os.Exit(1)
	}
}

func handleControllerQuery(controller, query string, raw, jsonOut, refresh bool) {
	switch query {
	case "":
		// Show all controller info
		showControllerInfo(controller, jsonOut, refresh)
	case "temperature", "temp":
		showControllerTemperature(controller, raw, jsonOut)
	case "devices", "disks", "drives":
		showControllerDevices(controller, jsonOut, refresh)
	case "enclosures", "enc":
		showControllerEnclosures(controller, jsonOut, refresh)
	default:
		fmt.Fprintf(os.Stderr, "Unknown query '%s' for controller\n", query)
		fmt.Fprintln(os.Stderr, "Supported queries: temperature, devices, enclosures (or none for all info)")
		os.Exit(1)
	}
}

func showControllerInfo(controllerID string, jsonOut, refresh bool) {
	ctrl, enclosures, devices, err := hba.GetFullControllerInfo(controllerID, refresh)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Get fresh temperature
	if temp, err := hba.FetchControllerTemperature(controllerID); err == nil {
		ctrl.Temperature = temp
	}

	if jsonOut {
		output := map[string]interface{}{
			"controller": ctrl,
			"enclosures": enclosures,
			"device_count": len(devices),
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(output)
		return
	}

	// Pretty print
	fmt.Printf("Controller %s\n", controllerID)
	fmt.Println(strings.Repeat("=", 50))

	fmt.Println("\nIdentification:")
	fmt.Printf("  Type:           %s\n", ctrl.Type)
	if ctrl.Model != "" {
		fmt.Printf("  Model:          %s\n", ctrl.Model)
	}
	if ctrl.Serial != "" {
		fmt.Printf("  Serial:         %s\n", ctrl.Serial)
	}
	if ctrl.SASAddress != "" {
		fmt.Printf("  SAS Address:    %s\n", ctrl.SASAddress)
	}

	fmt.Println("\nFirmware:")
	fmt.Printf("  Firmware:       %s\n", ctrl.FirmwareVersion)
	fmt.Printf("  BIOS:           %s\n", ctrl.BIOSVersion)
	if ctrl.DriverName != "" {
		fmt.Printf("  Driver:         %s (%s)\n", ctrl.DriverName, ctrl.DriverVersion)
	}
	if ctrl.NVDataVersion != "" {
		fmt.Printf("  NVDATA:         %s\n", ctrl.NVDataVersion)
	}

	fmt.Println("\nPCI:")
	if ctrl.PCIAddress != "" {
		fmt.Printf("  Address:        %s\n", ctrl.PCIAddress)
	} else {
		fmt.Printf("  Bus/Dev/Func:   %d/%d/%d\n", ctrl.PCIBus, ctrl.PCIDevice, ctrl.PCIFunction)
	}
	if ctrl.PCIVendorID != "" {
		fmt.Printf("  Vendor/Device:  %s / %s\n", ctrl.PCIVendorID, ctrl.PCIDeviceID)
	}

	fmt.Println("\nCapabilities:")
	fmt.Printf("  Max Devices:    %d\n", ctrl.MaxPhysicalDevices)
	fmt.Printf("  Concurrent Cmd: %d\n", ctrl.ConcurrentCommands)
	if ctrl.SupportedDrives != "" {
		fmt.Printf("  Drive Types:    %s\n", ctrl.SupportedDrives)
	}
	if ctrl.PhyCount > 0 {
		fmt.Printf("  PHY Count:      %d\n", ctrl.PhyCount)
	}
	fmt.Printf("  RAID Support:   %v\n", ctrl.RAIDSupport)

	fmt.Println("\nStatus:")
	if ctrl.Temperature != nil {
		status := "OK"
		if *ctrl.Temperature >= 80 {
			status = "HOT"
		} else if *ctrl.Temperature >= 70 {
			status = "WARM"
		}
		fmt.Printf("  Temperature:    %d°C (%s)\n", *ctrl.Temperature, status)
	}

	fmt.Printf("\nAttached: %d enclosure(s), %d device(s)\n", len(enclosures), len(devices))
}

func showControllerTemperature(controllerID string, raw, jsonOut bool) {
	temp, err := hba.FetchControllerTemperature(controllerID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if temp == nil {
		fmt.Fprintf(os.Stderr, "Could not read temperature for %s\n", controllerID)
		os.Exit(1)
	}

	if jsonOut {
		json.NewEncoder(os.Stdout).Encode(map[string]int{"temperature": *temp})
		return
	}

	if raw {
		fmt.Println(*temp)
	} else {
		fmt.Printf("Controller %s temperature: %d°C\n", controllerID, *temp)
	}
}

func showControllerDevices(controllerID string, jsonOut, refresh bool) {
	_, _, devices, err := hba.GetFullControllerInfo(controllerID, refresh)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(devices)
		return
	}

	fmt.Printf("Devices attached to %s\n", controllerID)
	fmt.Println(strings.Repeat("=", 80))
	fmt.Printf("%-6s %-6s %-12s %-18s %-10s %s\n",
		"ENC", "SLOT", "SERIAL", "MODEL", "SIZE", "STATE")
	fmt.Println(strings.Repeat("-", 80))

	for _, d := range devices {
		size := fmt.Sprintf("%d GB", d.SizeMB/1024)
		if d.SizeMB/1024 >= 1000 {
			size = fmt.Sprintf("%.1f TB", float64(d.SizeMB)/1024/1024)
		}
		fmt.Printf("%-6d %-6d %-12s %-18s %-10s %s\n",
			d.EnclosureID, d.Slot, d.Serial, d.Model, size, d.State)
	}
	fmt.Printf("\nTotal: %d devices\n", len(devices))
}

func showControllerEnclosures(controllerID string, jsonOut, refresh bool) {
	_, enclosures, _, err := hba.GetFullControllerInfo(controllerID, refresh)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(enclosures)
		return
	}

	fmt.Printf("Enclosures attached to %s\n", controllerID)
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("%-6s %-20s %-8s %s\n", "ID", "LOGICAL ID", "SLOTS", "START")
	fmt.Println(strings.Repeat("-", 60))

	for _, e := range enclosures {
		fmt.Printf("%-6d %-20s %-8d %d\n",
			e.ID, e.LogicalID, e.NumSlots, e.StartSlot)
	}
}

func handleDeviceBySlot(item, query string, raw, jsonOut, refresh bool) {
	// Parse enclosure:slot (e2:5 or 2:5)
	item = strings.TrimPrefix(strings.ToLower(item), "e")
	parts := strings.Split(item, ":")
	if len(parts) != 2 {
		fmt.Fprintf(os.Stderr, "Invalid slot format '%s', use enclosure:slot (e.g., 2:5)\n", item)
		os.Exit(1)
	}

	enclosure, err1 := strconv.Atoi(parts[0])
	slot, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		fmt.Fprintf(os.Stderr, "Invalid slot format '%s'\n", item)
		os.Exit(1)
	}

	dev := hba.GetDeviceBySlot(enclosure, slot)
	if dev == nil {
		fmt.Fprintf(os.Stderr, "No device found at enclosure %d, slot %d\n", enclosure, slot)
		os.Exit(1)
	}

	printDevice(dev, query, raw, jsonOut)
}

func handleDeviceBySerial(serial, query string, raw, jsonOut, refresh bool) {
	dev := hba.GetDeviceBySerial(serial)
	if dev == nil {
		fmt.Fprintf(os.Stderr, "No device found with serial '%s'\n", serial)
		os.Exit(1)
	}

	printDevice(dev, query, raw, jsonOut)
}

func printDevice(dev *hba.PhysicalDevice, query string, raw, jsonOut bool) {
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(dev)
		return
	}

	// If specific query, return just that field
	if query != "" {
		val := getDeviceField(dev, query)
		if val == "" {
			fmt.Fprintf(os.Stderr, "Unknown field '%s'\n", query)
			os.Exit(1)
		}
		if raw {
			fmt.Println(val)
		} else {
			fmt.Printf("%s: %s\n", query, val)
		}
		return
	}

	// Full device info
	fmt.Printf("Device at Enclosure %d, Slot %d\n", dev.EnclosureID, dev.Slot)
	fmt.Println(strings.Repeat("=", 50))

	fmt.Println("\nIdentification:")
	fmt.Printf("  Serial:         %s\n", dev.Serial)
	if dev.SerialVPD != "" {
		fmt.Printf("  Serial (VPD):   %s\n", dev.SerialVPD)
	}
	fmt.Printf("  Manufacturer:   %s\n", dev.Manufacturer)
	fmt.Printf("  Model:          %s\n", dev.Model)
	fmt.Printf("  Firmware:       %s\n", dev.Firmware)

	fmt.Println("\nConnectivity:")
	fmt.Printf("  SAS Address:    %s\n", dev.SASAddress)
	if dev.GUID != "" {
		fmt.Printf("  GUID:           %s\n", dev.GUID)
	}
	fmt.Printf("  Protocol:       %s\n", dev.Protocol)
	fmt.Printf("  Drive Type:     %s\n", dev.DriveType)

	fmt.Println("\nCapacity:")
	sizeGB := dev.SizeMB / 1024
	if sizeGB >= 1000 {
		fmt.Printf("  Size:           %.2f TB\n", float64(dev.SizeMB)/1024/1024)
	} else {
		fmt.Printf("  Size:           %d GB\n", sizeGB)
	}
	fmt.Printf("  Sectors:        %d\n", dev.Sectors)

	fmt.Println("\nStatus:")
	fmt.Printf("  State:          %s\n", dev.State)
}

func getDeviceField(dev *hba.PhysicalDevice, field string) string {
	switch strings.ToLower(field) {
	case "serial":
		return dev.Serial
	case "model":
		return dev.Model
	case "manufacturer", "mfg":
		return dev.Manufacturer
	case "firmware", "fw":
		return dev.Firmware
	case "sas", "sas_address":
		return dev.SASAddress
	case "guid":
		return dev.GUID
	case "protocol":
		return dev.Protocol
	case "type", "drive_type":
		return dev.DriveType
	case "state":
		return dev.State
	case "slot":
		return strconv.Itoa(dev.Slot)
	case "enclosure", "enc":
		return strconv.Itoa(dev.EnclosureID)
	case "size":
		return fmt.Sprintf("%d MB", dev.SizeMB)
	default:
		return ""
	}
}
