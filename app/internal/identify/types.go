package identify

import "errors"

// ErrNotFound is returned when a query doesn't match any device
var ErrNotFound = errors.New("device not found")

// DeviceType categorizes the entity type
type DeviceType string

const (
	TypeDisk       DeviceType = "disk"
	TypePartition  DeviceType = "partition"
	TypeNVMeNS     DeviceType = "nvme_ns"
	TypeZFSPool    DeviceType = "zfs_pool"
	TypeZFSDataset DeviceType = "zfs_dataset"
	TypeLVMPV      DeviceType = "lvm_pv"
	TypeLVMVG      DeviceType = "lvm_vg"
	TypeLVMLV      DeviceType = "lvm_lv"
	TypeMDArray    DeviceType = "md_array"
	TypeDMDevice   DeviceType = "dm_device"
	TypeLoop       DeviceType = "loop"
	TypeROM        DeviceType = "rom"
)

// IdentifierType describes what type of identifier was matched
type IdentifierType string

const (
	IDDevicePath  IdentifierType = "device_path"
	IDKernelName  IdentifierType = "kernel_name"
	IDSerial      IdentifierType = "serial"
	IDWWN         IdentifierType = "wwn"
	IDLUID        IdentifierType = "luid"
	IDMajMin      IdentifierType = "maj_min"
	IDSCSIAddr    IdentifierType = "scsi_addr"
	IDNGUID       IdentifierType = "nguid"
	IDEUI64       IdentifierType = "eui64"
	IDPartUUID    IdentifierType = "partuuid"
	IDPartLabel   IdentifierType = "partlabel"
	IDFSUUID      IdentifierType = "fs_uuid"
	IDFSLabel     IdentifierType = "fs_label"
	IDByID        IdentifierType = "by_id"
	IDByPath      IdentifierType = "by_path"
	IDZFSPoolGUID IdentifierType = "zfs_pool_guid"
	IDZFSPoolName IdentifierType = "zfs_pool_name"
	IDZFSDataGUID IdentifierType = "zfs_dataset_guid"
	IDZFSDataName IdentifierType = "zfs_dataset_name"
	IDZFSVdevGUID IdentifierType = "zfs_vdev_guid"
	IDLVMPVDevice IdentifierType = "lvm_pv_device"
	IDLVMPVUUID   IdentifierType = "lvm_pv_uuid"
	IDLVMVGUUID   IdentifierType = "lvm_vg_uuid"
	IDLVMVGName   IdentifierType = "lvm_vg_name"
	IDLVMLVUUID   IdentifierType = "lvm_lv_uuid"
	IDLVMLVName   IdentifierType = "lvm_lv_name"
	IDLVMLVPath   IdentifierType = "lvm_lv_path"
	IDMDArrUUID   IdentifierType = "md_array_uuid"
	IDMDDevUUID   IdentifierType = "md_device_uuid"
	IDMDName      IdentifierType = "md_name"
	IDDMName      IdentifierType = "dm_name"
	IDDMUUID      IdentifierType = "dm_uuid"
	IDSymlink     IdentifierType = "symlink"
	IDUnknown     IdentifierType = "unknown"
)

// DeviceEntity represents a single identifiable storage entity with all its identifiers
type DeviceEntity struct {
	// Core identification
	Type       DeviceType `json:"type"`
	DevicePath string     `json:"device_path,omitempty"`
	KernelName string     `json:"kernel_name,omitempty"`

	// Physical disk identifiers
	Serial *string `json:"serial,omitempty"`
	WWN    *string `json:"wwn,omitempty"`
	LUID   *string `json:"luid,omitempty"`
	Model  *string `json:"model,omitempty"`
	Vendor *string `json:"vendor,omitempty"`

	// Block device identifiers
	MajMin    *string `json:"maj_min,omitempty"`
	Size      *string `json:"size,omitempty"`
	SCSIAddr  *string `json:"scsi_addr,omitempty"`
	Transport *string `json:"transport,omitempty"`

	// NVMe-specific identifiers
	NGUID *string `json:"nguid,omitempty"`
	EUI64 *string `json:"eui64,omitempty"`

	// Partition identifiers
	PartUUID   *string `json:"partuuid,omitempty"`
	PartLabel  *string `json:"partlabel,omitempty"`
	PartNum    *int    `json:"part_num,omitempty"`
	ParentDisk *string `json:"parent_disk,omitempty"`

	// Filesystem identifiers
	FSUUID  *string `json:"fs_uuid,omitempty"`
	FSLabel *string `json:"fs_label,omitempty"`
	FSType  *string `json:"fs_type,omitempty"`

	// /dev/disk/by-* paths (all symlink names pointing to this device)
	ByID        []string `json:"by_id,omitempty"`
	ByPath      []string `json:"by_path,omitempty"`
	ByUUID      *string  `json:"by_uuid,omitempty"`
	ByPartUUID  *string  `json:"by_partuuid,omitempty"`
	ByLabel     *string  `json:"by_label,omitempty"`
	ByPartLabel *string  `json:"by_partlabel,omitempty"`

	// ZFS identifiers
	ZFSPoolGUID    *string `json:"zfs_pool_guid,omitempty"`
	ZFSPoolName    *string `json:"zfs_pool_name,omitempty"`
	ZFSDatasetGUID *string `json:"zfs_dataset_guid,omitempty"`
	ZFSDatasetName *string `json:"zfs_dataset_name,omitempty"`
	ZFSVdevGUID    *string `json:"zfs_vdev_guid,omitempty"`

	// LVM identifiers
	LVMPVDevice *string `json:"lvm_pv_device,omitempty"`
	LVMPVUUID   *string `json:"lvm_pv_uuid,omitempty"`
	LVMVGUUID   *string `json:"lvm_vg_uuid,omitempty"`
	LVMVGName   *string `json:"lvm_vg_name,omitempty"`
	LVMLVUUID   *string `json:"lvm_lv_uuid,omitempty"`
	LVMLVName   *string `json:"lvm_lv_name,omitempty"`
	LVMLVPath   *string `json:"lvm_lv_path,omitempty"`

	// MD RAID identifiers
	MDArrUUID *string `json:"md_array_uuid,omitempty"`
	MDDevUUID *string `json:"md_device_uuid,omitempty"`
	MDName    *string `json:"md_name,omitempty"`

	// Device-mapper identifiers
	DMName *string `json:"dm_name,omitempty"`
	DMUUID *string `json:"dm_uuid,omitempty"`
}

// LookupResult contains the matched entity and metadata about the match
type LookupResult struct {
	Query     string         `json:"query"`
	MatchedAs IdentifierType `json:"matched_as"`
	Device    *DeviceEntity  `json:"device"`
}

// ptr is a helper to create a pointer to a string
func ptr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ptrInt is a helper to create a pointer to an int
func ptrInt(i int) *int {
	return &i
}
