package engine

import (
	"context"
	"fmt"

	"github.com/shatim-ops/cloudru-vm-cli/internal/state"
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
		e.Printer.Success("No active deployment.")
		e.Printer.Hint("Run 'cloudru-vm deploy' to create one.")
		return nil
	}

	if e.Printer.IsJSON() {
		return e.Printer.JSON(active)
	}

	headers := []string{"Property", "Value"}
	rows := [][]string{
		{"ID", active.ID},
		{"VM ID", active.VMID},
		{"VM IP", active.VMIP},
		{"Flavor", active.VMFlavor},
		{"Region", active.Region},
		{"Status", active.Status},
		{"Compose Hash", active.ComposeHash},
		{"Created", active.CreatedAt.Format("2006-01-02 15:04:05")},
		{"Updated", active.UpdatedAt.Format("2006-01-02 15:04:05")},
	}

	if len(active.ExposedPorts) > 0 {
		ports := fmt.Sprintf("%v", active.ExposedPorts)
		rows = append(rows, []string{"Exposed Ports", ports})
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
		e.Printer.Success("No deployments found.")
		return nil
	}

	if e.Printer.IsJSON() {
		return e.Printer.JSON(stateFile.Deployments)
	}

	headers := []string{"ID", "VM IP", "Status", "Flavor", "Created"}
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
