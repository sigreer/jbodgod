package sources

// SourceEntity contains device data collected from a source
// This mirrors DeviceEntity but is local to avoid import cycles
type SourceEntity struct {
	// Core identification
	Type       string
	DevicePath string
	KernelName string

	// Physical disk identifiers
	Serial *string
	WWN    *string
	LUID   *string
	Model  *string
	Vendor *string

	// Block device identifiers
	MajMin    *string
	Size      *string
	SCSIAddr  *string
	Transport *string

	// NVMe-specific identifiers
	NGUID *string
	EUI64 *string

	// Partition identifiers
	PartUUID   *string
	PartLabel  *string
	PartNum    *int
	ParentDisk *string

	// Filesystem identifiers
	FSUUID  *string
	FSLabel *string
	FSType  *string

	// /dev/disk/by-* paths
	ByID        []string
	ByPath      []string
	ByUUID      *string
	ByPartUUID  *string
	ByLabel     *string
	ByPartLabel *string

	// ZFS identifiers
	ZFSPoolGUID    *string
	ZFSPoolName    *string
	ZFSDatasetGUID *string
	ZFSDatasetName *string
	ZFSVdevGUID    *string

	// LVM identifiers
	LVMPVDevice *string
	LVMPVUUID   *string
	LVMVGUUID   *string
	LVMVGName   *string
	LVMLVUUID   *string
	LVMLVName   *string
	LVMLVPath   *string

	// MD RAID identifiers
	MDArrUUID *string
	MDDevUUID *string
	MDName    *string

	// Device-mapper identifiers
	DMName *string
	DMUUID *string
}
