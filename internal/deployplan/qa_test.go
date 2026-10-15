package deployplan

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// plan_id comes from the MCP client (over HTTP: from the network) and was
// joined into a path as is: "../state" read .evo/state.json as a
// plan, and Delete after apply removed it.
func TestPlanIDCannotLeaveThePlansDir(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, ".evo", "state.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, []byte(`{"version":1,"deployments":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"../state", "../../x", "/etc/passwd", `..\state`, "plan-../../state", ""} {
		if _, err := Load(dir, id); err == nil {
			t.Errorf("Load(%q) succeeded", id)
		}
		Delete(dir, id)
	}
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("state.json removed through a plan id: %v", err)
	}

	p := &Plan{PlanID: newPlanID(), ExpiresAt: time.Now().Add(time.Minute)}
	if err := Save(dir, p); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir, p.PlanID); err != nil {
		t.Fatalf("a real plan id: %v", err)
	}
}

type applyCloud struct {
	provision.CloudAPI
}

func (applyCloud) ListVMs(context.Context) ([]*provision.VM, error) { return nil, nil }
func (applyCloud) CreateVM(context.Context, provision.CreateVMRequest) (*provision.VM, error) {
	return &provision.VM{ID: "vm-made"}, nil
}

// Cancelled while waiting for the new VM: the error must name it, or the
// caller (deploy_apply) records nothing and the VM is billed unseen.
func TestApply_CancelledAfterCreate_NamesTheVM(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Apply(ctx, applyCloud{}, &Plan{VM: VMSpec{Name: "x"}})
	if err == nil || !strings.Contains(err.Error(), "vm-made") {
		t.Fatalf("Apply = %v, want an error naming vm-made", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error does not wrap context.Canceled: %v", err)
	}
}

// The API leaves availability_zone_id empty on subnets (BACKLOG [05]):
// match by the zone name in the subnet name, as engine.resolveNetwork does,
// and never take another zone's subnet when several exist.
func TestPickSubnet_ByZoneName(t *testing.T) {
	subnets := []*provision.Subnet{{ID: "sn-1", Name: "Default_ru.AZ-1"}, {ID: "sn-2", Name: "Default_ru.AZ-2"}}
	if got := pickSubnet(subnets, "z2", "ru.AZ-2"); got != "sn-2" {
		t.Errorf("pickSubnet = %q, want sn-2", got)
	}
	if got := pickSubnet(subnets, "z3", "ru.AZ-3"); got != "" {
		t.Errorf("pickSubnet for a zone without a subnet = %q, want none", got)
	}
	if got := pickSubnet(subnets[:1], "z3", "ru.AZ-3"); got != "sn-1" {
		t.Errorf("single subnet = %q, want sn-1", got)
	}
}
