package bootstrap

import (
	"fmt"

	"github.com/shatim-ops/cloudru-vm-cli/internal/errors"
)

// Config holds parameters for cloud-init template rendering.
type Config struct {
	SSHPublicKey   string // Ed25519 public key
	Ports          []int  // firewall allow list (always includes 22)
	SwapSizeMB     int    // default: 1024 for free tier
	DockerLogMax   string // default: "10m"
	ComposeVersion string // docker compose plugin version
}

// Defaults returns a Config with sensible defaults.
func Defaults() Config {
	return Config{
		SwapSizeMB:     1024,
		DockerLogMax:   "10m",
		ComposeVersion: "v2.24.0",
	}
}

// Validate checks the config for errors.
//   - SSHPublicKey must not be empty
//   - Ports must include 22 (added automatically if missing)
//   - SwapSizeMB must be > 0
func (c *Config) Validate() error {
	if c.SSHPublicKey == "" {
		return errors.New(
			errors.CodeConfigInvalid,
			"SSH public key is required",
			"Provide an Ed25519 public key via --ssh-key or config file",
		)
	}

	if c.SwapSizeMB <= 0 {
		return errors.New(
			errors.CodeConfigInvalid,
			fmt.Sprintf("swap size must be > 0, got %d", c.SwapSizeMB),
			"Set a positive swap size in MB (default: 1024)",
		)
	}

	// Ensure port 22 is always present.
	has22 := false
	for _, p := range c.Ports {
		if p == 22 {
			has22 = true
			break
		}
	}
	if !has22 {
		c.Ports = append(c.Ports, 22)
	}

	return nil
}

// Render generates the cloud-init YAML user-data script from config.
// Calls Validate() first. Returns CLIError(CONFIG_INVALID) on validation failure.
func Render(cfg Config) (string, error) {
	if err := cfg.Validate(); err != nil {
		return "", err
	}
	return renderTemplate(cfg)
}
