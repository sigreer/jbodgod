package ses

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/sigreer/jbodgod/internal/db"
	"github.com/sigreer/jbodgod/internal/hba"
	"github.com/sigreer/jbodgod/internal/identify"
)

// DefaultLocateTimeout is the default duration for locate LED
const DefaultLocateTimeout = 30 * time.Second

// encSlotPattern matches "enclosure:slot" format like "0:5" or "1:12"
var encSlotPattern = regexp.MustCompile(`^(\d+):(\d+)$`)

// ParseEnclosureSlot parses an "enclosure:slot" string
// Returns enclosure, slot, and true if parsing succeeded
func ParseEnclosureSlot(query string) (enclosure, slot int, ok bool) {
	matches := encSlotPattern.FindStringSubmatch(query)
	if len(matches) != 3 {
		return 0, 0, false
	}
	enclosure, _ = strconv.Atoi(matches[1])
	slot, _ = strconv.Atoi(matches[2])
	return enclosure, slot, true
}

// GetLocateInfo returns detailed information about a device for the locate command
// without actually turning on the LED (useful for --info-only or validation)
func GetLocateInfo(query string) (*LocateInfo, error) {
	// Build device index and look up
	idx, err := identify.BuildIndex()
	if err != nil {
		return nil, fmt.Errorf("failed to build device index: %w", err)
	}

	entity, matchedAs, err := idx.Lookup(query)
	if err != nil {
		return nil, fmt.Errorf("device not found: %s", query)
	}

	info := &LocateInfo{
		Query:     query,
		MatchedAs: string(matchedAs),
	}

	if entity.DevicePath != "" {
		info.DevicePath = entity.DevicePath
	}
	if entity.Serial != nil {
		info.Serial = *entity.Serial
	}
	if entity.Model != nil {
		info.Model = *entity.Model
	}

	// Get HBA info (enclosure and slot)
	if info.Serial == "" {
		return info, fmt.Errorf("device %s has no serial number (needed for HBA lookup)", query)
	}

	hbaDev := hba.GetDeviceBySerial(info.Serial)
	if hbaDev == nil {
		return info, fmt.Errorf("device %s not found in HBA (serial: %s) - not in a JBOD enclosure?", query, info.Serial)
	}

	info.EnclosureID = hbaDev.EnclosureID
	info.Slot = hbaDev.Slot

	// Get enclosure info to find SAS address for SES mapping
	_, enclosures, _, err := hba.FetchSas3ircuData(0, false)
	if err != nil {
		return info, fmt.Errorf("failed to fetch HBA enclosure data: %w", err)
	}

	var enclosure *hba.EnclosureInfo
	for i := range enclosures {
		if enclosures[i].ID == hbaDev.EnclosureID {
			enclosure = &enclosures[i]
			break
		}
	}

	if enclosure == nil {
		return info, fmt.Errorf("enclosure %d not found in HBA data", hbaDev.EnclosureID)
	}

	// Map enclosure to SES sg device
	sesEnc, err := MapEnclosureToSGDevice(enclosure.ID, enclosure.LogicalID, enclosure.SASAddress)
	if err != nil {
		return info, fmt.Errorf("could not find SES device for enclosure %d: %w", enclosure.ID, err)
	}

	info.SGDevice = sesEnc.SGDevice

	return info, nil
}

// GetLocateInfoBySlot returns locate info for a specific enclosure:slot
// This works even when no drive is present (for locating empty bays)
func GetLocateInfoBySlot(enclosure, slot int) (*LocateInfo, error) {
	info := &LocateInfo{
		Query:       fmt.Sprintf("%d:%d", enclosure, slot),
		MatchedAs:   "enclosure_slot",
		EnclosureID: enclosure,
		Slot:        slot,
	}

	// Check if there's a device at this slot
	hbaDev := hba.GetDeviceBySlot(enclosure, slot)
	if hbaDev != nil {
		info.Serial = hbaDev.Serial
		info.Model = hbaDev.Model
	}

	// Get enclosure info for SES mapping
	_, enclosures, _, err := hba.FetchSas3ircuData(0, false)
	if err != nil {
		return info, fmt.Errorf("failed to fetch HBA enclosure data: %w", err)
	}

	var enc *hba.EnclosureInfo
	for i := range enclosures {
		if enclosures[i].ID == enclosure {
			enc = &enclosures[i]
			break
		}
	}

	if enc == nil {
		return info, fmt.Errorf("enclosure %d not found", enclosure)
	}

	// Map to SES device
	sesEnc, err := MapEnclosureToSGDevice(enc.ID, enc.LogicalID, enc.SASAddress)
	if err != nil {
		return info, fmt.Errorf("could not find SES device for enclosure %d: %w", enclosure, err)
	}

	info.SGDevice = sesEnc.SGDevice
	return info, nil
}

