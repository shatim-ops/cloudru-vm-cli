package engine

import (
	"context"
	stderrors "errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/confirm"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/sgrules"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// DestroyOptions holds parameters for the destroy command.
type DestroyOptions struct {
	Force  bool // skip confirmation prompt (--force / --yes)
	DryRun bool // show what would be deleted and stop
}

// Destroy tears down the active deployment.
func (e *Engine) Destroy(ctx context.Context, opts DestroyOptions) error {
	statePath := state.DefaultPath(e.StateDir)
	if !opts.DryRun {
		unlock, err := state.Lock(statePath)
		if err != nil {
			return err
		}
		defer unlock()
	}
	stateFile, err := state.Read(statePath)
	if err != nil {
		return err
	}

	active := stateFile.ActiveDeployment()
	// Deployments destroyed earlier whose port rules could not be deleted
	// then: this run tries again.
	pending := sgrules.Pending(stateFile)
	if active == nil {
		if len(pending) > 0 {
			return e.finishPendingRules(ctx, opts, statePath, stateFile, pending)
		}
		if e.Printer.IsJSON() {
			return e.Printer.JSON(map[string]any{"status": "none", "message": "No active deployment to destroy."})
		}
		e.Printer.Success("No active deployment to destroy.")
		return nil
	}

	e.Printer.Step(1, 3, "Reading deployment info...")
	e.Printer.StepDone(fmt.Sprintf("VM %s (%s) in %s", active.VMID, active.VMIP, active.Region))

	// Without a terminal nobody can answer: confirm.Ask fails with exit code 3
	// instead of reading EOF as a refusal and exiting 0.
	proceed, err := confirm.Ask(ctx, destroyPreview(active, stateFile, pending), e.confirmOptions(opts.Force, opts.DryRun, false))
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

	// Delete VM, then its floating IP and the port rules deploy created.
	e.Printer.Step(3, 3, "Deleting VM, floating IP and port rules...")
	prevStatus := active.Status
	active.Status = "destroying"
	active.UpdatedAt = time.Now()
	stateFile.UpsertDeployment(*active)
	_ = state.Write(statePath, stateFile)

	if active.VMID == "" {
		// No ID to delete by: DELETE /vms/ with an empty ID is not a VM.
		e.Printer.Warning("No VM ID in state; nothing to delete in the cloud")
	} else if err = e.deleteVMWhenDeletable(ctx, active.VMID); err != nil {
		// If VM is already gone (VM_NOT_FOUND), that's fine.
		var cliErr *clerrors.CLIError
		if stderrors.As(err, &cliErr) && cliErr.Code == clerrors.CodeVMNotFound {
			e.Printer.StepDone("VM already deleted")
		} else {
			// The VM is still there: the record keeps saying what it was.
			active.Status = prevStatus
			stateFile.UpsertDeployment(*active)
			_ = state.Write(statePath, stateFile)
			if st := provision.HTTPStatus(err); st == http.StatusConflict || st == http.StatusUnprocessableEntity {
				return clerrors.Wrap(clerrors.CodeAPIError,
					fmt.Sprintf("VM %s cannot be deleted in its current state", active.VMID),
					fmt.Sprintf("It is probably still being created or changing state (waited %s). Wait until it is running "+
						"(Cloud.ru console, or 'evo vm get %s') and re-run 'evo destroy': the record is kept, nothing is lost.",
						e.timings().DeletableWaitTimeout, active.VMID),
					err)
			}
			return err
		}
	} else {
		// Poll until gone.
		t := e.timings()
		deadline := time.After(t.DestroyPollTimeout)
		ticker := time.NewTicker(t.DestroyPollInterval)
		defer ticker.Stop()
		confirmed := false
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
						confirmed = true
						break pollLoop
					}
				}
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if confirmed {
			e.Printer.StepDone("VM deleted")
		} else {
			e.Printer.StepDone(fmt.Sprintf("VM deletion requested (not confirmed within %s)", t.DestroyPollTimeout))
		}
	}

	// The VM is gone but its floating IP is billed until released. On failure
	// the record stays ("destroying"), so re-running destroy retries.
	stateFile.UpsertDeployment(*active)
	fipErr := e.releaseFloatingIP(ctx, active)

	// Port rules only now that the VM is gone: had its delete failed, the
	// app would still be serving and must keep its ports. A failed rule
	// delete is a warning, not a failed destroy: the record stays (as
	// "destroyed") with the rule marked, and the next destroy retries.
	rules := e.closeRules(ctx, stateFile, append([]string{active.ID}, pending...))
	if fipErr != nil {
		_ = state.Write(statePath, stateFile)
		return fipErr
	}

	// Clean up state. Retire moves records: active points into the slice.
	vmID, vmIP := active.VMID, active.VMIP
	for _, id := range append([]string{active.ID}, pending...) {
		sgrules.Retire(stateFile, id)
	}
	if err := state.Write(statePath, stateFile); err != nil {
		return err
	}

	if e.Printer.IsJSON() {
		out := map[string]any{"status": "destroyed", "vm_id": vmID, "vm_ip": vmIP}
		if !rules.Empty() {
			out["security_group_rules"] = rules
		}
		return e.Printer.JSON(out)
	}
	e.Printer.Success("Deployment destroyed successfully.")
	return nil
}

// closeRules deletes the port rules the given deployments created and
// reports it; see sgrules.Close.
func (e *Engine) closeRules(ctx context.Context, sf *state.File, ids []string) sgrules.CloseResult {
	var res sgrules.CloseResult
	for _, id := range ids {
		res.Add(sgrules.Close(ctx, e.Cloud, sf, id))
	}
	if len(res.Closed) > 0 {
		e.Printer.StepDone("Closed ports " + strings.Join(res.Closed, ",") + " in the security group")
	}
	for _, k := range res.Kept {
		e.Printer.StepDone("Kept port " + k)
	}
	if len(res.Failed) > 0 {
		e.Printer.Warning("Could not delete the security group rules for ports " + strings.Join(res.Failed, "; ") +
			". They stay in state: re-run 'evo destroy' to retry, or delete them in the Cloud.ru console.")
	}
	return res
}

