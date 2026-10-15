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

func (applyCloud) ListVMsInProject(context.Context, string) ([]*provision.VM, error) {
	return nil, nil
}
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

// lostAnswerCloud creates the VM but loses the answer: the VM is listed only
// after the create call.
type lostAnswerCloud struct {
	provision.CloudAPI
	created bool
}

func (c *lostAnswerCloud) CreateVM(context.Context, provision.CreateVMRequest) (*provision.VM, error) {
	c.created = true
	return nil, errors.New("context deadline exceeded")
}
func (c *lostAnswerCloud) ListVMsInProject(context.Context, string) ([]*provision.VM, error) {
	if !c.created {
		return nil, nil
	}
	return []*provision.VM{{ID: "vm-lost", Name: "x", CreatedAt: time.Now()}}, nil
}

// The create call fails but the VM exists: the result names it, so
// deploy_apply records it and a retry adopts it.
func TestApply_CreateFailsButVMExists_NamesTheVM(t *testing.T) {
	res, err := Apply(context.Background(), &lostAnswerCloud{}, &Plan{ProjectID: "p", VM: VMSpec{Name: "x"}})
	if err == nil || !strings.Contains(err.Error(), "vm-lost") {
		t.Fatalf("Apply = %v, want an error naming vm-lost", err)
	}
	if res == nil || res.VMID != "vm-lost" {
		t.Fatalf("result %+v, want VMID vm-lost", res)
	}
}

// existingCloud already has the plan's VM in the project, or fails to list.
type existingCloud struct {
	applyCloud
	listErr error
	creates int
	project string
}

func (c *existingCloud) ListVMsInProject(_ context.Context, project string) ([]*provision.VM, error) {
	c.project = project
	if c.listErr != nil {
		return nil, c.listErr
	}
	return []*provision.VM{{ID: "vm-old", Name: "x", Status: "running"}}, nil
}
func (c *existingCloud) CreateVM(context.Context, provision.CreateVMRequest) (*provision.VM, error) {
	c.creates++
	return &provision.VM{ID: "vm-second"}, nil
}

// A retried deploy_apply adopts the VM of the plan's project instead of
// creating a second one (QA H5).
func TestApply_AdoptsExistingVMInProject(t *testing.T) {
	c := &existingCloud{}
	res, err := Apply(context.Background(), c, &Plan{ProjectID: "p-1", VM: VMSpec{Name: "x"}})
	if c.creates != 0 {
		t.Fatalf("CreateVM called %d times, want 0 (the VM exists)", c.creates)
	}
	if c.project != "p-1" {
		t.Errorf("listed project %q, want the plan's p-1", c.project)
	}
	if err == nil && (res == nil || res.VMID != "vm-old") {
		t.Errorf("result %+v, want VMID vm-old", res)
	}
}

// The lookup fails: nothing is created, so a retry cannot leave two VMs.
func TestApply_LookupFails_CreatesNothing(t *testing.T) {
	c := &existingCloud{listErr: errors.New("502 bad gateway")}
	_, err := Apply(context.Background(), c, &Plan{ProjectID: "p-1", VM: VMSpec{Name: "x"}})
	if err == nil || !strings.Contains(err.Error(), "nothing was created") {
		t.Fatalf("Apply = %v, want a lookup error", err)
	}
	if c.creates != 0 {
		t.Errorf("CreateVM called %d times after a failed lookup, want 0", c.creates)
	}
}
