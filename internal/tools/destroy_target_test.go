package tools

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// targetCloud has one VM whose list entry carries no floating IP (as the
// API's VM list does); its FIP is attached until the VM is gone.
type targetCloud struct {
	provision.CloudAPI
	deleteErr   error
	vmGone      bool
	fipTries    int
	releasedFIP string
}

func (*targetCloud) SetToken(string) {}
func (*targetCloud) ListVMsInProject(context.Context, string) ([]*provision.VM, error) {
	return []*provision.VM{{ID: "vm-1", Name: "web"}}, nil
}
func (c *targetCloud) DeleteVMInProject(context.Context, string, string) error {
	if c.deleteErr != nil {
		return c.deleteErr
	}
	c.vmGone = true
	return nil
}
func (c *targetCloud) GetVM(context.Context, string) (*provision.VM, error) {
	if c.vmGone {
		return nil, clerrors.New(clerrors.CodeVMNotFound, "gone", "")
	}
	return &provision.VM{ID: "vm-1", Status: "creating"}, nil
}
func (c *targetCloud) DeleteFloatingIP(_ context.Context, id, _ string) error {
	c.fipTries++
	if c.fipTries < 2 {
		return clerrors.New(clerrors.CodeAPIError, "floating ip is attached", "")
	}
	c.releasedFIP = id
	return nil
}
func (*targetCloud) ListSecurityGroupRules(context.Context, string) ([]*provision.SGRule, error) {
	return nil, nil
}

func targetRegistry(t *testing.T, cloud provision.CloudAPI) (*Registry, string) {
	t.Helper()
	dir := t.TempDir()
	sf := &state.File{Version: 1}
	sf.UpsertDeployment(state.DeploymentRecord{ID: "vm-1", VMID: "vm-1", VMIP: "203.0.113.5", FloatingIPID: "fip-1", Status: "running"})
	if err := state.Write(state.DefaultPath(dir), sf); err != nil {
		t.Fatal(err)
	}
	r := New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config:   config.Config{ProjectID: "p", KeyID: "k", Secret: "s"},
			Auth:     staticToken{},
			Cloud:    cloud,
			Printer:  output.NewWithWriter(&buf, output.ModeText, false),
			StateDir: dir,
			Timings:  engine.Timings{FIPRetryInterval: time.Millisecond, DestroyPollInterval: time.Millisecond, DestroyPollTimeout: time.Second},
		}, nil
	})
	return r, dir
}

// destroy vm_id=… releases the VM's FIP named in the state, retrying while
// it is still attached; it used to read the list's empty flat field and
// release nothing (QA H4).
func TestDestroyTargeted_ReleasesFIPFromState(t *testing.T) {
	cloud := &targetCloud{}
	r, _ := targetRegistry(t, cloud)
	text, isErr := r.handlers["destroy"](context.Background(), map[string]any{"vm_id": "vm-1", "confirmed": true})
	if isErr {
		t.Fatalf("destroy: %s", text)
	}
	if cloud.releasedFIP != "fip-1" || cloud.fipTries < 2 {
		t.Fatalf("released %q after %d tries, want fip-1 after a retry", cloud.releasedFIP, cloud.fipTries)
	}
	if !strings.Contains(text, "released floating IP 203.0.113.5") {
		t.Errorf("answer does not name the released FIP: %s", text)
	}
}

// A VM still being created: a clear "come back later", nothing deleted.
func TestDestroyTargeted_NotDeletableYet(t *testing.T) {
	cloud := &targetCloud{deleteErr: clerrors.Wrap(clerrors.CodeAPIError, "API returned status 409", "",
		&provision.StatusError{Status: 409, Body: "creating"})}
	r, _ := targetRegistry(t, cloud)
	text, isErr := r.handlers["destroy"](context.Background(), map[string]any{"vm_id": "vm-1", "confirmed": true})
	if !isErr || !strings.Contains(text, "not_deletable_yet") || !strings.Contains(text, "again") {
		t.Fatalf("answer = %s", text)
	}
	if cloud.fipTries != 0 {
		t.Errorf("FIP touched while the VM still exists")
	}
}
