package ses

import (
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/sigreer/jbodgod/internal/cache"
)

// DiscoverSESDevices finds all SES-capable enclosure devices
// Parses output from: lsscsi -g
// Returns a slice of discovered SES enclosures
func DiscoverSESDevices() ([]*EnclosureSES, error) {
	c := cache.Global()
	cacheKey := "ses:devices"

	// Check cache first
	if cached := c.Get(cacheKey); cached != nil {
		return cached.([]*EnclosureSES), nil
	}

	// Check if lsscsi is available
	if _, err := exec.LookPath("lsscsi"); err != nil {
		return nil, ErrLsscsiNotInstalled
	}

	// Execute lsscsi -g to get generic devices
	out, err := exec.Command("lsscsi", "-g").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("lsscsi failed: %w", err)
	}

	var enclosures []*EnclosureSES

	// Parse output for enclosure devices
	// Format: [H:C:T:L]  enclosu VENDOR   PRODUCT    REV   -         /dev/sg<N>
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if !strings.Contains(strings.ToLower(line), "enclosu") {
			continue
		}

		enc, err := parseLsscsiEnclosureLine(line)
		if err != nil {
			continue
		}

		// Get SAS address via sg_ses for matching with HBA data
		sasAddr := getSESDeviceSASAddress(enc.SGDevice)
		if sasAddr != "" {
			enc.SASAddress = sasAddr
		}

		enclosures = append(enclosures, enc)
	}

	// Cache with slow TTL (hardware topology rarely changes)
	if len(enclosures) > 0 {
		c.SetSlow(cacheKey, enclosures)
	}

	return enclosures, nil
}

// parseLsscsiEnclosureLine parses a single lsscsi output line for an enclosure
func parseLsscsiEnclosureLine(line string) (*EnclosureSES, error) {
	// Example: [6:0:24:0]   enclosu SMC      SC826-P          0001  -         /dev/sg23
	//          [H:C:T:L]    type    vendor   product          rev   block     generic

	// Extract sg device using regex
	sgRe := regexp.MustCompile(`(/dev/sg\d+)\s*$`)
	sgMatches := sgRe.FindStringSubmatch(line)
	if len(sgMatches) < 2 {
		return nil, errors.New("no sg device found")
	}

	enc := &EnclosureSES{
		SGDevice: sgMatches[1],
	}

	// Extract vendor and product (after "enclosu" field)
	fields := strings.Fields(line)
	for i, f := range fields {
		if strings.ToLower(f) == "enclosu" && i+2 < len(fields) {
			enc.Vendor = fields[i+1]
			enc.Product = fields[i+2]
			break
		}
	}

	return enc, nil
}

// getSESDeviceSASAddress retrieves the SAS address for an SES device
// Uses: sg_ses --page=ed /dev/sg<N>
func getSESDeviceSASAddress(sgDevice string) string {
	// Try to get SAS address from enclosure descriptor page
	out, err := exec.Command("sudo", "sg_ses", "--page=ed", sgDevice).CombinedOutput()
	if err != nil {
		// Fallback: try to get it from the additional element status page
		out, err = exec.Command("sudo", "sg_ses", "--page=aes", sgDevice).CombinedOutput()
		if err != nil {
			return ""
		}
	}

	// Parse for SAS address
	// Look for patterns like: "SAS address: 0x500304800f1xxxxx" or "attached SAS address: 5..."
	patterns := []string{
		`(?i)sas\s+address[:\s]+0x([0-9a-fA-F]+)`,
		`(?i)sas\s+address[:\s]+([0-9a-fA-F]{16})`,
		`(?i)attached\s+sas\s+address[:\s]+([0-9a-fA-F]+)`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		if matches := re.FindStringSubmatch(string(out)); len(matches) > 1 {
			return strings.ToLower(matches[1])
		}
	}

	return ""
}

// MapEnclosureToSGDevice maps an HBA enclosure to its SES sg device
// Cross-references using SAS address
func MapEnclosureToSGDevice(enclosureID int, enclosureLogicalID string, enclosureSASAddr string) (*EnclosureSES, error) {
	devices, err := DiscoverSESDevices()
	if err != nil {
		return nil, err
	}

	if len(devices) == 0 {
		// No SES devices found - sg module might not be loaded
		return nil, fmt.Errorf("%w (try: sudo modprobe sg)", ErrSGDeviceNotFound)
	}

	// Normalize the input SAS address for comparison
	normalizedInput := normalizeSASAddress(enclosureSASAddr)

	// Try matching by SAS address (most reliable)
	if normalizedInput != "" {
		for _, enc := range devices {
			if enc.SASAddress == "" {
				continue
			}
			normalizedEnc := normalizeSASAddress(enc.SASAddress)

			// Check for suffix match (addresses may have different prefixes)
			if strings.HasSuffix(normalizedInput, normalizedEnc) ||
				strings.HasSuffix(normalizedEnc, normalizedInput) ||
				normalizedInput == normalizedEnc {
				enc.EnclosureID = enclosureID
				enc.LogicalID = enclosureLogicalID
				return enc, nil
			}
		}
	}

	// Fallback: if only one enclosure exists, use it
	if len(devices) == 1 {
		devices[0].EnclosureID = enclosureID
		devices[0].LogicalID = enclosureLogicalID
		return devices[0], nil
	}

	// Multiple enclosures but no SAS address match
	return nil, ErrSGDeviceNotFound
}

// normalizeSASAddress normalizes a SAS address for comparison
func normalizeSASAddress(addr string) string {
	// Remove 0x prefix if present
	addr = strings.TrimPrefix(strings.ToLower(addr), "0x")
	// Remove any dashes or colons
	addr = strings.ReplaceAll(addr, "-", "")
	addr = strings.ReplaceAll(addr, ":", "")
	return addr
}

// GetEnclosureByID finds an enclosure by its ID from a list
func GetEnclosureByID(enclosures []*EnclosureSES, id int) *EnclosureSES {
	for _, enc := range enclosures {
		if enc.EnclosureID == id {
			return enc
		}
	}
	return nil
}
