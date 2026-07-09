package engine

import (
	"github.com/dzhechko/cloudru-vm-cli/internal/auth"
	"github.com/dzhechko/cloudru-vm-cli/internal/config"
	"github.com/dzhechko/cloudru-vm-cli/internal/deploy"
	"github.com/dzhechko/cloudru-vm-cli/internal/output"
	"github.com/dzhechko/cloudru-vm-cli/internal/provision"
)

// Engine orchestrates all CLI operations.
type Engine struct {
	Config    config.Config
	Auth      auth.TokenProvider
	Cloud     provision.CloudAPI
	Deployer  deploy.Deployer
	Printer   *output.Printer
	SSHKeyPEM []byte  // Ed25519 private key for SSH
	SSHPubKey string  // Ed25519 public key for cloud-init
	StateDir  string  // project root (for state file)
}

// DeployOptions holds parameters for the deploy command.
type DeployOptions struct {
	ComposeFile    string
	Flavor         string // "auto" or explicit flavor name
	Region         string
	Force          bool // force redeploy even if hash unchanged
	NonInteractive bool
}
