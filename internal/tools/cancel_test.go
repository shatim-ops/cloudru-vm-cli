package tools

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/cloudru-vm-cli/internal/config"
	"github.com/shatim-ops/cloudru-vm-cli/internal/deployplan"
	"github.com/shatim-ops/cloudru-vm-cli/internal/engine"
	"github.com/shatim-ops/cloudru-vm-cli/internal/output"
	"github.com/shatim-ops/cloudru-vm-cli/internal/provision"
	"github.com/shatim-ops/cloudru-vm-cli/internal/stack"
	"github.com/shatim-ops/cloudru-vm-cli/internal/state"
)

// Вызов MCP отменяют (клиент ушёл, notifications/cancelled, остановка
// сервера) уже после того, как запрос создания ушёл в облако. ВМ есть и
// оплачивается: ответ должен её назвать, state — содержать, а повтор — не
// создавать вторую.

// cancelCloud создаёт ВМ и тут же отменяет контекст вызова — как клиент,
// ушедший сразу после POST создания.
type cancelCloud struct {
	provision.CloudAPI
	cancel  context.CancelFunc
	creates int
}

func (*cancelCloud) SetToken(string)                                  {}
func (*cancelCloud) ListVMs(context.Context) ([]*provision.VM, error) { return nil, nil }
func (*cancelCloud) ListFlavors(context.Context) ([]*provision.Flavor, error) {
	return []*provision.Flavor{{ID: "fl-1", Name: "lowcost10-1-1", VCPUs: 1, RAMGB: 1}}, nil
}
func (*cancelCloud) GetImage(context.Context, string) (*provision.ImageDetail, error) {
	return &provision.ImageDetail{ID: "img-1", Name: "ubuntu"}, nil
}
func (*cancelCloud) ListAvailabilityZones(context.Context) ([]*provision.AvailabilityZone, error) {
	return nil, nil
}
func (*cancelCloud) ListSubnets(context.Context, string) ([]*provision.Subnet, error) {
	return []*provision.Subnet{{ID: "sn-1", Name: "default"}}, nil
}
func (*cancelCloud) ListSecurityGroups(context.Context, string) ([]*provision.SecurityGroup, error) {
	return nil, nil
}
func (c *cancelCloud) CreateVM(context.Context, provision.CreateVMRequest) (*provision.VM, error) {
	c.creates++
	c.cancel()
	return &provision.VM{ID: "vm-made"}, nil
}

func cancelRegistry(t *testing.T, cloud provision.CloudAPI) (*Registry, string) {
	t.Helper()
	dir := t.TempDir()
	r := New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config:    config.Config{ProjectID: "p", Region: "z", ImageID: "img-1", KeyID: "k", Secret: "s"},
			Auth:      staticToken{},
			Cloud:     cloud,
			Printer:   output.NewWithWriter(&buf, output.ModeText, false),
			StateDir:  dir,
			SSHKeyPEM: []byte("key"),
			SSHPubKey: "ssh-ed25519 AAAA test",
		}, nil
	})
	return r, dir
}

