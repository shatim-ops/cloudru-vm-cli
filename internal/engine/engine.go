package engine

import (
	"context"
	"time"

	"github.com/shatim-ops/cloudru-vm-cli/internal/auth"
	"github.com/shatim-ops/cloudru-vm-cli/internal/config"
	"github.com/shatim-ops/cloudru-vm-cli/internal/deploy"
	"github.com/shatim-ops/cloudru-vm-cli/internal/output"
	"github.com/shatim-ops/cloudru-vm-cli/internal/provision"
	"github.com/shatim-ops/cloudru-vm-cli/internal/verify"
)

// Engine orchestrates all CLI operations.
type Engine struct {
	Config    config.Config
	Auth      auth.TokenProvider
	Cloud     provision.CloudAPI
	Deployer  deploy.Deployer
	Printer   *output.Printer
	SSHKeyPEM []byte // Ed25519 private key for SSH
	SSHPubKey string // Ed25519 public key for cloud-init
	StateDir  string // project root (for state file)

	// Timings overrides poll/retry waits; zero fields keep production defaults.
	Timings Timings
	// Probe runs post-deploy health checks; nil means verify.Check.
	Probe func(ctx context.Context, ip string, ports []int, timeout time.Duration) []verify.ProbeResult
}

// DeployOptions holds parameters for the deploy command.
type DeployOptions struct {
	ComposeFile    string
	Flavor         string // "auto" or explicit flavor name
	Region         string
	Force          bool // force redeploy even if hash unchanged
	NonInteractive bool // never prompt; creating a VM then needs Yes
	Yes            bool // consent to create the VM given in advance (--yes)
	DryRun         bool // show what would be created and its cost, then stop
}
