package deployplan

import (
	"context"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// adoptFIPCloud: the plan's VM exists with a floating IP already attached.
type adoptFIPCloud struct {
	existingCloud
	fipCreates int
}

func (c *adoptFIPCloud) GetVMDetail(context.Context, string) (*provision.VMDetail, error) {
	return &provision.VMDetail{ID: "vm-old", Interfaces: []provision.VMInterface{{ID: "if-1", Primary: true, Type: "regular",
		FloatingIP: &provision.IfaceFloatingIP{ID: "fip-have", IPAddress: "203.0.113.5", State: "in_use"}}}}, nil
}
func (c *adoptFIPCloud) CreateFloatingIP(context.Context, provision.CreateFloatingIPRequest) (*provision.FloatingIP, error) {
	c.fipCreates++
	return &provision.FloatingIP{ID: "fip-second", IPAddress: "203.0.113.6"}, nil
}

// R2: a retried deploy_apply uses the FIP already on the VM and returns its
// id for the state, instead of creating a second one.
func TestQAr3_Apply_AdoptUsesAttachedFIP(t *testing.T) {
	c := &adoptFIPCloud{}
	res, err := ApplyWithTimeout(context.Background(), c, &Plan{ProjectID: "p-1", VM: VMSpec{Name: "x"}, Network: NetworkSpec{FloatingIP: true}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if c.fipCreates != 0 || res.PublicIP != "203.0.113.5" || res.FloatingIPID != "fip-have" {
		t.Fatalf("FIP creates %d, result %+v; want the attached fip-have", c.fipCreates, res)
	}
}

// lostFIPCloud: the VM has no FIP; the create answer is lost, the FIP exists.
type lostFIPCloud struct {
	adoptFIPCloud
}

func (c *lostFIPCloud) GetVMDetail(context.Context, string) (*provision.VMDetail, error) {
	return &provision.VMDetail{ID: "vm-old", Interfaces: []provision.VMInterface{{ID: "if-1", Primary: true, Type: "regular"}}}, nil
}
func (c *lostFIPCloud) CreateFloatingIP(context.Context, provision.CreateFloatingIPRequest) (*provision.FloatingIP, error) {
	c.fipCreates++
	return nil, context.DeadlineExceeded
}
func (c *lostFIPCloud) ListFloatingIPs(context.Context, string) ([]*provision.FloatingIP, error) {
	return []*provision.FloatingIP{{ID: "fip-user", Name: "my-reserved", IPAddress: "198.51.100.1", State: "available"},
		{ID: "fip-made", Name: "x-fip", IPAddress: "203.0.113.7", State: "in_use"}}, nil
}

// R2/R9: the lost create is found by its name and recorded; a free FIP of
// the user is never taken.
func TestQAr3_Apply_LostFIPCreateFoundByName(t *testing.T) {
	c := &lostFIPCloud{}
	res, err := ApplyWithTimeout(context.Background(), c, &Plan{ProjectID: "p-1", VM: VMSpec{Name: "x"}, Network: NetworkSpec{FloatingIP: true}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.FloatingIPID != "fip-made" || res.PublicIP != "203.0.113.7" {
		t.Fatalf("result %+v; want fip-made found by name, never the user's fip-user", res)
	}
}

// R8: a VM with the plan's name in another zone is not adopted; a stopped
// one is reported as stopped, not running.
type zoneCloud struct {
	existingCloud
	vm *provision.VM
}

func (c *zoneCloud) ListVMsInProject(context.Context, string) ([]*provision.VM, error) {
	return []*provision.VM{c.vm}, nil
}
func (c *zoneCloud) GetVMDetail(context.Context, string) (*provision.VMDetail, error) {
	return &provision.VMDetail{}, nil
}

func TestQAr3_Apply_AdoptChecksZoneAndState(t *testing.T) {
	other := &zoneCloud{vm: &provision.VM{ID: "vm-old", Name: "x", Status: "running", Region: "zone-A"}}
	if res, err := ApplyWithTimeout(context.Background(), other, &Plan{ProjectID: "p-1", VM: VMSpec{Name: "x", Zone: RefSpec{ID: "zone-B"}}}, 0); err == nil {
		t.Fatalf("VM of another zone adopted: %+v", res)
	}
	stopped := &zoneCloud{vm: &provision.VM{ID: "vm-old", Name: "x", Status: "stopped", Region: "zone-B"}}
	res, err := ApplyWithTimeout(context.Background(), stopped, &Plan{ProjectID: "p-1", VM: VMSpec{Name: "x", Zone: RefSpec{ID: "zone-B"}}}, 0)
	if err != nil || res.Status != "stopped" {
		t.Fatalf("stopped VM: %+v, %v; want status stopped", res, err)
	}
}
