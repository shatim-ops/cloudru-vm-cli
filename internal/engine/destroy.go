package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/shatim-ops/cloudru-vm-cli/internal/confirm"
	clerrors "github.com/shatim-ops/cloudru-vm-cli/internal/errors"
	"github.com/shatim-ops/cloudru-vm-cli/internal/state"
)

// DestroyOptions holds parameters for the destroy command.
type DestroyOptions struct {
	Force  bool // skip confirmation prompt (--force / --yes)
	DryRun bool // show what would be deleted and stop
}

// Destroy tears down the active deployment.
func (e *Engine) Destroy(ctx context.Context, opts DestroyOptions) error {
	statePath := state.DefaultPath(e.StateDir)
	stateFile, err := state.Read(statePath)
	if err != nil {
		return err
	}

	active := stateFile.ActiveDeployment()
	if active == nil {
		e.Printer.Success("No active deployment to destroy.")
		return nil
	}

	e.Printer.Step(1, 3, "Reading deployment info...")
	e.Printer.StepDone(fmt.Sprintf("VM %s (%s) in %s", active.VMID, active.VMIP, active.Region))

	// Without a terminal nobody can answer: confirm.Ask fails with exit code 3
	// instead of reading EOF as a refusal and exiting 0.
	proceed, err := confirm.Ask(ctx, destroyPreview(active), e.confirmOptions(opts.Force, opts.DryRun, false))
	if err != nil {
		return err
	}
	if !proceed {
		return nil // --dry-run
	}

	// Auth.
	e.Printer.Step(2, 3, "Authenticating...")
	tok, err := e.Auth.Token(ctx)
	if err != nil {
		return err
	}
	if ts, ok := e.Cloud.(interface{ SetToken(string) }); ok {
		ts.SetToken(tok.Value)
	}
	e.Printer.StepDone("Authenticated")

	// Delete VM.
	e.Printer.Step(3, 3, "Deleting VM...")
	active.Status = "destroying"
	active.UpdatedAt = time.Now()
	stateFile.UpsertDeployment(*active)
	_ = state.Write(statePath, stateFile)

	err = e.Cloud.DeleteVM(ctx, active.VMID)
	if err != nil {
		// If VM is already gone (VM_NOT_FOUND), that's fine.
		if cliErr, ok := err.(*clerrors.CLIError); ok && cliErr.Code == clerrors.CodeVMNotFound {
			e.Printer.StepDone("VM already deleted")
		} else {
			return err
		}
	} else {
		// Poll until gone.
		deadline := time.After(30 * time.Second)
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
	pollLoop:
		for {
			select {
			case <-deadline:
				e.Printer.Warning("Timeout waiting for VM deletion confirmation")
				break pollLoop
			case <-ticker.C:
				_, getErr := e.Cloud.GetVM(ctx, active.VMID)
				if getErr != nil {
					if cliErr, ok := getErr.(*clerrors.CLIError); ok && cliErr.Code == clerrors.CodeVMNotFound {
						break pollLoop
					}
				}
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		e.Printer.StepDone("VM deleted")
	}

	// Clean up state.
	stateFile.RemoveDeployment(active.ID)
	if err := state.Write(statePath, stateFile); err != nil {
		return err
	}

	e.Printer.Success("Deployment destroyed successfully.")
	return nil
}
