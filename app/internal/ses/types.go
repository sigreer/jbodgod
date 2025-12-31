package ses

import "errors"

// Common errors
var (
	ErrEnclosureNotFound = errors.New("enclosure not found")
	ErrSGDeviceNotFound  = errors.New("sg device for enclosure not found")
	ErrSlotNotFound      = errors.New("slot not found in enclosure")
	ErrSgSesNotInstalled = errors.New("sg_ses not found in PATH")
	ErrLsscsiNotInstalled = errors.New("lsscsi not found in PATH")
	ErrPermissionDenied  = errors.New("permission denied (requires root)")
)

// EnclosureSES represents an SES-capable enclosure with its control device
type EnclosureSES struct {
	EnclosureID int    // Matches hba.EnclosureInfo.ID
	LogicalID   string // Enclosure logical ID for cross-reference
	SASAddress  string // SAS address for matching
	SGDevice    string // /dev/sg<N> control device
	NumSlots    int    // Total slots in enclosure
	Vendor      string // Enclosure vendor
	Product     string // Enclosure product name
}

// SlotLEDState represents the LED state of a slot
type SlotLEDState struct {
	Slot   int
	Ident  bool // Locate/Identify LED
	Fault  bool // Fault LED
	Active bool // Active/Activity LED
}

// LocateInfo contains information about a located device for display
type LocateInfo struct {
	Query       string `json:"query"`
	MatchedAs   string `json:"matched_as"`
	DevicePath  string `json:"device_path"`
	Serial      string `json:"serial"`
	Model       string `json:"model,omitempty"`
	EnclosureID int    `json:"enclosure_id"`
	Slot        int    `json:"slot"`
	SGDevice    string `json:"sg_device"`
}
