package hba

// ControllerInfo contains HBA/RAID controller information
type ControllerInfo struct {
	// Identification
	ID          string `json:"id"`           // c0, c1, etc.
	Type        string `json:"type"`         // SAS3008, etc.
	Model       string `json:"model"`        // Dell HBA330 Adp
	Serial      string `json:"serial"`       // Controller serial
	SASAddress  string `json:"sas_address"`  // SAS WWN

	// Firmware/BIOS
	FirmwareVersion string `json:"firmware_version"`
	BIOSVersion     string `json:"bios_version"`
	DriverName      string `json:"driver_name"`
	DriverVersion   string `json:"driver_version"`
	NVDataVersion   string `json:"nvdata_version,omitempty"`

	// PCI Info
	PCIAddress   string `json:"pci_address"`
	PCIBus       int    `json:"pci_bus"`
	PCIDevice    int    `json:"pci_device"`
	PCIFunction  int    `json:"pci_function"`
	PCIVendorID  string `json:"pci_vendor_id,omitempty"`
	PCIDeviceID  string `json:"pci_device_id,omitempty"`

	// Capabilities
	MaxPhysicalDevices int    `json:"max_physical_devices"`
	ConcurrentCommands int    `json:"concurrent_commands"`
	SupportedDrives    string `json:"supported_drives"` // SAS, SATA
	RAIDSupport        bool   `json:"raid_support"`

	// Status
	Temperature     *int   `json:"temperature,omitempty"` // ROC temperature
	ChannelDesc     string `json:"channel_desc,omitempty"`
	PhyCount        int    `json:"phy_count,omitempty"`
}

// EnclosureInfo contains JBOD enclosure information
type EnclosureInfo struct {
	ID           int    `json:"id"`            // Enclosure number
	LogicalID    string `json:"logical_id"`    // Enclosure logical ID
	NumSlots     int    `json:"num_slots"`     // Total slots
	StartSlot    int    `json:"start_slot"`    // First slot number
	Manufacturer string `json:"manufacturer"`  // SMC, etc.
	Model        string `json:"model"`         // SC826-P
	Firmware     string `json:"firmware"`      // Enclosure firmware
	Serial       string `json:"serial"`        // Enclosure serial
	SASAddress   string `json:"sas_address"`   // Enclosure SAS address
}

// PhysicalDevice contains per-drive information from HBA
type PhysicalDevice struct {
	// Location
	EnclosureID int    `json:"enclosure_id"`
	Slot        int    `json:"slot"`
	SASAddress  string `json:"sas_address"`
	GUID        string `json:"guid"`

	// Identification
	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`
	Serial       string `json:"serial"`
	SerialVPD    string `json:"serial_vpd,omitempty"` // Unit Serial No (VPD)
	Firmware     string `json:"firmware"`

	// Characteristics
	Protocol  string `json:"protocol"`   // SAS, SATA
	DriveType string `json:"drive_type"` // SAS_HDD, SATA_SSD, etc.
	SizeMB    int64  `json:"size_mb"`
	Sectors   int64  `json:"sectors"`

	// State
	State string `json:"state"` // Ready, Standby, etc.
}

// HBAData contains all data retrieved from HBA tools
type HBAData struct {
	Controllers []ControllerInfo  `json:"controllers"`
	Enclosures  []EnclosureInfo   `json:"enclosures"`
	Devices     []PhysicalDevice  `json:"devices"`
}
