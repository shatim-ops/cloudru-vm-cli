// Package config implements configuration loading with ADR-008 precedence:
// CLI flags > env vars > config file > defaults.
package config

import (
	"fmt"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Config holds all configuration values for cloudru-vm.
type Config struct {
	// Auth
	APIKey string `yaml:"api_key" env:"CLOUDRU_API_KEY"` // legacy: "keyId:secret"
	KeyID  string `yaml:"key_id" env:"CLOUDRU_KEY_ID"`
	Secret string `yaml:"secret" env:"CLOUDRU_SECRET"`

	// Deploy
	ProjectID string `yaml:"project_id" env:"CLOUDRU_PROJECT_ID"` // required UUID
	Region    string `yaml:"region" env:"CLOUDRU_REGION"`          // availability_zone_id UUID
	Flavor    string `yaml:"flavor" env:"CLOUDRU_FLAVOR"`
	ImageID   string `yaml:"image_id" env:"CLOUDRU_IMAGE_ID"` // boot image UUID
	DiskSize  int    `yaml:"disk_size" env:"CLOUDRU_DISK_SIZE"`
	Timeout   int    `yaml:"timeout" env:"CLOUDRU_TIMEOUT"` // SSH timeout in seconds

	// Output
	JSON    bool `yaml:"-"` // --json flag only
	Verbose bool `yaml:"-"` // --verbose flag only
	NoColor bool `yaml:"-"` // --no-color flag only

	// Project
	ComposeFile string `yaml:"compose_file" env:"CLOUDRU_COMPOSE_FILE"`
	ProjectName string `yaml:"project_name" env:"CLOUDRU_PROJECT_NAME"`
}

// Source tracks where each config value came from (for doctor command).
type Source struct {
	Key    string
	Value  any
	Origin string // "default", "file", "env", "flag"
}

// Defaults returns a Config with sensible default values.
func Defaults() Config {
	return Config{
		Flavor:      "auto",
		DiskSize:    10,
		Timeout:     300,
		ComposeFile: "docker-compose.yml",
	}
}

// Load reads configuration from file path (if exists), applies env var overrides,
// then applies flag overrides. flags is a map of field names to values set via CLI flags.
// This implements ADR-008 precedence: flags > env > file > defaults.
func Load(filePath string, flags map[string]any) (Config, error) {
	cfg, _, err := loadInternal(filePath, flags, false)
	return cfg, err
}

// LoadWithSources returns config plus the origin of each value.
func LoadWithSources(filePath string, flags map[string]any) (Config, []Source, error) {
	return loadInternal(filePath, flags, true)
}

// envMapping maps env var names to config field names.
var envMapping = []struct {
	envVar string
	field  string
}{
	{"CLOUDRU_API_KEY", "APIKey"},
	{"CLOUDRU_PROJECT_ID", "ProjectID"},
	{"CLOUDRU_REGION", "Region"},
	{"CLOUDRU_FLAVOR", "Flavor"},
	{"CLOUDRU_IMAGE_ID", "ImageID"},
	{"CLOUDRU_DISK_SIZE", "DiskSize"},
	{"CLOUDRU_TIMEOUT", "Timeout"},
	{"CLOUDRU_COMPOSE_FILE", "ComposeFile"},
	{"CLOUDRU_PROJECT_NAME", "ProjectName"},
}

func loadInternal(filePath string, flags map[string]any, trackSources bool) (Config, []Source, error) {
	cfg := Defaults()

	// Track origins: start with defaults for fields that have them.
	origins := map[string]string{
		"Region":      "default",
		"Flavor":      "default",
		"Timeout":     "default",
		"ComposeFile": "default",
	}

	// Layer 2: config file (if exists).
	if filePath != "" {
		data, err := os.ReadFile(filePath)
		if err != nil {
			if !os.IsNotExist(err) {
				return Config{}, nil, fmt.Errorf("reading config file: %w", err)
			}
			// File doesn't exist — not an error, skip.
		} else {
			var fileCfg Config
			if err := yaml.Unmarshal(data, &fileCfg); err != nil {
				return Config{}, nil, fmt.Errorf("parsing config file: %w", err)
			}
			mergeFromFile(&cfg, &fileCfg, origins)
		}
	}

	// Layer 3: env vars.
	for _, m := range envMapping {
		val, ok := os.LookupEnv(m.envVar)
		if !ok {
			continue
		}
		if err := setField(&cfg, m.field, val); err != nil {
			return Config{}, nil, fmt.Errorf("env %s: %w", m.envVar, err)
		}
		origins[m.field] = "env"
	}

	// Layer 4: CLI flags (highest precedence).
	if flags != nil {
		for key, val := range flags {
			if err := setFieldAny(&cfg, key, val); err != nil {
				return Config{}, nil, fmt.Errorf("flag %s: %w", key, err)
			}
			origins[key] = "flag"
		}
	}

	if !trackSources {
		return cfg, nil, nil
	}

	sources := buildSources(&cfg, origins)
	return cfg, sources, nil
}

// mergeFromFile copies non-zero values from fileCfg into cfg.
func mergeFromFile(cfg, fileCfg *Config, origins map[string]string) {
	if fileCfg.APIKey != "" {
		cfg.APIKey = fileCfg.APIKey
		origins["APIKey"] = "file"
	}
	if fileCfg.ProjectID != "" {
		cfg.ProjectID = fileCfg.ProjectID
		origins["ProjectID"] = "file"
	}
	if fileCfg.Region != "" {
		cfg.Region = fileCfg.Region
		origins["Region"] = "file"
	}
	if fileCfg.Flavor != "" {
		cfg.Flavor = fileCfg.Flavor
		origins["Flavor"] = "file"
	}
	if fileCfg.ImageID != "" {
		cfg.ImageID = fileCfg.ImageID
		origins["ImageID"] = "file"
	}
	if fileCfg.DiskSize != 0 {
		cfg.DiskSize = fileCfg.DiskSize
		origins["DiskSize"] = "file"
	}
	if fileCfg.Timeout != 0 {
		cfg.Timeout = fileCfg.Timeout
		origins["Timeout"] = "file"
	}
	if fileCfg.ComposeFile != "" {
		cfg.ComposeFile = fileCfg.ComposeFile
		origins["ComposeFile"] = "file"
	}
	if fileCfg.ProjectName != "" {
		cfg.ProjectName = fileCfg.ProjectName
		origins["ProjectName"] = "file"
	}
}

// setField sets a Config field from a string value (used for env vars).
func setField(cfg *Config, field, val string) error {
	switch field {
	case "APIKey":
		cfg.APIKey = val
	case "ProjectID":
		cfg.ProjectID = val
	case "Region":
		cfg.Region = val
	case "Flavor":
		cfg.Flavor = val
	case "ImageID":
		cfg.ImageID = val
	case "DiskSize":
		n, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("invalid integer: %w", err)
		}
		cfg.DiskSize = n
	case "Timeout":
		n, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("invalid integer: %w", err)
		}
		cfg.Timeout = n
	case "ComposeFile":
		cfg.ComposeFile = val
	case "ProjectName":
		cfg.ProjectName = val
	default:
		return fmt.Errorf("unknown field %q", field)
	}
	return nil
}

