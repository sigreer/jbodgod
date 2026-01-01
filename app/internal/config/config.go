package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	// Discovery mode: "auto", "lsscsi", "hba", or "static" (default if drives specified)
	Discovery  string      `yaml:"discovery,omitempty"`
	Enclosures []Enclosure `yaml:"enclosures"`
	Thresholds Thresholds  `yaml:"thresholds"`
	Alerts     Alerts      `yaml:"alerts"`
}

type Enclosure struct {
	Name   string  `yaml:"name"`
	Drives []Drive `yaml:"drives"`
}

type Drive struct {
	Name   string `yaml:"name"`
	Device string `yaml:"device"`
	UUID   string `yaml:"uuid,omitempty"`
}

type Thresholds struct {
	WarningTemp      int    `yaml:"warning_temp"`
	CriticalTemp     int    `yaml:"critical_temp"`
	ActionOnCritical string `yaml:"action_on_critical"`
}

type Alerts struct {
	Email   string `yaml:"email,omitempty"`
	Webhook string `yaml:"webhook,omitempty"`
}

// defaultConfig provides baseline settings; drives are discovered dynamically
var defaultConfig = Config{
	Discovery: "auto",
	Thresholds: Thresholds{
		WarningTemp:      55,
		CriticalTemp:     60,
		ActionOnCritical: "alert",
	},
}

func Load(path string) (*Config, error) {
	if path == "" {
		// Try default locations
		candidates := []string{
			"/etc/jbodgod/config.yaml",
			filepath.Join(os.Getenv("HOME"), ".config/jbodgod/config.yaml"),
			"config.yaml",
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				path = c
				break
			}
		}
	}

	var cfg Config
	if path == "" {
		// No config file found - use defaults with auto-discovery
		cfg = defaultConfig
	} else {
		data, err := os.ReadFile(path)
		if err != nil {
			cfg = defaultConfig
		} else {
			if err := yaml.Unmarshal(data, &cfg); err != nil {
				return nil, err
			}
		}
	}

	// Apply defaults for missing thresholds
	if cfg.Thresholds.WarningTemp == 0 {
		cfg.Thresholds.WarningTemp = defaultConfig.Thresholds.WarningTemp
	}
	if cfg.Thresholds.CriticalTemp == 0 {
		cfg.Thresholds.CriticalTemp = defaultConfig.Thresholds.CriticalTemp
	}
	if cfg.Thresholds.ActionOnCritical == "" {
		cfg.Thresholds.ActionOnCritical = defaultConfig.Thresholds.ActionOnCritical
	}

	// Determine discovery mode
	discoveryMode := cfg.Discovery
	if discoveryMode == "" {
		// If drives are explicitly configured, use static mode
		if len(cfg.GetAllDrives()) > 0 {
			discoveryMode = "static"
		} else {
			discoveryMode = "auto"
		}
	}

	// If not static mode, discover drives dynamically
	if discoveryMode != "static" {
		drives, err := discoverDrivesWithMode(discoveryMode)
		if err != nil {
			return nil, fmt.Errorf("drive discovery failed: %w", err)
		}

		if len(drives) > 0 {
			cfg.Enclosures = []Enclosure{
				{
					Name:   "discovered",
					Drives: drives,
				},
			}
		}
	}

	return &cfg, nil
}

// discoverDrivesWithMode discovers drives using the specified mode
func discoverDrivesWithMode(mode string) ([]Drive, error) {
	switch mode {
	case "lsscsi":
		return DiscoverDrives()
	case "hba":
		return DiscoverDrivesFromHBA()
	case "auto":
		// Try HBA first (more accurate for JBOD), fall back to lsscsi
		drives, err := DiscoverDrivesFromHBA()
		if err == nil && len(drives) > 0 {
			return drives, nil
		}
		return DiscoverDrives()
	default:
		return DiscoverDrives()
	}
}

func (c *Config) GetAllDrives() []Drive {
	var drives []Drive
	for _, enc := range c.Enclosures {
		drives = append(drives, enc.Drives...)
	}
	return drives
}
