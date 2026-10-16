package engine

import (
	"context"
	stderrors "errors"
	"fmt"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// StatusOptions holds parameters for the status command.
type StatusOptions struct {
	NoRefresh bool // read the local state only, no request to the cloud
}

// CloudCheck is what one GET /vms/{id} said about a deployment's VM. status
// shows it next to the local record, which alone kept a VM deleted in the
// console "running" (ревью r5 U12). It never changes the state file.
type CloudCheck struct {
	// Checked: the cloud answered — with the VM (State) or 404 (NotFound).
	Checked bool `json:"cloud_checked"`
	// State is the VM's live state ("running", "stopped", ...).
	State string `json:"cloud_state,omitempty"`
	// NotFound: Cloud.ru answered 404 — the VM was deleted outside evo;
	// 'evo destroy' cleans up the record.
	NotFound bool `json:"not_found_in_cloud,omitempty"`
	// Error says why the cloud was not checked (no key, network, 5xx),
	// as a phrase without the error code; empty when Checked or when the
	// check was skipped.
	Error string `json:"cloud_error,omitempty"`
	// Reason is the error code of Error (API_UNREACHABLE, AUTH_REQUIRED,
	// …), as reason of a tool error (ревью r5 M5).
	Reason string `json:"cloud_reason,omitempty"`
}

// DeploymentStatus is the status answer: the local record plus what the
// cloud said about its VM.
type DeploymentStatus struct {
	state.DeploymentRecord
	CloudCheck
}

// CheckCloud asks Cloud.ru for the record's VM: one GET /vms/{id}, at most
// Timings.StatusRefreshTimeout including authentication. A failure is reported in
// the result, never returned: the local state is still worth showing.
// Read-only — the state file is not touched.
func (e *Engine) CheckCloud(ctx context.Context, rec *state.DeploymentRecord) CloudCheck {
	notChecked := func(err error) CloudCheck {
		c := CloudCheck{Error: err.Error(), Reason: clerrors.CodeAPIError}
		var ce *clerrors.CLIError
		if stderrors.As(err, &ce) {
			if ce.Message != "" {
				c.Error = ce.Message
			}
			if clerrors.IsCode(ce.Code) {
				c.Reason = ce.Code
			}
		}
		return c
	}
	if rec.VMID == "" {
		return notChecked(fmt.Errorf("%s", i18n.T("no VM ID in state", "в state нет ID ВМ")))
	}
	if e.Auth == nil || e.Cloud == nil {
		return notChecked(fmt.Errorf("%s", i18n.T("no Cloud.ru client", "нет клиента Cloud.ru")))
	}
	timeout := e.timings().StatusRefreshTimeout
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	timedOut := func(err error) error {
		if stderrors.Is(cctx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
			return fmt.Errorf(i18n.T("no answer within %s", "нет ответа за %s"), timeout)
		}
		return err
	}

	tok, err := e.Auth.Token(cctx)
	if err != nil {
		return notChecked(timedOut(err))
	}
	e.Cloud.SetToken(tok.Value)
	vm, err := e.Cloud.GetVM(cctx, rec.VMID)
	if err != nil {
		var ce *clerrors.CLIError
		if stderrors.As(err, &ce) && ce.Code == clerrors.CodeVMNotFound {
			return CloudCheck{Checked: true, NotFound: true}
		}
		return notChecked(timedOut(err))
	}
	if vm == nil {
		return notChecked(fmt.Errorf("%s", i18n.T("empty answer", "пустой ответ")))
	}
	return CloudCheck{Checked: true, State: vm.Status}
}

// Status displays the current deployment status: the local record and,
// unless opts.NoRefresh, the VM's live state from one bounded GET.
func (e *Engine) Status(ctx context.Context, opts StatusOptions) error {
	statePath := state.DefaultPath(e.StateDir)
	stateFile, err := state.Read(statePath)
	if err != nil {
		return err
	}

	active := stateFile.ActiveDeployment()
	if active == nil {
		if e.Printer.IsJSON() {
			// The same answer as the MCP status tool: --json must print JSON.
			return e.Printer.JSON(map[string]any{"status": "none", "message": i18n.T("No active deployment.", "Активного развёртывания нет.")})
		}
		e.Printer.Success(i18n.T("No active deployment.", "Активного развёртывания нет."))
		e.Printer.Hint(i18n.T("Run 'evo deploy' to create one.", "Создать: 'evo deploy'."))
		return nil
	}

	var check CloudCheck
	if !opts.NoRefresh {
		check = e.CheckCloud(ctx, active)
	}

	if e.Printer.IsJSON() {
		return e.Printer.JSON(DeploymentStatus{DeploymentRecord: *active, CloudCheck: check})
	}

	headers := []string{i18n.T("Property", "Параметр"), i18n.T("Value", "Значение")}
	rows := [][]string{
		{"ID", active.ID},
		{i18n.T("VM ID", "ID ВМ"), active.VMID},
		{i18n.T("VM IP", "IP ВМ"), active.VMIP},
		{"Flavor", active.VMFlavor},
		{i18n.T("Region", "Зона"), active.Region},
		{i18n.T("Status", "Состояние"), active.Status},
	}
	if !opts.NoRefresh {
		rows = append(rows, []string{i18n.T("In the cloud", "В облаке"), cloudCell(check)})
	}
	rows = append(rows,
		[]string{i18n.T("Compose Hash", "Хеш compose"), active.ComposeHash},
		[]string{i18n.T("Created", "Создано"), active.CreatedAt.Format("2006-01-02 15:04:05")},
		[]string{i18n.T("Updated", "Обновлено"), active.UpdatedAt.Format("2006-01-02 15:04:05")},
	)

	if len(active.ExposedPorts) > 0 {
		ports := fmt.Sprintf("%v", active.ExposedPorts)
		rows = append(rows, []string{i18n.T("Exposed Ports", "Открытые порты"), ports})
	}

	e.Printer.Table(headers, rows)
	if check.NotFound {
		e.Printer.Hint(i18n.T("The record is stale: the VM was deleted outside evo. 'evo destroy' removes it and releases its floating IP and port rules.",
			"Запись устарела: ВМ удалили вне evo. 'evo destroy' уберёт запись и освободит её floating IP и правила портов."))
	}
	return nil
}

// cloudCell is the "In the cloud" row of status.
func cloudCell(c CloudCheck) string {
	switch {
	case c.NotFound:
		return i18n.T("not found (deleted outside evo) → evo destroy", "не найдена (удалена вне evo) → evo destroy")
	case c.Checked && c.State == "":
		return i18n.T("found, state unknown", "есть, состояние неизвестно")
	case c.Checked:
		return c.State
	default:
		return i18n.T("not checked: ", "не проверено: ") + c.Error
	}
}

// List displays all deployments.
func (e *Engine) List(ctx context.Context) error {
	statePath := state.DefaultPath(e.StateDir)
	stateFile, err := state.Read(statePath)
	if err != nil {
		return err
	}

	if len(stateFile.Deployments) == 0 {
		e.Printer.Success(i18n.T("No deployments found.", "Развёртываний нет."))
		return nil
	}

	if e.Printer.IsJSON() {
		return e.Printer.JSON(stateFile.Deployments)
	}

	headers := []string{"ID", i18n.T("VM IP", "IP ВМ"), i18n.T("Status", "Состояние"), "Flavor", i18n.T("Created", "Создано")}
	var rows [][]string
	for _, d := range stateFile.Deployments {
		rows = append(rows, []string{
			d.ID,
			d.VMIP,
			d.Status,
			d.VMFlavor,
			d.CreatedAt.Format("2006-01-02 15:04"),
		})
	}

	e.Printer.Table(headers, rows)
	return nil
}
