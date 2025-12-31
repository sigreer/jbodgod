package sources

import (
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
)

// LsblkSource collects device information from lsblk
type LsblkSource struct{}

// lsblkOutput represents the JSON output from lsblk
type lsblkOutput struct {
	Blockdevices []lsblkDevice `json:"blockdevices"`
}

// lsblkDevice represents a single device in lsblk output
type lsblkDevice struct {
	Name      string        `json:"name"`
	Kname     string        `json:"kname"`
	Path      string        `json:"path"`
	MajMin    string        `json:"maj:min"`
	Type      string        `json:"type"`
	Size      string        `json:"size"`
	Serial    string        `json:"serial"`
	WWN       string        `json:"wwn"`
	Model     string        `json:"model"`
	Vendor    string        `json:"vendor"`
	PartUUID  string        `json:"partuuid"`
	PartLabel string        `json:"partlabel"`
	PartN     string        `json:"partn"`
	PKName    string        `json:"pkname"`
	UUID      string        `json:"uuid"`
	Label     string        `json:"label"`
	FSType    string        `json:"fstype"`
	Tran      string        `json:"tran"`
	HCTL      string        `json:"hctl"`
	Children  []lsblkDevice `json:"children,omitempty"`
}

// Collect gathers device information from lsblk
func (s *LsblkSource) Collect() (map[string]*SourceEntity, error) {
	entities := make(map[string]*SourceEntity)

	// Run lsblk with comprehensive columns
	cmd := exec.Command("lsblk", "-J", "-o",
		"NAME,KNAME,PATH,MAJ:MIN,TYPE,SIZE,SERIAL,WWN,MODEL,VENDOR,PARTUUID,PARTLABEL,PARTN,PKNAME,UUID,LABEL,FSTYPE,TRAN,HCTL")
	out, err := cmd.Output()
	if err != nil {
		return entities, err
	}

	var output lsblkOutput
	if err := json.Unmarshal(out, &output); err != nil {
		return entities, err
	}

	// Process devices recursively
	for _, dev := range output.Blockdevices {
		s.processDevice(dev, entities)
	}

	return entities, nil
}

func (s *LsblkSource) processDevice(dev lsblkDevice, entities map[string]*SourceEntity) {
	entity := &SourceEntity{
		Type:       dev.Type,
		DevicePath: dev.Path,
		KernelName: dev.Kname,
	}

	// Set optional string fields
	if dev.Serial != "" {
		entity.Serial = ptr(dev.Serial)
	}
	if dev.WWN != "" {
		entity.WWN = ptr(dev.WWN)
	}
	if dev.Model != "" {
		entity.Model = ptr(strings.TrimSpace(dev.Model))
	}
	if dev.Vendor != "" {
		entity.Vendor = ptr(strings.TrimSpace(dev.Vendor))
	}
	if dev.MajMin != "" {
		entity.MajMin = ptr(dev.MajMin)
	}
	if dev.Size != "" {
		entity.Size = ptr(dev.Size)
	}
	if dev.HCTL != "" {
		entity.SCSIAddr = ptr(dev.HCTL)
	}
	if dev.Tran != "" {
		entity.Transport = ptr(dev.Tran)
	}

	// Partition-specific fields
	if dev.PartUUID != "" {
		entity.PartUUID = ptr(dev.PartUUID)
	}
	if dev.PartLabel != "" {
		entity.PartLabel = ptr(dev.PartLabel)
	}
	if dev.PartN != "" {
		if n, err := strconv.Atoi(dev.PartN); err == nil {
			entity.PartNum = &n
		}
	}
	if dev.PKName != "" {
		parent := "/dev/" + dev.PKName
		entity.ParentDisk = ptr(parent)
	}

	// Filesystem fields
	if dev.UUID != "" {
		entity.FSUUID = ptr(dev.UUID)
	}
	if dev.Label != "" {
		entity.FSLabel = ptr(dev.Label)
	}
	if dev.FSType != "" {
		entity.FSType = ptr(dev.FSType)
	}

	// Store by device path
	if dev.Path != "" {
		entities[dev.Path] = entity
	}

	// Process children recursively
	for _, child := range dev.Children {
		s.processDevice(child, entities)
	}
}

// ptr creates a pointer to a string
func ptr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