func stateHasVM(t *testing.T, dir, vmID string) *state.DeploymentRecord {
	t.Helper()
	sf, err := state.Read(state.DefaultPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	for i := range sf.Deployments {
		if sf.Deployments[i].VMID == vmID {
			return &sf.Deployments[i]
		}
	}
	t.Fatalf("ВМ %s нет в state: %+v", vmID, sf.Deployments)
	return nil
}

func TestCancel_ProvisionAfterCreateNamesAndRecordsVM(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cloud := &cancelCloud{cancel: cancel}
	r, dir := cancelRegistry(t, cloud)
	text, isErr := r.handlers["provision"](ctx, map[string]any{"name": "web", "confirmed": true})
	if !isErr || !strings.Contains(text, "vm-made") || !strings.Contains(text, `"vm_id": "vm-made"`) {
		t.Fatalf("ответ не называет ВМ: %s", text)
	}
	if rec := stateHasVM(t, dir, "vm-made"); rec.Status != "creating" {
		t.Errorf("status в state = %q", rec.Status)
	}
}

func TestCancel_DeployApplyAfterCreateRecordsVMAndKeepsPlan(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cloud := &cancelCloud{cancel: cancel}
	r, dir := cancelRegistry(t, cloud)
	plan := &deployplan.Plan{PlanID: "plan-0123456789ab", ExpiresAt: time.Now().Add(time.Hour), VM: deployplan.VMSpec{Name: "web"}}
	if err := deployplan.Save(dir, plan); err != nil {
		t.Fatal(err)
	}
	text, isErr := r.handlers["deploy_apply"](ctx, map[string]any{"plan_id": plan.PlanID, "confirmed": true})
	if !isErr || !strings.Contains(text, "vm-made") {
		t.Fatalf("ответ не называет ВМ: %s", text)
	}
	if rec := stateHasVM(t, dir, "vm-made"); rec.Status != "creating" {
		t.Errorf("status в state = %q", rec.Status)
	}
	// План остаётся: повтор с тем же plan_id подхватывает ВМ по имени.
	if _, err := deployplan.Load(dir, plan.PlanID); err != nil {
		t.Errorf("план удалён после прерывания: %v", err)
	}
}

// Шаг vm стека: provision прервали после создания — шаг помнит ВМ, попытка
// не считается, следующий crank берёт ВМ, а не создаёт вторую.
func TestCancel_StackStepInterruptedAdoptsOnNextCrank(t *testing.T) {
	stackHome(t)
	r := newTestServer()
	j := planJob(t, `{"name":"i","vm":{"name":"v"}}`)
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	r.handlers["provision"] = func(context.Context, map[string]any) (string, bool) {
		calls++
		cancel()
		return toJSON(map[string]any{"status": "error", "vm_id": "vm-9", "error": "interrupted"}), true
	}
	r.handleStackApply(ctx, map[string]any{"job_id": j.ID, "confirmed": true})
	job, err := stack.LoadJob(j.ID)
	if err != nil {
		t.Fatal(err)
	}
	st := job.Steps[0]
	if !st.Interrupted || st.ResourceID != "vm-9" || st.Attempts != 0 || st.Status != "pending" {
		t.Fatalf("после отмены: %+v", st)
	}
	r.handleStackApply(context.Background(), map[string]any{"job_id": j.ID, "confirmed": true})
	job, _ = stack.LoadJob(j.ID)
	if calls != 1 || job.Outputs["vm"] != "vm-9" || job.Steps[0].Status != "done" {
		t.Fatalf("provision вызван %d раз, outputs %v, шаг %+v", calls, job.Outputs, job.Steps[0])
	}
}

// Шаг с поиском по имени (postgres): прерванный create без id в ответе —
// следующий crank находит кластер через pg_list и не создаёт второй.
func TestCancel_StackStepInterruptedReconcilesByName(t *testing.T) {
	stackHome(t)
	r := newTestServer()
	j := planJob(t, `{"name":"i","postgres":{"name":"p"}}`)
	ctx, cancel := context.WithCancel(context.Background())
	creates := 0
	r.handlers["pg_create"] = func(context.Context, map[string]any) (string, bool) {
		creates++
		cancel()
		return errResult(context.Canceled), true
	}
	r.handlers["pg_list"] = fakeOK(map[string]any{"clusters": []any{map[string]any{"name": "p", "id": "pg-1"}}})
	r.handlers["pg_get"] = fakeOK(map[string]any{"status": "CREATING"})
	r.handleStackApply(ctx, map[string]any{"job_id": j.ID, "confirmed": true})
	r.handleStackApply(context.Background(), map[string]any{"job_id": j.ID, "confirmed": true})
	job, _ := stack.LoadJob(j.ID)
	if creates != 1 || job.Steps[0].ResourceID != "pg-1" || job.Steps[0].Status != "waiting" {
		t.Fatalf("pg_create вызван %d раз, шаг %+v", creates, job.Steps[0])
	}
}
