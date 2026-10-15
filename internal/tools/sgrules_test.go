package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/cloudru-vm-cli/internal/config"
	"github.com/shatim-ops/cloudru-vm-cli/internal/deployplan"
	"github.com/shatim-ops/cloudru-vm-cli/internal/engine"
	"github.com/shatim-ops/cloudru-vm-cli/internal/output"
	"github.com/shatim-ops/cloudru-vm-cli/internal/provision"
	"github.com/shatim-ops/cloudru-vm-cli/internal/state"
)

// sgCloud is a project with one VM and one group with rules.
type sgCloud struct {
	provision.CloudAPI
	vm      *provision.VM
	rules   []*provision.SGRule
	deleted []string
}

func (*sgCloud) SetToken(string) {}
func (c *sgCloud) ListVMs(context.Context) ([]*provision.VM, error) {
	return []*provision.VM{c.vm}, nil
}
func (c *sgCloud) ListVMsInProject(context.Context, string) ([]*provision.VM, error) {
	return []*provision.VM{c.vm}, nil
}
func (*sgCloud) DeleteVMInProject(context.Context, string, string) error { return nil }
func (*sgCloud) ListSecurityGroups(context.Context, string) ([]*provision.SecurityGroup, error) {
	return []*provision.SecurityGroup{{ID: "sg-1", Name: "SSH-access_ru.AZ-1"}}, nil
}
func (c *sgCloud) ListSecurityGroupRules(context.Context, string) ([]*provision.SGRule, error) {
	return c.rules, nil
}
func (c *sgCloud) DeleteSecurityGroupRule(_ context.Context, sg, id string) error {
	c.deleted = append(c.deleted, sg+"/"+id)
	return nil
}

func sgRegistry(t *testing.T, cloud *sgCloud) (*Registry, string) {
	t.Helper()
	dir := t.TempDir()
	return New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config:   config.Config{ProjectID: "00000000-0000-0000-0000-000000000001"},
			Auth:     staticToken{},
			Cloud:    cloud,
			Printer:  output.NewWithWriter(&buf, output.ModeText, false),
			StateDir: dir,
		}, nil
	}), dir
}

func callSGTool(t *testing.T, r *Registry, name string, args map[string]any) string {
	t.Helper()
	for _, tl := range r.Tools() {
		if tl.Name == name {
			text, isErr := tl.Handler(context.Background(), args)
			if isErr {
				t.Fatalf("%s: %s", name, text)
			}
			return text
		}
	}
	t.Fatalf("no tool %s", name)
	return ""
}

func writeRecords(t *testing.T, dir string, recs ...state.DeploymentRecord) {
	t.Helper()
	sf := &state.File{Version: 1}
	for _, r := range recs {
		sf.UpsertDeployment(r)
	}
	if err := state.Write(state.DefaultPath(dir), sf); err != nil {
		t.Fatal(err)
	}
}

// MCP destroy of a VM by id closes the rules its deployment created, with
// the same code as the CLI destroy.
func TestDestroyTargeted_ClosesOwnRules(t *testing.T) {
	cloud := &sgCloud{vm: &provision.VM{ID: "vm-1", Name: "web"}}
	r, dir := sgRegistry(t, cloud)
	writeRecords(t, dir, state.DeploymentRecord{ID: "vm-1", VMID: "vm-1", Status: "running", SecurityGroupID: "sg-1",
		SecurityGroupPorts: []string{"80/tcp"}, OpenedRules: []state.SGRule{{ID: "rule-80", SecurityGroupID: "sg-1", Port: "80/tcp"}}})

	text := callSGTool(t, r, "destroy", map[string]any{"vm_id": "vm-1", "confirmed": true, "release_ip": false})
	if strings.Join(cloud.deleted, ",") != "sg-1/rule-80" {
		t.Errorf("deleted = %v", cloud.deleted)
	}
	if !strings.Contains(text, "closed ports 80") {
		t.Errorf("answer: %s", text)
	}
	sf, _ := state.Read(state.DefaultPath(dir))
	if len(sf.Deployments) != 0 {
		t.Errorf("state left: %+v", sf.Deployments)
	}
}

// deploy_apply adopting an existing VM opens nothing; the rules an earlier
// apply recorded must stay with the record, or destroy never deletes them.
func TestDeployApply_AdoptKeepsRecordedRules(t *testing.T) {
	cloud := &sgCloud{vm: &provision.VM{ID: "vm-1", Name: "wiki-1", Status: "running"}}
	r, dir := sgRegistry(t, cloud)
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	writeRecords(t, dir, state.DeploymentRecord{ID: "vm-1", VMID: "vm-1", Status: "running", SecurityGroupID: "sg-1",
		SecurityGroupPorts: []string{"80/tcp"}, OpenedRules: []state.SGRule{{ID: "rule-80", SecurityGroupID: "sg-1", Port: "80/tcp"}},
		CreatedAt: created})
	plan := &deployplan.Plan{PlanID: "plan-0123456789ab", ExpiresAt: time.Now().Add(time.Hour), Product: "wiki",
		VM: deployplan.VMSpec{Name: "wiki-1"}, Network: deployplan.NetworkSpec{SecurityGroupID: "sg-1"}, Ports: []int{80}}
	if err := deployplan.Save(dir, plan); err != nil {
		t.Fatal(err)
	}

	callSGTool(t, r, "deploy_apply", map[string]any{"plan_id": plan.PlanID, "confirmed": true})
	sf, _ := state.Read(state.DefaultPath(dir))
	rec := sf.FindDeployment("vm-1")
	if rec == nil || rec.SecurityGroupID != "sg-1" || len(rec.OpenedRules) != 1 || rec.OpenedRules[0].ID != "rule-80" ||
		!rec.CreatedAt.Equal(created) {
		t.Errorf("record = %+v", rec)
	}
}

// list_security_groups rules=true shows each group's rules (the group list
// carries none); without it the answer is as before.
func TestListSecurityGroups_Rules(t *testing.T) {
	cloud := &sgCloud{rules: []*provision.SGRule{{ID: "rule-80", Direction: "ingress", IPProtocol: "tcp", PortRange: "80:80", Description: "cloudru-vm: port 80", State: "created"}}}
	r, _ := sgRegistry(t, cloud)

	var got struct {
		Groups []struct {
			ID    string             `json:"id"`
			Rules []provision.SGRule `json:"rules"`
		} `json:"security_groups"`
	}
	if err := json.Unmarshal([]byte(callSGTool(t, r, "list_security_groups", map[string]any{"rules": true})), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Groups) != 1 || len(got.Groups[0].Rules) != 1 || got.Groups[0].Rules[0].Description != "cloudru-vm: port 80" {
		t.Errorf("got %+v", got)
	}
	if text := callSGTool(t, r, "list_security_groups", nil); strings.Contains(text, "rules") {
		t.Errorf("rules without rules=true: %s", text)
	}
}
