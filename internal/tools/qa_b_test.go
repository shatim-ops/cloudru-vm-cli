package tools

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/deployplan"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// Релизный QA, B-lifecycle: учёт floating IP у deploy_apply и destroy.

func qaBRegistry(t *testing.T, cloud provision.CloudAPI, dir string) *Registry {
	t.Helper()
	return New(func(map[string]any) (*engine.Engine, error) {
		return &engine.Engine{
			Config: config.Config{ProjectID: "p", Region: "z", ImageID: "img-1", KeyID: "k", Secret: "s"},
			Auth:   staticToken{}, Cloud: cloud,
			Printer:  output.NewWithWriter(&bytes.Buffer{}, output.ModeText, false),
			StateDir: dir, SSHKeyPEM: []byte("key"), SSHPubKey: "ssh-ed25519 AAAA test",
			Timings: engine.Timings{FIPRetryInterval: time.Millisecond, DestroyPollInterval: time.Millisecond, DestroyPollTimeout: time.Second},
		}, nil
	})
}

// №2: повтор deploy_apply, не прочитавший интерфейс ВМ, не затирает
// floating IP старой записи — иначе destroy его не нашёл бы.
func TestQAB_RecordDeployment_KeepsFloatingIPOfOldRecord(t *testing.T) {
	dir := t.TempDir()
	sf := &state.File{Version: 1}
	sf.UpsertDeployment(state.DeploymentRecord{ID: "vm-1", VMID: "vm-1", VMIP: "203.0.113.5", FloatingIPID: "fip-1", Status: "creating"})
	if err := state.Write(state.DefaultPath(dir), sf); err != nil {
		t.Fatal(err)
	}
	res := &deployplan.ApplyResult{VMID: "vm-1", Status: "creating"}
	if err := recordDeployment(dir, &deployplan.Plan{ProjectID: "p", VM: deployplan.VMSpec{Name: "x"}}, res, res.Status); err != nil {
		t.Fatal(err)
	}
	got, _ := state.Read(state.DefaultPath(dir))
	if rec := got.FindDeployment("vm-1"); rec.FloatingIPID != "fip-1" || rec.VMIP != "203.0.113.5" {
		t.Fatalf("floating_ip_id=%q vm_ip=%q", rec.FloatingIPID, rec.VMIP)
	}
}

// ВМ, которую этот каталог не записывал, с адресом на ней.
type foreignVMCloud struct {
	provision.CloudAPI
	fipName  string
	gone     bool
	released string
}

func (*foreignVMCloud) SetToken(string) {}
func (*foreignVMCloud) ListVMsInProject(context.Context, string) ([]*provision.VM, error) {
	return []*provision.VM{{ID: "vm-9", Name: "user-vm", FloatingIP: "198.51.100.7"}}, nil
}
func (c *foreignVMCloud) DeleteVMInProject(context.Context, string, string) error {
	c.gone = true
	return nil
}
func (c *foreignVMCloud) GetVM(context.Context, string) (*provision.VM, error) {
	if c.gone {
		return nil, clerrors.New(clerrors.CodeVMNotFound, "gone", "")
	}
	return &provision.VM{ID: "vm-9"}, nil
}
func (c *foreignVMCloud) ListFloatingIPs(context.Context, string) ([]*provision.FloatingIP, error) {
	return []*provision.FloatingIP{{ID: "fip-user", Name: c.fipName, IPAddress: "198.51.100.7", State: "in_use"}}, nil
}
func (c *foreignVMCloud) DeleteFloatingIP(_ context.Context, id, _ string) error {
	c.released = id
	return nil
}

// №5: destroy vm_id чужой ВМ не удаляет адрес, зарезервированный
// пользователем; адрес evo (deploy_apply «<имя>-fip») — удаляет; явный
// release_ip=true — удаляет любой. Превью говорит о правиле.
func TestQAB_DestroyTargeted_ForeignVMFloatingIP(t *testing.T) {
	for _, tc := range []struct {
		fipName string
		release any
		want    string
	}{
		{"my-static-ip", nil, ""},
		{"user-vm-fip", nil, "fip-user"},
		{"my-static-ip", true, "fip-user"},
	} {
		cloud := &foreignVMCloud{fipName: tc.fipName}
		r := qaBRegistry(t, cloud, t.TempDir())
		preview, _ := r.handlers["destroy"](context.Background(), map[string]any{"vm_id": "vm-9"})
		if !strings.Contains(preview, "release_ip=true") {
			t.Fatalf("превью не говорит о floating IP: %s", preview)
		}
		args := map[string]any{"vm_id": "vm-9", "confirmed": true}
		if tc.release != nil {
			args["release_ip"] = tc.release
		}
		text, isErr := r.handlers["destroy"](context.Background(), args)
		if isErr || cloud.released != tc.want {
			t.Errorf("%s release_ip=%v: освобождён %q, ждали %q; ответ %s", tc.fipName, tc.release, cloud.released, tc.want, text)
		}
		if tc.want == "" && !strings.Contains(text, "kept") {
			t.Errorf("ответ не говорит, что адрес оставлен: %s", text)
		}
	}
}
