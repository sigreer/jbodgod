package collector

// DriveData represents comprehensive drive information from all sources
type DriveData struct {
	// === Identifiers ===
	Device    string  `json:"device"`
	Name      string  `json:"name,omitempty"`
	Serial    *string `json:"serial,omitempty"`
	SerialVPD *string `json:"serial_vpd,omitempty"`
	WWN       *string `json:"wwn,omitempty"`
	LUID      *string `json:"luid,omitempty"`
	GUID      *string `json:"guid,omitempty"`
	SASAddress *string `json:"sas_address,omitempty"`
	ByIDPath  *string `json:"by_id_path,omitempty"`

	// === Hardware ===
	Model      *string `json:"model,omitempty"`
	Vendor     *string `json:"vendor,omitempty"`
	Firmware   *string `json:"firmware,omitempty"`
	SizeBytes  *int64  `json:"size_bytes,omitempty"`
	Protocol   *string `json:"protocol,omitempty"`   // SAS, SATA, NVMe
	DriveType  *string `json:"drive_type,omitempty"` // HDD, SSD
	FormFactor *string `json:"form_factor,omitempty"`
	SectorSize *int    `json:"sector_size,omitempty"`
	LinkSpeed  *string `json:"link_speed,omitempty"`

	// === Physical Location ===
	ControllerID *string `json:"controller_id,omitempty"`
	Enclosure    *int    `json:"enclosure,omitempty"`
	Slot         *int    `json:"slot,omitempty"`
	PhyNum       *int    `json:"phy_num,omitempty"`
	SCSIAddr     *string `json:"scsi_addr,omitempty"`
	DeviceID     *int    `json:"device_id,omitempty"` // HBA device ID

	// === Runtime State ===
	State       string  `json:"state"`
	Temp        *int    `json:"temp,omitempty"`
	SmartHealth *string `json:"smart_health,omitempty"`

	// === Storage Stack: ZFS ===
	Zpool     *string    `json:"zpool,omitempty"`
	Vdev      *string    `json:"vdev,omitempty"`
	VdevGUID  *string    `json:"vdev_guid,omitempty"`
	ZfsErrors *ZfsErrors `json:"zfs_errors,omitempty"`

	// === Storage Stack: LVM ===
	LvmPV   *string `json:"lvm_pv,omitempty"`
	LvmVG   *string `json:"lvm_vg,omitempty"`
	LvmPVUUID *string `json:"lvm_pv_uuid,omitempty"`

	// === Filesystem ===
	FSType  *string `json:"fs_type,omitempty"`
	FSLabel *string `json:"fs_label,omitempty"`
	FSUUID  *string `json:"fs_uuid,omitempty"`
	PartUUID *string `json:"part_uuid,omitempty"`
	PartLabel *string `json:"part_label,omitempty"`

	// === SMART Metrics ===
	PowerOnHours *int `json:"power_on_hours,omitempty"`
	Reallocated  *int `json:"reallocated_sectors,omitempty"`
	PendingSectors *int `json:"pending_sectors,omitempty"`
	MediaErrors  *int `json:"media_errors,omitempty"`
}

// ZfsErrors holds ZFS vdev error counts
type ZfsErrors struct {
	Read  int `json:"read"`
	Write int `json:"write"`
	Cksum int `json:"cksum"`
}

// ControllerData represents HBA controller information
type ControllerData struct {
	ID            string  `json:"id"`
	Model         *string `json:"model,omitempty"`
	Serial        *string `json:"serial,omitempty"`
	SASAddress    *string `json:"sas_address,omitempty"`
	FirmwareVer   *string `json:"firmware_version,omitempty"`
	BIOSVer       *string `json:"bios_version,omitempty"`
	DriverVer     *string `json:"driver_version,omitempty"`
	PCIAddress    *string `json:"pci_address,omitempty"`
	Temperature   *int    `json:"temperature,omitempty"`
	PhysicalDrives int    `json:"physical_drives"`
}

// EnclosureData represents enclosure information
type EnclosureData struct {
	ID           int     `json:"id"`
	ControllerID string  `json:"controller_id"`
	NumSlots     int     `json:"num_slots"`
	Model        *string `json:"model,omitempty"`
	Vendor       *string `json:"vendor,omitempty"`
}

