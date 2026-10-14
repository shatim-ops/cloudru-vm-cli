package engine

import (
	"bytes"
	"context"
	stderrors "errors"
	"strings"
	"testing"

	"github.com/shatim-ops/cloudru-vm-cli/internal/compose"
	"github.com/shatim-ops/cloudru-vm-cli/internal/config"
	clerrors "github.com/shatim-ops/cloudru-vm-cli/internal/errors"
	"github.com/shatim-ops/cloudru-vm-cli/internal/output"
	"github.com/shatim-ops/cloudru-vm-cli/internal/provision"
	"github.com/shatim-ops/cloudru-vm-cli/internal/state"
)

// interruptCloud creates a VM and records deletions. Any other CloudAPI call
// hits the nil embedded interface and panics.
type interruptCloud struct {
	provision.CloudAPI
	deleted []string
}

func (c *interruptCloud) GetImage(context.Context, string) (*provision.ImageDetail, error) {
	return &provision.ImageDetail{}, nil
}

func (c *interruptCloud) CreateVM(context.Context, provision.CreateVMRequest) (*provision.VM, error) {
	return &provision.VM{ID: "vm-new", Status: "creating"}, nil
}

func (c *interruptCloud) DeleteVM(_ context.Context, id string) error {
	c.deleted = append(c.deleted, id)
	return nil
}

// provisionInterrupted runs provisionVM with a context that is already
// cancelled, as after Ctrl-C in the CLI. The cancelled context is the only
// ready case of the poll select (the ticker is seconds away), so the path
// is deterministic and needs no sleeps.
func provisionInterrupted(t *testing.T, nonInteractive bool) (*interruptCloud, *Engine, *bytes.Buffer, error) {
	t.Helper()
	dir := t.TempDir()
	cloud := &interruptCloud{}
	var stdout, stderr bytes.Buffer
	e := &Engine{
		Config:   config.Config{ProjectID: "p", ImageID: "img"},
		Cloud:    cloud,
		Printer:  output.NewWithWriters(&stdout, &stderr, output.ModePlain, false),
		StateDir: dir,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	statePath := state.DefaultPath(dir)
	sf := &state.File{Version: 1}
	_, err := e.provisionVM(ctx, DeployOptions{NonInteractive: nonInteractive}, sf, statePath,
		&compose.Project{Hash: "h"}, "flavor", "az")
	return cloud, e, &stderr, err
}

func assertVMRecorded(t *testing.T, e *Engine) {
	t.Helper()
	sf, err := state.Read(state.DefaultPath(e.StateDir))
	if err != nil {
		t.Fatal(err)
	}
	if a := sf.ActiveDeployment(); a == nil || a.VMID != "vm-new" {
		t.Fatalf("VM not recorded in state: %+v", a)
	}
}

func TestProvisionVM_CancelledNoTTY_LeavesVMAndSaysSo(t *testing.T) {
	withPromptStdin(t, strings.NewReader("y\n"), false)

	cloud, e, stderr, err := provisionInterrupted(t, false)

	if got := clerrors.ExitCode(err); got != clerrors.ExitInterrupted {
		t.Fatalf("exit code = %d (err %v), want %d", got, err, clerrors.ExitInterrupted)
	}
	if len(cloud.deleted) != 0 {
		t.Errorf("VM deleted without a question: %v", cloud.deleted)
	}
	if strings.Contains(stderr.String(), "Delete the VM") {
		t.Errorf("asked without a terminal: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "VM vm-new left running") {
		t.Errorf("no warning that the VM is still running: %q", stderr.String())
	}
	var cliErr *clerrors.CLIError
	if !stderrors.As(err, &cliErr) || !strings.Contains(cliErr.Hint, "vm-new") || !strings.Contains(cliErr.Hint, "cloudru-vm destroy") {
		t.Errorf("hint must name the VM and how to delete it: %v", err)
	}
	assertVMRecorded(t, e)
}

func TestProvisionVM_CancelledNonInteractiveFlag_DoesNotAsk(t *testing.T) {
	withPromptStdin(t, strings.NewReader("y\n"), true)

	cloud, e, stderr, err := provisionInterrupted(t, true)

	if got := clerrors.ExitCode(err); got != clerrors.ExitInterrupted {
		t.Fatalf("exit code = %d (err %v), want %d", got, err, clerrors.ExitInterrupted)
	}
	if len(cloud.deleted) != 0 || strings.Contains(stderr.String(), "Delete the VM") {
		t.Errorf("--non-interactive must not ask or delete: deleted=%v stderr=%q", cloud.deleted, stderr.String())
	}
	assertVMRecorded(t, e)
}

func TestProvisionVM_CancelledInteractive_AsksAndDeletesOnYes(t *testing.T) {
	withPromptStdin(t, strings.NewReader("y\n"), true)

	cloud, _, stderr, err := provisionInterrupted(t, false)

	if !strings.Contains(stderr.String(), "Delete the VM vm-new? [y/N]") {
		t.Errorf("question not asked: %q", stderr.String())
	}
	if len(cloud.deleted) != 1 || cloud.deleted[0] != "vm-new" {
		t.Errorf("deleted = %v, want [vm-new]", cloud.deleted)
	}
	if got := clerrors.ExitCode(err); got != clerrors.ExitInterrupted {
		t.Errorf("exit code = %d (err %v), want %d", got, err, clerrors.ExitInterrupted)
	}
}

func TestProvisionVM_CancelledInteractive_KeepsVMOnNo(t *testing.T) {
	withPromptStdin(t, strings.NewReader("\n"), true)

	cloud, e, stderr, _ := provisionInterrupted(t, false)

	if !strings.Contains(stderr.String(), "Delete the VM vm-new?") {
		t.Errorf("question not asked: %q", stderr.String())
	}
	if len(cloud.deleted) != 0 {
		t.Errorf("VM deleted on 'no': %v", cloud.deleted)
	}
	assertVMRecorded(t, e)
}
