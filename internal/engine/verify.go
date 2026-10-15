package engine

import (
	"context"
	"fmt"
	"time"

	clerrors "github.com/shatim-ops/cloudru-vm-cli/internal/errors"
	"github.com/shatim-ops/cloudru-vm-cli/internal/state"
	"github.com/shatim-ops/cloudru-vm-cli/internal/verify"
)

// Verify probes the exposed ports of the active deployment and prints the
// results. An unhealthy service is an error (exit 1): scripts and CI must be
// able to tell a broken deployment from a working one.
func (e *Engine) Verify(ctx context.Context) error {
	stateFile, err := state.Read(state.DefaultPath(e.StateDir))
	if err != nil {
		return err
	}

	active := stateFile.ActiveDeployment()
	if active == nil {
		if e.Printer.IsJSON() {
			return e.Printer.JSON([]verify.ProbeResult{}) // nothing checked
		}
		e.Printer.Success("No active deployment to verify.")
		e.Printer.Hint("Run 'evo deploy' first.")
		return nil
	}
	if len(active.ExposedPorts) == 0 {
		if e.Printer.IsJSON() {
			return e.Printer.JSON([]verify.ProbeResult{})
		}
		e.Printer.Success("No exposed ports to verify.")
		return nil
	}

	probe := e.Probe
	if probe == nil {
		probe = verify.Check
	}
	results := probe(ctx, active.VMIP, active.ExposedPorts, e.timings().VerifyTimeout)

	unhealthy := 0
	for _, r := range results {
		if !r.Healthy {
			unhealthy++
		}
	}

	if e.Printer.IsJSON() {
		if err := e.Printer.JSON(results); err != nil {
			return err
		}
	} else {
		headers := []string{"Port", "Status", "Healthy", "Latency"}
		var rows [][]string
		for _, r := range results {
			healthy := "yes"
			status := fmt.Sprintf("%d", r.StatusCode)
			if !r.Healthy {
				healthy = "no"
				if r.Error != "" {
					status = r.Error
				}
			}
			rows = append(rows, []string{
				fmt.Sprintf("%d", r.Port),
				status,
				healthy,
				r.Latency.Round(time.Millisecond).String(),
			})
		}
		e.Printer.Table(headers, rows)
	}

	if unhealthy > 0 {
		return clerrors.New(clerrors.CodeUnhealthy,
			fmt.Sprintf("%d of %d health check(s) failed on %s", unhealthy, len(results), active.VMIP),
			"Check the service logs with 'evo logs'.")
	}
	return nil
}
