package identify

import (
	"path/filepath"
	"sync"

	"github.com/sigreer/jbodgod/internal/identify/sources"
)

// DataSource is the interface for device data sources
type DataSource interface {
	Collect() (map[string]*sources.SourceEntity, error)
}

// DeviceIndex holds all discovered devices with multiple lookup indexes
type DeviceIndex struct {
	// Primary storage: device path -> entity
	Entities map[string]*DeviceEntity

	// Reverse lookup indexes: identifier value -> device path
	ByKernelName  map[string]string
	BySerial      map[string]string
	ByWWN         map[string]string
	ByLUID        map[string]string
	ByMajMin      map[string]string
	BySCSIAddr    map[string]string
	ByNGUID       map[string]string
	ByEUI64       map[string]string
	ByPartUUID    map[string]string
	ByPartLabel   map[string]string
	ByFSUUID      map[string]string
	ByFSLabel     map[string]string
	ByIDPath      map[string]string // by-id symlink name -> device
	ByPathPath    map[string]string // by-path symlink name -> device

	// ZFS indexes
	ByZFSPoolGUID map[string]string
	ByZFSPoolName map[string]string
	ByZFSDataGUID map[string]string
	ByZFSDataName map[string]string
	ByZFSVdevGUID map[string]string

	// LVM indexes
	ByLVMPVUUID map[string]string
	ByLVMVGUUID map[string]string
	ByLVMVGName map[string]string
	ByLVMLVUUID map[string]string
	ByLVMLVName map[string]string
	ByLVMLVPath map[string]string

	// MD RAID indexes
	ByMDArrUUID map[string]string
	ByMDName    map[string]string

	// Device-mapper indexes
	ByDMName map[string]string
	ByDMUUID map[string]string

	// Symlink path -> device path
	SymlinkMap map[string]string
}

// NewDeviceIndex creates an empty device index
func NewDeviceIndex() *DeviceIndex {
	return &DeviceIndex{
		Entities:      make(map[string]*DeviceEntity),
		ByKernelName:  make(map[string]string),
		BySerial:      make(map[string]string),
		ByWWN:         make(map[string]string),
		ByLUID:        make(map[string]string),
		ByMajMin:      make(map[string]string),
		BySCSIAddr:    make(map[string]string),
		ByNGUID:       make(map[string]string),
		ByEUI64:       make(map[string]string),
		ByPartUUID:    make(map[string]string),
		ByPartLabel:   make(map[string]string),
		ByFSUUID:      make(map[string]string),
		ByFSLabel:     make(map[string]string),
		ByIDPath:      make(map[string]string),
		ByPathPath:    make(map[string]string),
		ByZFSPoolGUID: make(map[string]string),
		ByZFSPoolName: make(map[string]string),
		ByZFSDataGUID: make(map[string]string),
		ByZFSDataName: make(map[string]string),
		ByZFSVdevGUID: make(map[string]string),
		ByLVMPVUUID:   make(map[string]string),
		ByLVMVGUUID:   make(map[string]string),
		ByLVMVGName:   make(map[string]string),
		ByLVMLVUUID:   make(map[string]string),
		ByLVMLVName:   make(map[string]string),
		ByLVMLVPath:   make(map[string]string),
		ByMDArrUUID:   make(map[string]string),
		ByMDName:      make(map[string]string),
		ByDMName:      make(map[string]string),
		ByDMUUID:      make(map[string]string),
		SymlinkMap:    make(map[string]string),
	}
}

// BuildIndex collects data from all sources and builds the lookup index
func BuildIndex() (*DeviceIndex, error) {
	idx := NewDeviceIndex()

	// Define data sources
	dataSources := []DataSource{
		&sources.LsblkSource{},
		&sources.DiskBySource{},
		&sources.SmartSource{},
		&sources.ZFSSource{},
		&sources.LVMSource{},
		&sources.MDRaidSource{},
		&sources.DMSource{},
	}

	// Collect data from all sources in parallel
	results := make([]map[string]*sources.SourceEntity, len(dataSources))
	var wg sync.WaitGroup

	for i, src := range dataSources {
		wg.Add(1)
		go func(idx int, s DataSource) {
			defer wg.Done()
			data, _ := s.Collect()
			results[idx] = data
		}(i, src)
	}
	wg.Wait()

	// Merge results (later sources augment earlier ones)
	for _, result := range results {
		idx.mergeSourceEntities(result)
	}

	// Build symlink mappings
	diskBy := &sources.DiskBySource{}
	idx.SymlinkMap = diskBy.GetSymlinkMappings()

	// Build reverse indexes
	idx.buildIndexes()

	return idx, nil
}

