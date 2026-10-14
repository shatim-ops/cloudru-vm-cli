package engine

import (
	"bufio"
	"context"
	stderrors "errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	clerrors "github.com/shatim-ops/cloudru-vm-cli/internal/errors"
	"github.com/shatim-ops/cloudru-vm-cli/internal/state"
)

// DestroyOptions holds parameters for the destroy command.
type DestroyOptions struct {
	Force bool // skip confirmation prompt
}

// promptStdin is where confirmation answers (destroy, deploy interrupt) are
// read from; promptStdinIsTTY tells whether a human can answer at all.
// Variables so tests can replace them.
var (
	promptStdin      io.Reader = os.Stdin
	promptStdinIsTTY           = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }
)

// Destroy tears down the active deployment.
func (e *Engine) Destroy(ctx context.Context, opts DestroyOptions) error {
	statePath := state.DefaultPath(e.StateDir)
	stateFile, err := state.Read(statePath)
	if err != nil {
		return err
	}

	active := stateFile.ActiveDeployment()

	// Without a terminal nobody can answer the prompt: reading stdin would
	// hit EOF and look like a refusal. Fail loudly instead of exiting 0.
	if !opts.Force && !promptStdinIsTTY() {
		target := "the deployment"
		if active != nil {
			target = "VM " + active.VMID
		}
		return clerrors.New(clerrors.CodeConfirmationRequired,
			fmt.Sprintf("refusing to destroy %s: confirmation required and stdin is not a terminal", target),
			"Re-run with --force to destroy without a prompt.")
	}

	if active == nil {
		e.Printer.Success("No active deployment to destroy.")
		return nil
	}

	e.Printer.Step(1, 3, "Reading deployment info...")
	e.Printer.StepDone(fmt.Sprintf("VM %s (%s) in %s", active.VMID, active.VMIP, active.Region))

	if !opts.Force {
		e.Printer.Warning(fmt.Sprintf("This will permanently delete VM %s and all data on it.", active.VMID))
		fmt.Fprintf(e.Printer.ErrWriter(), "Type 'yes' to confirm: ")
		answer, err := readAnswer(ctx, promptStdin)
		if err != nil {
			return err
		}
		if answer != "yes" {
			return clerrors.New(clerrors.CodeConfirmationRequired,
				"destruction cancelled: confirmation not given",
				"Type 'yes' at the prompt, or re-run with --force.")
		}
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

	// Delete VM, then its floating IP.
	e.Printer.Step(3, 3, "Deleting VM and floating IP...")
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
		t := e.timings()
		deadline := time.After(t.DestroyPollTimeout)
		ticker := time.NewTicker(t.DestroyPollInterval)
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

	// The VM is gone but its floating IP is billed until released. On failure
	// the record stays ("destroying"), so re-running destroy retries.
	if err := e.releaseFloatingIP(ctx, active); err != nil {
		stateFile.UpsertDeployment(*active)
		_ = state.Write(statePath, stateFile)
		return err
	}

	// Clean up state.
	stateFile.RemoveDeployment(active.ID)
	if err := state.Write(statePath, stateFile); err != nil {
		return err
	}

	e.Printer.Success("Deployment destroyed successfully.")
	return nil
}

// fipReleaseAttempts bounds DeleteFloatingIP retries: right after the VM is
// deleted the platform may still report the FIP as attached.
const fipReleaseAttempts = 5

// releaseFloatingIP deletes the deployment's floating IP. The ID comes from
// state; records written before floating_ip_id existed fall back to looking
// the address up in list_floating_ips. Nothing to release is not an error.
func (e *Engine) releaseFloatingIP(ctx context.Context, rec *state.DeploymentRecord) error {
	projectID := e.Config.ProjectID
	fipID := rec.FloatingIPID
	if fipID == "" && rec.VMIP != "" {
		fips, err := e.Cloud.ListFloatingIPs(ctx, projectID)
		if err != nil {
			return clerrors.Wrap(clerrors.CodeAPIError,
				fmt.Sprintf("cannot look up floating IP %s", rec.VMIP),
				"The VM is deleted. Re-run 'cloudru-vm destroy --force' to release the floating IP, or release it in the Cloud.ru console.",
				err)
		}
		for _, f := range fips {
			if f.IPAddress == rec.VMIP {
				fipID = f.ID
				break
			}
		}
	}
	if fipID == "" {
		return nil
	}

	t := e.timings()
	var lastErr error
	for attempt := 0; attempt < fipReleaseAttempts; attempt++ {
		err := e.Cloud.DeleteFloatingIP(ctx, fipID, projectID)
		if err == nil {
			e.Printer.StepDone(fmt.Sprintf("Floating IP %s released", fipLabel(rec.VMIP, fipID)))
			return nil
		}
		var cliErr *clerrors.CLIError
		if stderrors.As(err, &cliErr) && cliErr.Code == clerrors.CodeVMNotFound {
			e.Printer.StepDone("Floating IP already released")
			return nil
		}
		lastErr = err
		if attempt == fipReleaseAttempts-1 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(t.FIPRetryInterval):
		}
	}
	return clerrors.Wrap(clerrors.CodeAPIError,
		fmt.Sprintf("VM deleted, but floating IP %s was not released", fipLabel(rec.VMIP, fipID)),
		"The floating IP is still billed. Re-run 'cloudru-vm destroy --force' or release it in the Cloud.ru console.",
		lastErr)
}

func fipLabel(ip, id string) string {
	if ip == "" {
		return id
	}
	return fmt.Sprintf("%s (%s)", ip, id)
}

// readAnswer reads one line from r, giving up when ctx is cancelled
// (Ctrl-C at the prompt).
func readAnswer(ctx context.Context, r io.Reader) (string, error) {
	ch := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(r).ReadString('\n')
		ch <- strings.TrimSpace(line)
	}()
	select {
	case answer := <-ch:
		return answer, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}
