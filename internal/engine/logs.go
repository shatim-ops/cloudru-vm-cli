package engine

import (
	"context"
	"fmt"

	"github.com/shatim-ops/cloudru-vm-cli/internal/deploy"
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

	if err := e.EnsureSSHKey(); err != nil {
		return err
	}

	// Connect via SSH.
	sshUser, err := sshUserOf(active)
	if err != nil {
		return err
	}
	if err := e.Deployer.Connect(ctx, active.VMIP, 22, sshUser, e.SSHKeyPEM, active.SSHHostKeyFingerprint); err != nil {
		return err
	}
	defer e.Deployer.Close()

	// Stream instead of collecting: --follow never ends on its own, and a
	// long log would not fit Exec's 120 s limit. Ctrl-C (ctx) ends the stream.
	err = e.Deployer.ExecStream(ctx, logsCommand(opts), e.Printer.Writer(), e.Printer.ErrWriter())
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return clerrors.Wrap(clerrors.CodeSSHTimeout,
			"failed to retrieve logs",
			"Check that containers are running.",
			err)
	}
	return nil
}

// logsCommand builds the remote `docker compose logs` command. The service
// name is quoted: it comes from the user and goes to a remote shell.
func logsCommand(opts LogsOptions) string {
	cmd := "cd " + remoteComposeDir + " && docker compose logs"
	if opts.Tail > 0 {
		cmd += fmt.Sprintf(" --tail %d", opts.Tail)
	}
	if opts.Follow {
		cmd += " -f"
	}
	if opts.Service != "" {
		cmd += " " + deploy.ShellQuote(opts.Service)
	}
	return cmd
}
