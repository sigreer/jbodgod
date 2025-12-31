package ses

import (
	"context"
	"fmt"
	"time"

	"github.com/sigreer/jbodgod/internal/hba"
	"github.com/sigreer/jbodgod/internal/identify"
)

// DefaultLocateTimeout is the default duration for locate LED
const DefaultLocateTimeout = 30 * time.Second

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
