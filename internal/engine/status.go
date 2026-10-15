package engine

import (
	"context"
	"fmt"

	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// Status displays the current deployment status.
func (e *Engine) Status(ctx context.Context) error {
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

	if e.Printer.IsJSON() {
		return e.Printer.JSON(active)
	}

	headers := []string{i18n.T("Property", "Параметр"), i18n.T("Value", "Значение")}
	rows := [][]string{
		{"ID", active.ID},
		{i18n.T("VM ID", "ID ВМ"), active.VMID},
		{i18n.T("VM IP", "IP ВМ"), active.VMIP},
		{"Flavor", active.VMFlavor},
		{i18n.T("Region", "Зона"), active.Region},
		{i18n.T("Status", "Состояние"), active.Status},
		{i18n.T("Compose Hash", "Хеш compose"), active.ComposeHash},
		{i18n.T("Created", "Создано"), active.CreatedAt.Format("2006-01-02 15:04:05")},
		{i18n.T("Updated", "Обновлено"), active.UpdatedAt.Format("2006-01-02 15:04:05")},
	}

	if len(active.ExposedPorts) > 0 {
		ports := fmt.Sprintf("%v", active.ExposedPorts)
		rows = append(rows, []string{i18n.T("Exposed Ports", "Открытые порты"), ports})
	}

	e.Printer.Table(headers, rows)
	return nil
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