// GetLocateInfoFromDB looks up a drive's last-known location from database
func GetLocateInfoFromDB(query string, database *db.DB) (*LocateInfo, error) {
	if database == nil {
		return nil, fmt.Errorf("database not available")
	}

	// Try looking up by serial
	drive, err := database.GetDriveBySerial(query)
	if err != nil {
		return nil, err
	}
	if drive == nil {
		return nil, fmt.Errorf("drive not found in inventory: %s", query)
	}

	if drive.EnclosureID == nil || drive.Slot == nil {
		return nil, fmt.Errorf("drive %s has no location info in inventory", query)
	}

	info := &LocateInfo{
		Query:       query,
		MatchedAs:   "database_serial",
		DevicePath:  drive.DevicePath + " (last known)",
		Serial:      drive.Serial,
		Model:       drive.Model,
		EnclosureID: *drive.EnclosureID,
		Slot:        *drive.Slot,
	}

	// Get enclosure info for SES mapping
	_, enclosures, _, err := hba.FetchSas3ircuData(0, false)
	if err != nil {
		return info, fmt.Errorf("failed to fetch HBA enclosure data: %w", err)
	}

	var enc *hba.EnclosureInfo
	for i := range enclosures {
		if enclosures[i].ID == *drive.EnclosureID {
			enc = &enclosures[i]
			break
		}
	}

	if enc == nil {
		return info, fmt.Errorf("enclosure %d not found", *drive.EnclosureID)
	}

	// Map to SES device
	sesEnc, err := MapEnclosureToSGDevice(enc.ID, enc.LogicalID, enc.SASAddress)
	if err != nil {
		return info, fmt.Errorf("could not find SES device for enclosure %d: %w", enc.ID, err)
	}

	info.SGDevice = sesEnc.SGDevice
	return info, nil
}

// GetLocateInfoWithFallback tries live lookup first, then database fallback
// It also supports enclosure:slot format directly
func GetLocateInfoWithFallback(query string, database *db.DB) (*LocateInfo, error) {
	// First, check if query is enclosure:slot format
	if enc, slot, ok := ParseEnclosureSlot(query); ok {
		return GetLocateInfoBySlot(enc, slot)
	}

	// Try normal live lookup
	info, err := GetLocateInfo(query)
	if err == nil {
		return info, nil
	}

	// If live lookup failed and we have a database, try DB lookup
	if database != nil {
		dbInfo, dbErr := GetLocateInfoFromDB(query, database)
		if dbErr == nil {
			return dbInfo, nil
		}
		// Return original error with note about DB lookup
		return nil, fmt.Errorf("%w (also checked inventory: %v)", err, dbErr)
	}

	return nil, err
}

// LocateByIdentifier locates a drive by any unique identifier
// This is the main entry point for the locate command
func LocateByIdentifier(query string, timeout time.Duration) (*LocateInfo, error) {
	// Get device info
	info, err := GetLocateInfo(query)
	if err != nil {
		return info, err
	}

	// Validate we have everything needed
	if info.SGDevice == "" {
		return info, fmt.Errorf("could not determine SES device for enclosure")
	}

	// Turn on LED with timeout
	ctx := context.Background()
	err = LocateWithTimeout(ctx, info.SGDevice, info.Slot, timeout)

	return info, err
}

// LocateOn turns on the locate LED for a device without timeout
func LocateOn(query string) (*LocateInfo, error) {
	info, err := GetLocateInfo(query)
	if err != nil {
		return info, err
	}

	if info.SGDevice == "" {
		return info, fmt.Errorf("could not determine SES device for enclosure")
	}

	if err := SetSlotIdentLED(info.SGDevice, info.Slot, true); err != nil {
		return info, fmt.Errorf("failed to turn on LED: %w", err)
	}

	return info, nil
}

// LocateOff turns off the locate LED for a device
func LocateOff(query string) (*LocateInfo, error) {
	info, err := GetLocateInfo(query)
	if err != nil {
		return info, err
	}

	if info.SGDevice == "" {
		return info, fmt.Errorf("could not determine SES device for enclosure")
	}

	if err := SetSlotIdentLED(info.SGDevice, info.Slot, false); err != nil {
		return info, fmt.Errorf("failed to turn off LED: %w", err)
	}

	return info, nil
}