// mergeSourceEntities merges source entity data into device entities
func (idx *DeviceIndex) mergeSourceEntities(data map[string]*sources.SourceEntity) {
	for key, src := range data {
		if src.DevicePath == "" {
			// Non-device entities (like ZFS pools, LVM VGs)
			entity := idx.convertSourceEntity(src)
			idx.Entities[key] = entity
			continue
		}

		existing, ok := idx.Entities[src.DevicePath]
		if !ok {
			idx.Entities[src.DevicePath] = idx.convertSourceEntity(src)
			continue
		}

		// Merge fields from source into existing
		idx.mergeIntoEntity(existing, src)
	}
}

// convertSourceEntity converts a SourceEntity to DeviceEntity
func (idx *DeviceIndex) convertSourceEntity(src *sources.SourceEntity) *DeviceEntity {
	return &DeviceEntity{
		Type:           idx.mapDeviceType(src.Type),
		DevicePath:     src.DevicePath,
		KernelName:     src.KernelName,
		Serial:         src.Serial,
		WWN:            src.WWN,
		LUID:           src.LUID,
		Model:          src.Model,
		Vendor:         src.Vendor,
		MajMin:         src.MajMin,
		Size:           src.Size,
		SCSIAddr:       src.SCSIAddr,
		Transport:      src.Transport,
		NGUID:          src.NGUID,
		EUI64:          src.EUI64,
		PartUUID:       src.PartUUID,
		PartLabel:      src.PartLabel,
		PartNum:        src.PartNum,
		ParentDisk:     src.ParentDisk,
		FSUUID:         src.FSUUID,
		FSLabel:        src.FSLabel,
		FSType:         src.FSType,
		ByID:           src.ByID,
		ByPath:         src.ByPath,
		ByUUID:         src.ByUUID,
		ByPartUUID:     src.ByPartUUID,
		ByLabel:        src.ByLabel,
		ByPartLabel:    src.ByPartLabel,
		ZFSPoolGUID:    src.ZFSPoolGUID,
		ZFSPoolName:    src.ZFSPoolName,
		ZFSDatasetGUID: src.ZFSDatasetGUID,
		ZFSDatasetName: src.ZFSDatasetName,
		ZFSVdevGUID:    src.ZFSVdevGUID,
		LVMPVDevice:    src.LVMPVDevice,
		LVMPVUUID:      src.LVMPVUUID,
		LVMVGUUID:      src.LVMVGUUID,
		LVMVGName:      src.LVMVGName,
		LVMLVUUID:      src.LVMLVUUID,
		LVMLVName:      src.LVMLVName,
		LVMLVPath:      src.LVMLVPath,
		MDArrUUID:      src.MDArrUUID,
		MDDevUUID:      src.MDDevUUID,
		MDName:         src.MDName,
		DMName:         src.DMName,
		DMUUID:         src.DMUUID,
	}
}

// mapDeviceType maps string type to DeviceType
func (idx *DeviceIndex) mapDeviceType(t string) DeviceType {
	switch t {
	case "disk":
		return TypeDisk
	case "part":
		return TypePartition
	case "lvm", "lvm_lv":
		return TypeLVMLV
	case "lvm_pv":
		return TypeLVMPV
	case "lvm_vg":
		return TypeLVMVG
	case "zfs_pool":
		return TypeZFSPool
	case "zfs_dataset":
		return TypeZFSDataset
	case "md_array", "raid0", "raid1", "raid5", "raid6", "raid10":
		return TypeMDArray
	case "dm", "dm_device", "crypt", "mpath":
		return TypeDMDevice
	case "loop":
		return TypeLoop
	case "rom":
		return TypeROM
	default:
		return TypeDisk
	}
}

