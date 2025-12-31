package sources

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
)

// LVMSource collects LVM PV, VG, and LV information
type LVMSource struct{}

// pvReport represents pvs JSON output
type pvReport struct {
	Report []struct {
		PV []struct {
			PVName string `json:"pv_name"`
			PVUUID string `json:"pv_uuid"`
			VGName string `json:"vg_name"`
		} `json:"pv"`
	} `json:"report"`
}

// vgReport represents vgs JSON output
type vgReport struct {
	Report []struct {
		VG []struct {
			VGName string `json:"vg_name"`
			VGUUID string `json:"vg_uuid"`
		} `json:"vg"`
	} `json:"report"`
}

// lvReport represents lvs JSON output
type lvReport struct {
	Report []struct {
		LV []struct {
			LVName string `json:"lv_name"`
			LVUUID string `json:"lv_uuid"`
			VGName string `json:"vg_name"`
			LVPath string `json:"lv_path"`
		} `json:"lv"`
	} `json:"report"`
}

// Collect gathers LVM information
func (s *LVMSource) Collect() (map[string]*SourceEntity, error) {
	entities := make(map[string]*SourceEntity)

	// Check if LVM is available
	if _, err := exec.LookPath("pvs"); err != nil {
		return entities, nil
	}

	// Get VG info for UUID lookup
	vgUUIDs := s.getVGUUIDs()

	// Get PV information
	pvs := s.getPVs()
	for _, pv := range pvs {
		// Resolve to actual device path
		devPath := s.resolveDevice(pv.PVName)

		entity := &SourceEntity{
			Type:        "lvm_pv",
			DevicePath:  devPath,
			LVMPVDevice: ptr(pv.PVName),
			LVMPVUUID:   ptr(pv.PVUUID),
		}

		if pv.VGName != "" {
			entity.LVMVGName = ptr(pv.VGName)
			if uuid, ok := vgUUIDs[pv.VGName]; ok {
				entity.LVMVGUUID = ptr(uuid)
			}
		}

		entities[devPath] = entity
	}

	// Get VG information (as separate entities)
	for vgName, vgUUID := range vgUUIDs {
		entity := &SourceEntity{
			Type:      "lvm_vg",
			LVMVGName: ptr(vgName),
			LVMVGUUID: ptr(vgUUID),
		}
		key := "lvm:vg:" + vgName
		entities[key] = entity
	}

	// Get LV information
	lvs := s.getLVs()
	for _, lv := range lvs {
		devPath := s.resolveDevice(lv.LVPath)

		entity := &SourceEntity{
			Type:       "lvm_lv",
			DevicePath: devPath,
			LVMLVName:  ptr(lv.LVName),
			LVMLVUUID:  ptr(lv.LVUUID),
			LVMLVPath:  ptr(lv.LVPath),
		}

		if lv.VGName != "" {
			entity.LVMVGName = ptr(lv.VGName)
			if uuid, ok := vgUUIDs[lv.VGName]; ok {
				entity.LVMVGUUID = ptr(uuid)
			}
		}

		entities[devPath] = entity
	}

	return entities, nil
}

// getPVs returns physical volume information
func (s *LVMSource) getPVs() []struct {
	PVName string
	PVUUID string
	VGName string
} {
	var pvs []struct {
		PVName string
		PVUUID string
		VGName string
	}

	out, err := exec.Command("pvs", "--reportformat", "json", "-o", "pv_name,pv_uuid,vg_name").Output()
	if err != nil {
		return pvs
	}

	var report pvReport
	if err := json.Unmarshal(out, &report); err != nil {
		return pvs
	}

	for _, r := range report.Report {
		for _, pv := range r.PV {
			pvs = append(pvs, struct {
				PVName string
				PVUUID string
				VGName string
			}{
				PVName: pv.PVName,
				PVUUID: pv.PVUUID,
				VGName: pv.VGName,
			})
		}
	}

	return pvs
}

// getVGUUIDs returns a map of VG name -> VG UUID
func (s *LVMSource) getVGUUIDs() map[string]string {
	result := make(map[string]string)

	out, err := exec.Command("vgs", "--reportformat", "json", "-o", "vg_name,vg_uuid").Output()
	if err != nil {
		return result
	}

	var report vgReport
	if err := json.Unmarshal(out, &report); err != nil {
		return result
	}

	for _, r := range report.Report {
		for _, vg := range r.VG {
			result[vg.VGName] = vg.VGUUID
		}
	}

	return result
}

// getLVs returns logical volume information
func (s *LVMSource) getLVs() []struct {
	LVName string
	LVUUID string
	VGName string
	LVPath string
} {
	var lvs []struct {
		LVName string
		LVUUID string
		VGName string
		LVPath string
	}

	out, err := exec.Command("lvs", "--reportformat", "json", "-o", "lv_name,lv_uuid,vg_name,lv_path").Output()
	if err != nil {
		return lvs
	}

	var report lvReport
	if err := json.Unmarshal(out, &report); err != nil {
		return lvs
	}

	for _, r := range report.Report {
		for _, lv := range r.LV {
			lvs = append(lvs, struct {
				LVName string
				LVUUID string
				VGName string
				LVPath string
			}{
				LVName: lv.LVName,
				LVUUID: lv.LVUUID,
				VGName: lv.VGName,
				LVPath: lv.LVPath,
			})
		}
	}

	return lvs
}

// resolveDevice resolves a device path to its canonical form
func (s *LVMSource) resolveDevice(device string) string {
	if device == "" {
		return ""
	}

	resolved, err := filepath.EvalSymlinks(device)
	if err != nil {
		return device
	}
	return resolved
}
