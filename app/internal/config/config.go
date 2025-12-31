package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
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

var defaultConfig = Config{
	Enclosures: []Enclosure{
		{
			Name: "jbod1",
			Drives: []Drive{
				{Name: "bay1", Device: "/dev/sdh"},
				{Name: "bay2", Device: "/dev/sdi"},
				{Name: "bay3", Device: "/dev/sdj"},
				{Name: "bay4", Device: "/dev/sdk"},
				{Name: "bay5", Device: "/dev/sdl"},
				{Name: "bay6", Device: "/dev/sdm"},
				{Name: "bay7", Device: "/dev/sdn"},
				{Name: "bay8", Device: "/dev/sdo"},
				{Name: "bay9", Device: "/dev/sdp"},
				{Name: "bay10", Device: "/dev/sdq"},
				{Name: "bay11", Device: "/dev/sdr"},
				{Name: "bay12", Device: "/dev/sds"},
			},
		},
	},
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

	if path == "" {
		// Return default config if no file found
		return &defaultConfig, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return &defaultConfig, nil
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) GetAllDrives() []Drive {
	var drives []Drive
	for _, enc := range c.Enclosures {
		drives = append(drives, enc.Drives...)
	}
	return drives
}
