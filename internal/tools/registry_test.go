package tools

import (
	"errors"
	"testing"

	"github.com/shatim-ops/cloudru-vm-cli/internal/engine"
)

// nilFactory is an EngineFactory that fails — for tests that must not reach
// the cloud.
func nilFactory(map[string]any) (*engine.Engine, error) {
	return nil, errors.New("no engine in tests")
}

// newTestServer keeps the name the handler tests had in internal/mcp.
func newTestServer() *Registry {
	return New(nilFactory)
}

// Every tool has exactly one handler and every handler belongs to a tool.
func TestRegistry_OneHandlerPerTool(t *testing.T) {
	r := newTestServer()
	seen := map[string]bool{}
	for _, tl := range r.Tools() {
		if seen[tl.Name] {
			t.Errorf("duplicate tool %q", tl.Name)
		}
		seen[tl.Name] = true
		if tl.Handler == nil {
			t.Errorf("tool %q has no handler", tl.Name)
		}
	}
	for name := range r.handlers {
		if !seen[name] {
			t.Errorf("handler %q has no tool definition", name)
		}
	}
	if len(r.Tools()) != 146 {
		t.Errorf("registry has %d tools, want 146", len(r.Tools()))
	}
}

func TestRegistry_EveryToolHasAKnownGroup(t *testing.T) {
	known := map[string]bool{}
	for _, g := range Groups() {
		known[g] = true
	}
	if len(known) != 23 {
		t.Errorf("%d groups, want 23: %v", len(known), Groups())
	}
	used := map[string]bool{}
	for _, tl := range newTestServer().Tools() {
		if !known[tl.Group] {
			t.Errorf("tool %q: group %q is not a known group", tl.Name, tl.Group)
		}
		used[tl.Group] = true
	}
	for g := range known {
		if !used[g] {
			t.Errorf("group %q has no tools", g)
		}
	}
}

func TestRegistry_Mutating(t *testing.T) {
	r := newTestServer()
	cases := map[string]bool{
		"disk_delete":      true,
		"deploy":           true,
		"s3_bucket_create": true,
		"cert_private_key": false,
		"list_zones":       false,
		"vm_get":           false,
		"pg_list":          false,
	}
	for name, want := range cases {
		tl, ok := r.Lookup(name)
		if !ok {
			t.Fatalf("tool %q not found", name)
		}
		if tl.Mutating != want {
			t.Errorf("%s: mutating = %v, want %v", name, tl.Mutating, want)
		}
	}
	if _, ok := r.Lookup("nope"); ok {
		t.Error("Lookup found a tool that does not exist")
	}
}