// mergeIntoEntity merges fields from source into existing entity
func (idx *DeviceIndex) mergeIntoEntity(dst *DeviceEntity, src *sources.SourceEntity) {
	if src.Type != "" && dst.Type == "" {
		dst.Type = idx.mapDeviceType(src.Type)
	}
	if src.KernelName != "" && dst.KernelName == "" {
		dst.KernelName = src.KernelName
	}
	if src.Serial != nil && dst.Serial == nil {
		dst.Serial = src.Serial
	}
	if src.WWN != nil && dst.WWN == nil {
		dst.WWN = src.WWN
	}
	if src.LUID != nil && dst.LUID == nil {
		dst.LUID = src.LUID
	}
	if src.Model != nil && dst.Model == nil {
		dst.Model = src.Model
	}
	if src.Vendor != nil && dst.Vendor == nil {
		dst.Vendor = src.Vendor
	}
	if src.MajMin != nil && dst.MajMin == nil {
		dst.MajMin = src.MajMin
	}
	if src.Size != nil && dst.Size == nil {
		dst.Size = src.Size
	}
	if src.SCSIAddr != nil && dst.SCSIAddr == nil {
		dst.SCSIAddr = src.SCSIAddr
	}
	if src.Transport != nil && dst.Transport == nil {
		dst.Transport = src.Transport
	}
	if src.NGUID != nil && dst.NGUID == nil {
		dst.NGUID = src.NGUID
	}
	if src.EUI64 != nil && dst.EUI64 == nil {
		dst.EUI64 = src.EUI64
	}
	if src.PartUUID != nil && dst.PartUUID == nil {
		dst.PartUUID = src.PartUUID
	}
	if src.PartLabel != nil && dst.PartLabel == nil {
		dst.PartLabel = src.PartLabel
	}
	if src.PartNum != nil && dst.PartNum == nil {
		dst.PartNum = src.PartNum
	}
	if src.ParentDisk != nil && dst.ParentDisk == nil {
		dst.ParentDisk = src.ParentDisk
	}
	if src.FSUUID != nil && dst.FSUUID == nil {
		dst.FSUUID = src.FSUUID
	}
	if src.FSLabel != nil && dst.FSLabel == nil {
		dst.FSLabel = src.FSLabel
	}
	if src.FSType != nil && dst.FSType == nil {
		dst.FSType = src.FSType
	}
	if len(src.ByID) > 0 && len(dst.ByID) == 0 {
		dst.ByID = src.ByID
	}
	if len(src.ByPath) > 0 && len(dst.ByPath) == 0 {
		dst.ByPath = src.ByPath
	}
	if src.ByUUID != nil && dst.ByUUID == nil {
		dst.ByUUID = src.ByUUID
	}
	if src.ByPartUUID != nil && dst.ByPartUUID == nil {
		dst.ByPartUUID = src.ByPartUUID
	}
	if src.ByLabel != nil && dst.ByLabel == nil {
		dst.ByLabel = src.ByLabel
	}
	if src.ByPartLabel != nil && dst.ByPartLabel == nil {
		dst.ByPartLabel = src.ByPartLabel
	}
	if src.ZFSPoolGUID != nil && dst.ZFSPoolGUID == nil {
		dst.ZFSPoolGUID = src.ZFSPoolGUID
	}
	if src.ZFSPoolName != nil && dst.ZFSPoolName == nil {
		dst.ZFSPoolName = src.ZFSPoolName
	}
	if src.ZFSDatasetGUID != nil && dst.ZFSDatasetGUID == nil {
		dst.ZFSDatasetGUID = src.ZFSDatasetGUID
	}
	if src.ZFSDatasetName != nil && dst.ZFSDatasetName == nil {
		dst.ZFSDatasetName = src.ZFSDatasetName
	}
	if src.ZFSVdevGUID != nil && dst.ZFSVdevGUID == nil {
		dst.ZFSVdevGUID = src.ZFSVdevGUID
	}
	if src.LVMPVDevice != nil && dst.LVMPVDevice == nil {
		dst.LVMPVDevice = src.LVMPVDevice
	}
	if src.LVMPVUUID != nil && dst.LVMPVUUID == nil {
		dst.LVMPVUUID = src.LVMPVUUID
	}
	if src.LVMVGUUID != nil && dst.LVMVGUUID == nil {
		dst.LVMVGUUID = src.LVMVGUUID
	}
	if src.LVMVGName != nil && dst.LVMVGName == nil {
		dst.LVMVGName = src.LVMVGName
	}
	if src.LVMLVUUID != nil && dst.LVMLVUUID == nil {
		dst.LVMLVUUID = src.LVMLVUUID
	}
	if src.LVMLVName != nil && dst.LVMLVName == nil {
		dst.LVMLVName = src.LVMLVName
	}
	if src.LVMLVPath != nil && dst.LVMLVPath == nil {
		dst.LVMLVPath = src.LVMLVPath
	}
	if src.MDArrUUID != nil && dst.MDArrUUID == nil {
		dst.MDArrUUID = src.MDArrUUID
	}
	if src.MDDevUUID != nil && dst.MDDevUUID == nil {
		dst.MDDevUUID = src.MDDevUUID
	}
	if src.MDName != nil && dst.MDName == nil {
		dst.MDName = src.MDName
	}
	if src.DMName != nil && dst.DMName == nil {
		dst.DMName = src.DMName
	}
	if src.DMUUID != nil && dst.DMUUID == nil {
		dst.DMUUID = src.DMUUID
	}
}

