package engine

import (
	"bytes"
	"context"
	stderrors "errors"
	"strings"
	"sync"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// fipCalls records CreateFloatingIP requests.
type fipCalls struct {
	mu   sync.Mutex
	reqs []provision.CreateFloatingIPRequest
}

func (f *fipCalls) record(req provision.CreateFloatingIPRequest) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, req)
	return len(f.reqs)
}

// The public IP is reserved before the VM and attached in the create
// request, so the VM boots with internet (cloud-init sets up the image's
// apt mirror at first boot); no second FIP is made after boot.
func TestDeploy_FIPAttachedAtCreation(t *testing.T) {
	var calls fipCalls
	cloud := &mockCloudAPI{createFloatingIPFn: func(_ context.Context, req provision.CreateFloatingIPRequest) (*provision.FloatingIP, error) {
		calls.record(req)
		return &provision.FloatingIP{ID: "fip-boot", IPAddress: "203.0.113.7", State: "available"}, nil
	}}
	eng, dir := newTestEngine(t, cloud, newMockDeployer())

	if err := eng.Deploy(context.Background(), newDeployOpts(t, dir)); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if len(calls.reqs) != 1 {
		t.Fatalf("CreateFloatingIP called %d times, want 1 (before the VM)", len(calls.reqs))
	}
	if calls.reqs[0].InterfaceID != "" {
		t.Errorf("boot FIP created with interface %q, want a free one", calls.reqs[0].InterfaceID)
	}
	if got := cloud.createReq.Interfaces[0].AttachExternalIPID; got != "fip-boot" {
		t.Errorf("create request attaches %q, want fip-boot", got)
	}
	a := readActive(t, dir)
	if a == nil || a.FloatingIPID != "fip-boot" || a.VMIP == "" {
		t.Fatalf("state: %+v, want FIP fip-boot with an IP", a)
	}
}

// A VM that was not created leaves no billed FIP behind.
func TestDeploy_CreateVMFails_ReleasesBootFIP(t *testing.T) {
	cloud := &mockCloudAPI{createVMFn: func(context.Context, provision.CreateVMRequest) (*provision.VM, error) {
		return nil, stderrors.New("422 quota")
	}}
	eng, dir := newTestEngine(t, cloud, newMockDeployer())

	if err := eng.Deploy(context.Background(), newDeployOpts(t, dir)); err == nil {
		t.Fatal("deploy succeeded, want the CreateVM error")
	}
	if len(cloud.deletedFIPs) != 1 || cloud.deletedFIPs[0] != "fip-1" {
		t.Errorf("released FIPs %v, want [fip-1]", cloud.deletedFIPs)
	}
}

// When the IP cannot be reserved up front, deploy goes on and attaches one
// after boot, as before, and says the first boot may be slow.
func TestDeploy_BootFIPFails_AttachesAfterBoot(t *testing.T) {
	var calls fipCalls
	cloud := &mockCloudAPI{createFloatingIPFn: func(_ context.Context, req provision.CreateFloatingIPRequest) (*provision.FloatingIP, error) {
		if calls.record(req) == 1 {
			return nil, stderrors.New("403 quota exceeded")
		}
		return &provision.FloatingIP{ID: "fip-late", IPAddress: "203.0.113.8", State: "in_use"}, nil
	}}
	var out bytes.Buffer
	eng, dir := newTestEngine(t, cloud, newMockDeployer())
	eng.Printer = output.NewWithWriter(&out, output.ModePlain, false)

	if err := eng.Deploy(context.Background(), newDeployOpts(t, dir)); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if got := cloud.createReq.Interfaces[0].AttachExternalIPID; got != "" {
		t.Errorf("create request attaches %q, want none", got)
	}
	if len(calls.reqs) != 2 || calls.reqs[1].InterfaceID == "" {
		t.Fatalf("FIP calls %+v, want a failed reservation then one on the interface", calls.reqs)
	}
	if !strings.Contains(out.String(), "attached after boot") {
		t.Errorf("no warning about the late IP:\n%s", out.String())
	}
	if a := readActive(t, dir); a == nil || a.FloatingIPID != "fip-late" {
		t.Errorf("state: %+v, want FIP fip-late", a)
	}
}
