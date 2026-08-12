package engine

import (
	"context"
	"fmt"

	clerrors "github.com/shatim-ops/cloudru-vm-cli/internal/errors"
	"github.com/shatim-ops/cloudru-vm-cli/internal/state"
)

// LogsOptions holds parameters for the logs command.
type LogsOptions struct {
	Service string // specific service name (empty = all)
	Tail    int    // number of lines (0 = all)
	Follow  bool   // live tail
}

// Logs retrieves container logs from the deployed VM.
func (e *Engine) Logs(ctx context.Context, opts LogsOptions) error {
	statePath := state.DefaultPath(e.StateDir)
	stateFile, err := state.Read(statePath)
	if err != nil {
		return err
	}

	active := stateFile.ActiveDeployment()
	if active == nil {
		return clerrors.New(clerrors.CodeVMNotFound,
			"no active deployment",
			"Run 'cloudru-vm deploy' first.")
	}

	// Auth.
	tok, err := e.Auth.Token(ctx)
	if err != nil {
		return err
	}
	if ts, ok := e.Cloud.(interface{ SetToken(string) }); ok {
		ts.SetToken(tok.Value)
	}

	// Connect via SSH.
	sshUser := active.SSHUser
	if sshUser == "" {
		sshUser = sshDefaultUser
	}
	if err := e.Deployer.Connect(ctx, active.VMIP, 22, sshUser, e.SSHKeyPEM, active.SSHHostKeyFingerprint); err != nil {
		return err
	}
	defer e.Deployer.Close()

	// Build command.
	cmd := "cd /opt/app && docker compose logs"
	if opts.Tail > 0 {
		cmd += fmt.Sprintf(" --tail %d", opts.Tail)
	}
	if opts.Follow {
		cmd += " -f"
	}
	if opts.Service != "" {
		cmd += " " + opts.Service
	}

	output, err := e.Deployer.Exec(ctx, cmd)
	if err != nil {
		return clerrors.Wrap(clerrors.CodeSSHTimeout,
			"failed to retrieve logs",
			"Check that containers are running.",
			err)
	}

	fmt.Fprint(e.Printer.Writer(), output)
	return nil
}