// setFieldAny sets a Config field from an any value (used for flags).
func setFieldAny(cfg *Config, field string, val any) error {
	switch field {
	case "APIKey":
		cfg.APIKey = toString(val)
	case "ProjectID":
		cfg.ProjectID = toString(val)
	case "Region":
		cfg.Region = toString(val)
	case "Flavor":
		cfg.Flavor = toString(val)
	case "ImageID":
		cfg.ImageID = toString(val)
	case "DiskSize":
		n, err := toInt(val)
		if err != nil {
			return err
		}
		cfg.DiskSize = n
	case "Timeout":
		n, err := toInt(val)
		if err != nil {
			return err
		}
		cfg.Timeout = n
	case "ComposeFile":
		cfg.ComposeFile = toString(val)
	case "ProjectName":
		cfg.ProjectName = toString(val)
	case "JSON":
		cfg.JSON = toBool(val)
	case "Verbose":
		cfg.Verbose = toBool(val)
	case "NoColor":
		cfg.NoColor = toBool(val)
	default:
		return fmt.Errorf("unknown field %q", field)
	}
	return nil
}

func toString(v any) string {
	switch s := v.(type) {
	case string:
		return s
	default:
		return fmt.Sprintf("%v", v)
	}
}

func toInt(v any) (int, error) {
	switch n := v.(type) {
	case int:
		return n, nil
	case int64:
		return int(n), nil
	case float64:
		return int(n), nil
	case string:
		return strconv.Atoi(n)
	default:
		return 0, fmt.Errorf("cannot convert %T to int", v)
	}
}

func toBool(v any) bool {
	switch b := v.(type) {
	case bool:
		return b
	case string:
		return b == "true" || b == "1"
	default:
		return false
	}
}

// buildSources constructs Source slice from current config and origins.
func buildSources(cfg *Config, origins map[string]string) []Source {
	type entry struct {
		key string
		val any
	}
	fields := []entry{
		{"APIKey", cfg.APIKey},
		{"Region", cfg.Region},
		{"Flavor", cfg.Flavor},
		{"Timeout", cfg.Timeout},
		{"ComposeFile", cfg.ComposeFile},
		{"ProjectName", cfg.ProjectName},
		{"JSON", cfg.JSON},
		{"Verbose", cfg.Verbose},
		{"NoColor", cfg.NoColor},
	}

	sources := make([]Source, 0, len(fields))
	for _, f := range fields {
		origin := origins[f.key]
		if origin == "" {
			origin = "default"
		}
		sources = append(sources, Source{
			Key:    f.key,
			Value:  f.val,
			Origin: origin,
		})
	}
	return sources
}
