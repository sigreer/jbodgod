package identify

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// PrintJSON outputs the lookup result as JSON
func PrintJSON(w io.Writer, result *LookupResult) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}

// PrintTable outputs the lookup result as a formatted table
func PrintTable(w io.Writer, result *LookupResult) {
	fmt.Fprintf(w, "Query:      %s\n", result.Query)
	fmt.Fprintf(w, "Matched As: %s\n", result.MatchedAs)
	if result.Device.DevicePath != "" {
		fmt.Fprintf(w, "Device:     %s\n", result.Device.DevicePath)
	}
	fmt.Fprintln(w)

	fmt.Fprintf(w, "%-20s %s\n", "IDENTIFIER", "VALUE")
	fmt.Fprintln(w, strings.Repeat("-", 60))

	e := result.Device

	// Core identifiers
	printField(w, "Type", string(e.Type))
	printField(w, "Device Path", e.DevicePath)
	printField(w, "Kernel Name", e.KernelName)

	// Hardware identifiers
	printPtrField(w, "Serial", e.Serial)
	printPtrField(w, "WWN", e.WWN)
	printPtrField(w, "LUID", e.LUID)
	printPtrField(w, "Model", e.Model)
	printPtrField(w, "Vendor", e.Vendor)

	// Block device info
	printPtrField(w, "MAJ:MIN", e.MajMin)
	printPtrField(w, "Size", e.Size)
	printPtrField(w, "SCSI Address", e.SCSIAddr)
	printPtrField(w, "Transport", e.Transport)

	// NVMe specific
	printPtrField(w, "NGUID", e.NGUID)
	printPtrField(w, "EUI-64", e.EUI64)

	// Partition info
	printPtrField(w, "Part UUID", e.PartUUID)
	printPtrField(w, "Part Label", e.PartLabel)
	if e.PartNum != nil {
		printField(w, "Part Number", fmt.Sprintf("%d", *e.PartNum))
	}
	printPtrField(w, "Parent Disk", e.ParentDisk)

	// Filesystem info
	printPtrField(w, "FS UUID", e.FSUUID)
	printPtrField(w, "FS Label", e.FSLabel)
	printPtrField(w, "FS Type", e.FSType)

	// /dev/disk/by-* symlinks
	if len(e.ByID) > 0 {
		for i, id := range e.ByID {
			if i == 0 {
				printField(w, "by-id", id)
			} else {
				printField(w, "", id)
			}
		}
	}
	if len(e.ByPath) > 0 {
		for i, p := range e.ByPath {
			if i == 0 {
				printField(w, "by-path", p)
			} else {
				printField(w, "", p)
			}
		}
	}
	printPtrField(w, "by-uuid", e.ByUUID)
	printPtrField(w, "by-partuuid", e.ByPartUUID)
	printPtrField(w, "by-label", e.ByLabel)
	printPtrField(w, "by-partlabel", e.ByPartLabel)

	// ZFS info
	printPtrField(w, "ZFS Pool", e.ZFSPoolName)
	printPtrField(w, "ZFS Pool GUID", e.ZFSPoolGUID)
	printPtrField(w, "ZFS Dataset", e.ZFSDatasetName)
	printPtrField(w, "ZFS Dataset GUID", e.ZFSDatasetGUID)
	printPtrField(w, "ZFS Vdev GUID", e.ZFSVdevGUID)

	// LVM info
	printPtrField(w, "LVM PV Device", e.LVMPVDevice)
	printPtrField(w, "LVM PV UUID", e.LVMPVUUID)
	printPtrField(w, "LVM VG Name", e.LVMVGName)
	printPtrField(w, "LVM VG UUID", e.LVMVGUUID)
	printPtrField(w, "LVM LV Name", e.LVMLVName)
	printPtrField(w, "LVM LV UUID", e.LVMLVUUID)
	printPtrField(w, "LVM LV Path", e.LVMLVPath)

	// MD RAID info
	printPtrField(w, "MD Array UUID", e.MDArrUUID)
	printPtrField(w, "MD Device UUID", e.MDDevUUID)
	printPtrField(w, "MD Name", e.MDName)

	// Device-mapper info
	printPtrField(w, "DM Name", e.DMName)
	printPtrField(w, "DM UUID", e.DMUUID)
}

// printField prints a field if value is non-empty
func printField(w io.Writer, label, value string) {
	if value != "" {
		fmt.Fprintf(w, "%-20s %s\n", label, value)
	}
}

// printPtrField prints a pointer field if non-nil
func printPtrField(w io.Writer, label string, value *string) {
	if value != nil && *value != "" {
		fmt.Fprintf(w, "%-20s %s\n", label, *value)
	}
}

// PrintQuiet outputs only the device path
func PrintQuiet(w io.Writer, result *LookupResult) {
	if result.Device.DevicePath != "" {
		fmt.Fprintln(w, result.Device.DevicePath)
	} else {
		// For non-device entities, output the matched identifier
		fmt.Fprintln(w, result.Query)
	}
}
