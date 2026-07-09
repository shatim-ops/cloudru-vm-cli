package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaults(t *testing.T) {
	cfg := Defaults()

	if cfg.Region != "" {
		t.Errorf("Region: got %q, want %q", cfg.Region, "")
	}
	if cfg.Flavor != "auto" {
		t.Errorf("Flavor: got %q, want %q", cfg.Flavor, "auto")
	}
	if cfg.Timeout != 300 {
		t.Errorf("Timeout: got %d, want %d", cfg.Timeout, 300)
	}
	if cfg.ComposeFile != "docker-compose.yml" {
		t.Errorf("ComposeFile: got %q, want %q", cfg.ComposeFile, "docker-compose.yml")
	}
	// Zero-value fields.
	if cfg.APIKey != "" {
		t.Errorf("APIKey: got %q, want empty", cfg.APIKey)
	}
	if cfg.ProjectName != "" {
		t.Errorf("ProjectName: got %q, want empty", cfg.ProjectName)
	}
}

func TestLoad_FileOverridesDefaults(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")

	data := []byte("region: ru-moscow-2\nflavor: m1.small\n")
	if err := os.WriteFile(cfgFile, data, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgFile, nil)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Region != "ru-moscow-2" {
		t.Errorf("Region: got %q, want %q", cfg.Region, "ru-moscow-2")
	}
	if cfg.Flavor != "m1.small" {
		t.Errorf("Flavor: got %q, want %q", cfg.Flavor, "m1.small")
	}
	// Default preserved for fields not in file.
	if cfg.Timeout != 300 {
		t.Errorf("Timeout: got %d, want %d (default)", cfg.Timeout, 300)
	}
}

func TestLoad_EnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")

	data := []byte("region: ru-moscow-2\n")
	if err := os.WriteFile(cfgFile, data, 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("CLOUDRU_REGION", "ru-spb-1")

	cfg, err := Load(cfgFile, nil)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Region != "ru-spb-1" {
		t.Errorf("Region: got %q, want %q (from env)", cfg.Region, "ru-spb-1")
	}
}

func TestLoad_FlagOverridesEnv(t *testing.T) {
	t.Setenv("CLOUDRU_REGION", "ru-spb-1")

	flags := map[string]any{
		"Region": "eu-west-1",
	}

	cfg, err := Load("", flags)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Region != "eu-west-1" {
		t.Errorf("Region: got %q, want %q (from flag)", cfg.Region, "eu-west-1")
	}
}

func TestLoad_MissingFile(t *testing.T) {
	cfg, err := Load("/tmp/nonexistent-cloudru-config-12345.yaml", nil)
	if err != nil {
		t.Fatalf("unexpected error for missing file: %v", err)
	}

	// Should equal defaults.
	defaults := Defaults()
	if cfg.Region != defaults.Region {
		t.Errorf("Region: got %q, want %q", cfg.Region, defaults.Region)
	}
}

func TestLoad_EmptyPath(t *testing.T) {
	cfg, err := Load("", nil)
	if err != nil {
		t.Fatal(err)
	}

	defaults := Defaults()
	if cfg.Region != defaults.Region {
		t.Errorf("Region: got %q, want %q", cfg.Region, defaults.Region)
	}
}

func TestLoad_FullPrecedence(t *testing.T) {
	// File sets region + flavor, env overrides region, flag overrides region again.
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")

	data := []byte("region: from-file\nflavor: from-file\n")
	if err := os.WriteFile(cfgFile, data, 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("CLOUDRU_REGION", "from-env")

	flags := map[string]any{
		"Region": "from-flag",
	}

	cfg, err := Load(cfgFile, flags)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Region != "from-flag" {
		t.Errorf("Region: got %q, want %q", cfg.Region, "from-flag")
	}
	if cfg.Flavor != "from-file" {
		t.Errorf("Flavor: got %q, want %q", cfg.Flavor, "from-file")
	}
}

func TestLoad_EnvTimeout(t *testing.T) {
	t.Setenv("CLOUDRU_TIMEOUT", "60")

	cfg, err := Load("", nil)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Timeout != 60 {
		t.Errorf("Timeout: got %d, want %d", cfg.Timeout, 60)
	}
}

func TestLoad_FlagBooleans(t *testing.T) {
	flags := map[string]any{
		"JSON":    true,
		"Verbose": true,
		"NoColor": true,
	}

	cfg, err := Load("", flags)
	if err != nil {
		t.Fatal(err)
	}

	if !cfg.JSON {
		t.Error("JSON: got false, want true")
	}
	if !cfg.Verbose {
		t.Error("Verbose: got false, want true")
	}
	if !cfg.NoColor {
		t.Error("NoColor: got false, want true")
	}
}

func TestLoadWithSources(t *testing.T) {
	// Clear any ambient CLOUDRU_* env vars that could interfere.
	for _, env := range []string{
		"CLOUDRU_API_KEY", "CLOUDRU_REGION", "CLOUDRU_FLAVOR",
		"CLOUDRU_TIMEOUT", "CLOUDRU_COMPOSE_FILE", "CLOUDRU_PROJECT_NAME",
	} {
		if orig, ok := os.LookupEnv(env); ok {
			t.Cleanup(func() { os.Setenv(env, orig) })
		} else {
			t.Cleanup(func() { os.Unsetenv(env) })
		}
		os.Unsetenv(env)
	}

	// Now set only the one we want for this test.
	os.Setenv("CLOUDRU_FLAVOR", "from-env")
	t.Cleanup(func() { os.Unsetenv("CLOUDRU_FLAVOR") })

	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")

	data := []byte("region: from-file\napi_key: secret-from-file\n")
	if err := os.WriteFile(cfgFile, data, 0644); err != nil {
		t.Fatal(err)
	}

	flags := map[string]any{
		"Verbose": true,
	}

	cfg, sources, err := LoadWithSources(cfgFile, flags)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Region != "from-file" {
		t.Errorf("Region: got %q, want %q", cfg.Region, "from-file")
	}

	// Build a lookup for easy assertions.
	originOf := make(map[string]string)
	for _, s := range sources {
		originOf[s.Key] = s.Origin
	}

	expectations := map[string]string{
		"APIKey":      "file",
		"Region":      "file",
		"Flavor":      "env",
		"Timeout":     "default",
		"ComposeFile": "default",
		"Verbose":     "flag",
		"JSON":        "default",
		"NoColor":     "default",
		"ProjectName": "default",
	}

	for key, wantOrigin := range expectations {
		got := originOf[key]
		if got != wantOrigin {
			t.Errorf("Source %q: origin got %q, want %q", key, got, wantOrigin)
		}
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")

	data := []byte("region: [invalid yaml\n")
	if err := os.WriteFile(cfgFile, data, 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(cfgFile, nil)
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

func TestLoad_InvalidEnvTimeout(t *testing.T) {
	t.Setenv("CLOUDRU_TIMEOUT", "not-a-number")

	_, err := Load("", nil)
	if err == nil {
		t.Fatal("expected error for non-integer CLOUDRU_TIMEOUT, got nil")
	}
}