// finishPendingRules is destroy with no active deployment, only rules left
// by an earlier destroy.
func (e *Engine) finishPendingRules(ctx context.Context, opts DestroyOptions, statePath string, sf *state.File, pending []string) error {
	proceed, err := confirm.Ask(ctx, pendingRulesPreview(sf, pending), e.confirmOptions(opts.Force, opts.DryRun, false))
	if err != nil {
		return err
	}
	if !proceed {
		return nil // --dry-run
	}
	tok, err := e.Auth.Token(ctx)
	if err != nil {
		return err
	}
	if ts, ok := e.Cloud.(interface{ SetToken(string) }); ok {
		ts.SetToken(tok.Value)
	}
	e.Printer.Step(1, 1, "Deleting port rules left by an earlier destroy...")
	rules := e.closeRules(ctx, sf, pending)
	for _, id := range pending {
		sgrules.Retire(sf, id)
	}
	if err := state.Write(statePath, sf); err != nil {
		return err
	}
	if e.Printer.IsJSON() {
		status := "destroyed"
		if len(rules.Failed) > 0 {
			status = "rules_pending"
		}
		return e.Printer.JSON(map[string]any{"status": status, "security_group_rules": rules})
	}
	if len(rules.Failed) == 0 {
		e.Printer.Success("Security group rules deleted.")
	}
	return nil
}

// fipReleaseAttempts bounds DeleteFloatingIP retries: right after the VM is
// deleted the platform may still report the FIP as attached.
const fipReleaseAttempts = 5

// releaseFloatingIP deletes the deployment's floating IP. The ID comes from
// state; records written before floating_ip_id existed fall back to looking
// the address up in list_floating_ips. Nothing to release is not an error.
func (e *Engine) releaseFloatingIP(ctx context.Context, rec *state.DeploymentRecord) error {
	projectID := e.projectOf(rec)
	fipID := rec.FloatingIPID
	// Without an ID: by address, else by the name deploy gives it (its
	// create response may have been lost, leaving neither in state).
	if fipID == "" && (rec.VMIP != "" || rec.VMID != "") {
		fips, err := e.Cloud.ListFloatingIPs(ctx, projectID)
		if err != nil {
			return clerrors.Wrap(clerrors.CodeAPIError,
				fmt.Sprintf("cannot look up floating IP %s", fipLabel(rec.VMIP, fipName(rec.VMID))),
				"The VM is deleted. Re-run 'evo destroy --force' to release the floating IP, or release it in the Cloud.ru console.",
				err)
		}
		for _, f := range fips {
			if (rec.VMIP != "" && f.IPAddress == rec.VMIP) || (rec.VMID != "" && f.Name == fipName(rec.VMID)) {
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
		"The floating IP is still billed. Re-run 'evo destroy --force' or release it in the Cloud.ru console.",
		lastErr)
}

// projectOf is the project a deployment lives in: the one recorded at
// deploy, else (records from older versions) the configured one.
func (e *Engine) projectOf(rec *state.DeploymentRecord) string {
	if rec.ProjectID != "" {
		return rec.ProjectID
	}
	return e.Config.ProjectID
}

func fipLabel(ip, id string) string {
	if ip == "" {
		return id
	}
	return fmt.Sprintf("%s (%s)", ip, id)
}

// isNotDeletableYet reports whether DeleteVM refused because the VM is in a
// transitional state (Cloud.ru answers 409 or 422 while it is being created
// or changes state).
func isNotDeletableYet(err error) bool {
	st := provision.HTTPStatus(err)
	return st == http.StatusConflict || st == http.StatusUnprocessableEntity
}

// isTransientServerError is a 502/503/504 from DELETE: the compute adapter
// repeats those only for GET, and while waiting for a deletable VM one of
// them should not end the wait.
func isTransientServerError(err error) bool {
	switch provision.HTTPStatus(err) {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// deleteVMWhenDeletable deletes the VM, waiting while Cloud.ru refuses
// because it is still being created: a VM stuck in "creating" (seen in
// cloud-e2e on 15.10.2026) is billed with its floating IP, and giving up at
// once left both behind. It retries until Timings.DeletableWaitTimeout, then
// returns the last refusal.
func (e *Engine) deleteVMWhenDeletable(ctx context.Context, vmID string) error {
	err := e.Cloud.DeleteVM(ctx, vmID)
	if err == nil || !isNotDeletableYet(err) {
		return err
	}
	t := e.timings()
	start := time.Now()
	deadline := start.Add(t.DeletableWaitTimeout)
	e.Printer.StepDone(fmt.Sprintf("VM %s cannot be deleted yet (still being created or changing state); waiting up to %s...",
		vmID, t.DeletableWaitTimeout))
	lastReport := start
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(t.DeletablePollInterval):
		}
		err = e.Cloud.DeleteVM(ctx, vmID)
		if err == nil || !(isNotDeletableYet(err) || isTransientServerError(err)) {
			return err
		}
		if time.Since(lastReport) >= time.Minute {
			lastReport = time.Now()
			status := "unknown (the status request failed)"
			if vm, gerr := e.Cloud.GetVM(ctx, vmID); gerr == nil && vm != nil {
				status = vm.Status
			}
			e.Printer.StepDone(fmt.Sprintf("still waiting: VM is %s (%s elapsed)", status, time.Since(start).Round(time.Second)))
		}
	}
	return err
}
