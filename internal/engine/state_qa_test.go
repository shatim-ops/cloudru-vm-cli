package engine

import (
	"context"
	stderrors "errors"
	"testing"

	clerrors "github.com/shatim-ops/cloudru-vm-cli/internal/errors"
	"github.com/shatim-ops/cloudru-vm-cli/internal/state"
)

func errCode(err error) string {
	var ce *clerrors.CLIError
	if stderrors.As(err, &ce) {
		return ce.Code
	}
	return ""
}

// Two deploys in one directory both saw "no deployment" and created two
// VMs; the second state write dropped the first VM's record.
func TestDeploy_RefusedWhileAnotherHoldsTheState(t *testing.T) {
	cloud := &mockCloudAPI{}
	eng, dir := newTestEngine(t, cloud, newMockDeployer())
	unlock, err := state.Lock(state.DefaultPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()

	err = eng.Deploy(context.Background(), newDeployOpts(t, dir))
	if errCode(err) != clerrors.CodeStateLocked {
		t.Fatalf("Deploy = %v, want %s", err, clerrors.CodeStateLocked)
	}
	if cloud.createCalled {
		t.Fatal("a VM was created while another deploy held the state")
	}

	// --dry-run changes nothing and is not blocked.
	opts := newDeployOpts(t, dir)
	opts.DryRun = true
	if err := eng.Deploy(context.Background(), opts); err != nil {
		t.Fatalf("dry run while locked: %v", err)
	}
}

func TestDestroy_RefusedWhileAnotherHoldsTheState(t *testing.T) {
	deleted := false
	cloud := &mockCloudAPI{deleteVMFn: func(context.Context, string) error { deleted = true; return nil }}
	eng, _, _ := destroyFixture(t, runningRecord())
	eng.Cloud = cloud
	unlock, err := state.Lock(state.DefaultPath(eng.StateDir))
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err := eng.Destroy(context.Background(), DestroyOptions{Force: true}); errCode(err) != clerrors.CodeStateLocked {
		t.Fatalf("Destroy = %v, want %s", err, clerrors.CodeStateLocked)
	}
	if deleted {
		t.Fatal("VM deleted while another command held the state")
	}
}