// buildIndexes creates reverse lookup indexes from entities
func (idx *DeviceIndex) buildIndexes() {
	for key, entity := range idx.Entities {
		devicePath := entity.DevicePath
		if devicePath == "" {
			devicePath = key // Use entity key for non-device entities
		}

		if entity.KernelName != "" {
			idx.ByKernelName[entity.KernelName] = devicePath
		}
		if entity.Serial != nil {
			idx.BySerial[*entity.Serial] = devicePath
		}
		if entity.WWN != nil {
			idx.ByWWN[*entity.WWN] = devicePath
		}
		if entity.LUID != nil {
			idx.ByLUID[*entity.LUID] = devicePath
		}
		if entity.MajMin != nil {
			idx.ByMajMin[*entity.MajMin] = devicePath
		}
		if entity.SCSIAddr != nil {
			idx.BySCSIAddr[*entity.SCSIAddr] = devicePath
		}
		if entity.NGUID != nil {
			idx.ByNGUID[*entity.NGUID] = devicePath
		}
		if entity.EUI64 != nil {
			idx.ByEUI64[*entity.EUI64] = devicePath
		}
		if entity.PartUUID != nil {
			idx.ByPartUUID[*entity.PartUUID] = devicePath
		}
		if entity.PartLabel != nil {
			idx.ByPartLabel[*entity.PartLabel] = devicePath
		}
		if entity.FSUUID != nil {
			idx.ByFSUUID[*entity.FSUUID] = devicePath
		}
		if entity.FSLabel != nil {
			idx.ByFSLabel[*entity.FSLabel] = devicePath
		}

		// Index by-id names
		for _, byID := range entity.ByID {
			idx.ByIDPath[byID] = devicePath
		}

		// Index by-path names
		for _, byPath := range entity.ByPath {
			idx.ByPathPath[byPath] = devicePath
		}

		// ZFS indexes
		if entity.ZFSPoolGUID != nil {
			idx.ByZFSPoolGUID[*entity.ZFSPoolGUID] = devicePath
		}
		if entity.ZFSPoolName != nil {
			idx.ByZFSPoolName[*entity.ZFSPoolName] = devicePath
		}
		if entity.ZFSDatasetGUID != nil {
			idx.ByZFSDataGUID[*entity.ZFSDatasetGUID] = devicePath
		}
		if entity.ZFSDatasetName != nil {
			idx.ByZFSDataName[*entity.ZFSDatasetName] = devicePath
		}
		if entity.ZFSVdevGUID != nil {
			idx.ByZFSVdevGUID[*entity.ZFSVdevGUID] = devicePath
		}

		// LVM indexes
		if entity.LVMPVUUID != nil {
			idx.ByLVMPVUUID[*entity.LVMPVUUID] = devicePath
		}
		if entity.LVMVGUUID != nil {
			idx.ByLVMVGUUID[*entity.LVMVGUUID] = devicePath
		}
		if entity.LVMVGName != nil {
			idx.ByLVMVGName[*entity.LVMVGName] = devicePath
		}
		if entity.LVMLVUUID != nil {
			idx.ByLVMLVUUID[*entity.LVMLVUUID] = devicePath
		}
		if entity.LVMLVName != nil {
			idx.ByLVMLVName[*entity.LVMLVName] = devicePath
		}
		if entity.LVMLVPath != nil {
			idx.ByLVMLVPath[*entity.LVMLVPath] = devicePath
		}

		// MD RAID indexes
		if entity.MDArrUUID != nil {
			idx.ByMDArrUUID[*entity.MDArrUUID] = devicePath
		}
		if entity.MDName != nil {
			idx.ByMDName[*entity.MDName] = devicePath
		}

		// Device-mapper indexes
		if entity.DMName != nil {
			idx.ByDMName[*entity.DMName] = devicePath
		}
		if entity.DMUUID != nil {
			idx.ByDMUUID[*entity.DMUUID] = devicePath
		}
	}
}