// SystemData holds bulk-collected system information
type SystemData struct {
	LsblkDevices  map[string]*LsblkDevice  // keyed by device name (sda, sdb)
	BlkidDevices  map[string]*BlkidDevice  // keyed by device path (/dev/sda1)
	LsscsiDevices map[string]*LsscsiDevice // keyed by device path
	ZpoolVdevs    map[string]*ZpoolVdev    // keyed by vdev GUID
	LvmPVs        map[string]*LvmPV        // keyed by device path
	ByIDLinks     map[string]string        // device path -> by-id path
	Controllers   map[string]*ControllerData
	HBADevices    map[string]*HBADevice    // keyed by serial
}

// LsblkDevice represents parsed lsblk output
type LsblkDevice struct {
	Name      string  `json:"name"`
	Path      string  `json:"path"`
	Size      *int64  `json:"size,omitempty"`
	Serial    *string `json:"serial,omitempty"`
	WWN       *string `json:"wwn,omitempty"`
	Model     *string `json:"model,omitempty"`
	Vendor    *string `json:"vendor,omitempty"`
	Rev       *string `json:"rev,omitempty"`
	HCTL      *string `json:"hctl,omitempty"`
	Tran      *string `json:"tran,omitempty"`
	Type      string  `json:"type"`
	MajMin    *string `json:"maj_min,omitempty"`
	FSType    *string `json:"fstype,omitempty"`
	UUID      *string `json:"uuid,omitempty"`
	Label     *string `json:"label,omitempty"`
	PartUUID  *string `json:"partuuid,omitempty"`
	PartLabel *string `json:"partlabel,omitempty"`
}

// BlkidDevice represents parsed blkid output
type BlkidDevice struct {
	Device    string  `json:"device"`
	UUID      *string `json:"uuid,omitempty"`
	UUIDSub   *string `json:"uuid_sub,omitempty"`
	Type      *string `json:"type,omitempty"`
	Label     *string `json:"label,omitempty"`
	PartUUID  *string `json:"partuuid,omitempty"`
	PartLabel *string `json:"partlabel,omitempty"`
}

// LsscsiDevice represents parsed lsscsi output
type LsscsiDevice struct {
	HCTL     string  `json:"hctl"`
	Type     string  `json:"type"`
	Vendor   *string `json:"vendor,omitempty"`
	Model    *string `json:"model,omitempty"`
	Rev      *string `json:"rev,omitempty"`
	Device   string  `json:"device"`
	SGDevice *string `json:"sg_device,omitempty"`
}

// ZpoolVdev represents a ZFS vdev
type ZpoolVdev struct {
	PoolName   string `json:"pool_name"`
	PoolState  string `json:"pool_state"`
	VdevGUID   string `json:"vdev_guid"`
	VdevType   string `json:"vdev_type"` // mirror, raidz, etc. or empty for leaf
	State      string `json:"state"`
	ReadErrors  int   `json:"read_errors"`
	WriteErrors int   `json:"write_errors"`
	CksumErrors int   `json:"cksum_errors"`
	DevicePath *string `json:"device_path,omitempty"` // for leaf vdevs
}

// LvmPV represents an LVM physical volume
type LvmPV struct {
	PVName string  `json:"pv_name"`
	PVUUID string  `json:"pv_uuid"`
	VGName *string `json:"vg_name,omitempty"`
	Size   *int64  `json:"size,omitempty"`
	Free   *int64  `json:"free,omitempty"`
}

// HBADevice represents a device from HBA tools (storcli/sas3ircu)
type HBADevice struct {
	ControllerID string  `json:"controller_id"`
	EnclosureID  int     `json:"enclosure_id"`
	Slot         int     `json:"slot"`
	DeviceID     *int    `json:"device_id,omitempty"`
	Serial       string  `json:"serial"`
	SerialVPD    *string `json:"serial_vpd,omitempty"`
	WWN          *string `json:"wwn,omitempty"`
	SASAddress   *string `json:"sas_address,omitempty"`
	Model        *string `json:"model,omitempty"`
	Vendor       *string `json:"vendor,omitempty"`
	Firmware     *string `json:"firmware,omitempty"`
	SizeBytes    *int64  `json:"size_bytes,omitempty"`
	SectorSize   *int    `json:"sector_size,omitempty"`
	Protocol     *string `json:"protocol,omitempty"`   // SAS, SATA
	MediaType    *string `json:"media_type,omitempty"` // HDD, SSD
	LinkSpeed    *string `json:"link_speed,omitempty"`
	State        *string `json:"state,omitempty"`
	MediaErrors  *int    `json:"media_errors,omitempty"`
	OtherErrors  *int    `json:"other_errors,omitempty"`
	PredFailure  *int    `json:"predictive_failure,omitempty"`
	SmartAlert   *bool   `json:"smart_alert,omitempty"`
	PhyNum       *int    `json:"phy_num,omitempty"`
}