// Lookup finds a device by any unique identifier
func (idx *DeviceIndex) Lookup(query string) (*DeviceEntity, IdentifierType, error) {
	// 1. Try direct device path or entity key
	if entity, ok := idx.Entities[query]; ok {
		return entity, IDDevicePath, nil
	}

	// 2. Try resolving as symlink path
	if resolved, err := filepath.EvalSymlinks(query); err == nil {
		if entity, ok := idx.Entities[resolved]; ok {
			return entity, IDSymlink, nil
		}
	}

	// 3. Try symlink map (for /dev/disk/by-* paths)
	if devPath, ok := idx.SymlinkMap[query]; ok {
		if entity, ok := idx.Entities[devPath]; ok {
			return entity, IDSymlink, nil
		}
	}

	// 4. Try each reverse index in order of specificity
	lookups := []struct {
		index  map[string]string
		idType IdentifierType
	}{
		{idx.ByKernelName, IDKernelName},
		{idx.BySerial, IDSerial},
		{idx.ByWWN, IDWWN},
		{idx.ByLUID, IDLUID},
		{idx.ByNGUID, IDNGUID},
		{idx.ByEUI64, IDEUI64},
		{idx.ByPartUUID, IDPartUUID},
		{idx.ByFSUUID, IDFSUUID},
		{idx.ByPartLabel, IDPartLabel},
		{idx.ByFSLabel, IDFSLabel},
		{idx.ByMajMin, IDMajMin},
		{idx.BySCSIAddr, IDSCSIAddr},
		{idx.ByIDPath, IDByID},
		{idx.ByPathPath, IDByPath},
		{idx.ByZFSPoolGUID, IDZFSPoolGUID},
		{idx.ByZFSPoolName, IDZFSPoolName},
		{idx.ByZFSDataGUID, IDZFSDataGUID},
		{idx.ByZFSDataName, IDZFSDataName},
		{idx.ByZFSVdevGUID, IDZFSVdevGUID},
		{idx.ByLVMPVUUID, IDLVMPVUUID},
		{idx.ByLVMVGUUID, IDLVMVGUUID},
		{idx.ByLVMVGName, IDLVMVGName},
		{idx.ByLVMLVUUID, IDLVMLVUUID},
		{idx.ByLVMLVName, IDLVMLVName},
		{idx.ByLVMLVPath, IDLVMLVPath},
		{idx.ByMDArrUUID, IDMDArrUUID},
		{idx.ByMDName, IDMDName},
		{idx.ByDMName, IDDMName},
		{idx.ByDMUUID, IDDMUUID},
	}

	for _, lookup := range lookups {
		if devPath, ok := lookup.index[query]; ok {
			if entity, ok := idx.Entities[devPath]; ok {
				return entity, lookup.idType, nil
			}
		}
	}

	return nil, IDUnknown, ErrNotFound
}
