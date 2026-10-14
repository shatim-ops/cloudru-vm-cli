// Package tools is the registry of cloudru-vm operations: name, group,
// description, JSON schema of the arguments, handler, and whether the
// operation changes anything in the cloud.
//
// The registry does not know about transports. The MCP server
// (internal/mcp) and the CLI ('cloudru-vm tools …' and the command trees)
// are two clients of the same handlers: arguments come in as
// map[string]any, the result goes out as a JSON string plus an isError flag.
//
// Handlers never write to stdout: engine output is redirected to in-memory
// buffers, because for MCP stdout is the protocol channel.
package tools

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/shatim-ops/cloudru-vm-cli/internal/engine"
)

// EngineFactory builds an *engine.Engine from a set of config overrides.
// Overrides use the same field names as config flags (e.g. "ComposeFile",
// "Region", "ProjectID"). A nil or empty map yields the default config.
type EngineFactory func(overrides map[string]any) (*engine.Engine, error)

// Handler executes a single tool call. It returns the textual (JSON) result
// and a flag telling whether the result is a tool-level error.
type Handler func(ctx context.Context, args map[string]any) (text string, isError bool)

// toolHandler is the name the handler files use.
type toolHandler = Handler

// Tool is one registry entry.
type Tool struct {
	Name        string         `json:"name"`
	Group       string         `json:"group"`
	Mutating    bool           `json:"mutating"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Handler     Handler        `json:"-"`
}

// OpWaitMaxSeconds is the longest wait_seconds the *_operation tools honour.
const OpWaitMaxSeconds = opWaitMaxSeconds

// Registry holds every tool and its handler. Handlers build an engine per
// call through the factory, so each call sees the current config, profile
// and working directory.
type Registry struct {
	newEngine EngineFactory
	tools     []Tool
	handlers  map[string]toolHandler

	// jobLocks serialize the stack tools per job_id (see lockJob).
	jobMu    sync.Mutex
	jobLocks map[string]*sync.Mutex
}

// New builds the registry. factory is called by handlers that need an
// engine (credentials, project, region).
func New(factory EngineFactory) *Registry {
	r := &Registry{newEngine: factory}
	r.registerTools()
	for i := range r.tools {
		t := &r.tools[i]
		t.Group = groupOf(t.Name)
		t.Mutating = isMutating(*t)
		t.Handler = r.handlers[t.Name]
	}
	return r
}

// Tools returns the tools in registration order (the order MCP advertises).
func (r *Registry) Tools() []Tool {
	return r.tools
}

// Lookup returns the tool with the given name.
func (r *Registry) Lookup(name string) (Tool, bool) {
	for _, t := range r.tools {
		if t.Name == name {
			return t, true
		}
	}
	return Tool{}, false
}

// Wrap replaces the handler of the named tool with wrap(current handler).
// Every caller sees the new handler: transports, Tools/Lookup, and the
// in-process calls the stack orchestrator makes. It reports whether the
// tool exists. Transport policies (MCP over HTTP) use it; the CLI builds its
// own registry and is not affected.
func (r *Registry) Wrap(name string, wrap func(next Handler) Handler) bool {
	next, ok := r.handlers[name]
	if !ok {
		return false
	}
	h := wrap(next)
	r.handlers[name] = h
	for i := range r.tools {
		if r.tools[i].Name == name {
			r.tools[i].Handler = h
		}
	}
	return true
}

// ErrResult formats an error as a JSON tool result, as the handlers do.
func ErrResult(err error) string { return errResult(err) }

// BoolArg reads a boolean argument, def when it is absent.
func BoolArg(args map[string]any, key string, def bool) bool { return boolArg(args, key, def) }

// Groups returns the group names in alphabetical order.
func Groups() []string {
	seen := map[string]bool{}
	for _, g := range groupByName {
		seen[g] = true
	}
	for _, p := range groupPrefixes {
		seen[p.group] = true
	}
	out := make([]string, 0, len(seen))
	for g := range seen {
		out = append(out, g)
	}
	sort.Strings(out)
	return out
}

// groupByName assigns tools whose name does not start with their group.
var groupByName = map[string]string{
	"deploy":       "deploy",
	"deploy_plan":  "deploy",
	"deploy_apply": "deploy",
	"status":       "deploy",
	"list":         "deploy",
	"logs":         "deploy",
	"destroy":      "deploy",
	"open_ports":   "deploy",

	"provision":    "vm",
	"vm_get":       "vm",
	"list_zones":   "vm",
	"list_flavors": "vm",

	"list_images": "image",

	"list_disk_types": "disk",

	"list_subnets":         "vpc",
	"list_security_groups": "vpc",
	"list_floating_ips":    "vpc",
	"release_floating_ip":  "vpc",
	"fip_attach":           "vpc",

	"iam_service_accounts": "iam",
	"org_projects":         "org",
	"org_quotas":           "org",
}

// groupPrefixes assigns the rest by name prefix.
var groupPrefixes = []struct{ prefix, group string }{
	{"vm_", "vm"},
	{"image_", "image"},
	{"disk_", "disk"},
	{"vpc_", "vpc"},
	{"snat_", "snat"},
	{"k8s_", "k8s"},
	{"pg_", "pg"},
	{"kafka_", "kafka"},
	{"redis_", "redis"},
	{"lb_", "nlb"},
	{"tg_", "nlb"},
	{"dns_", "dns"},
	{"secret_", "secret"},
	{"ar_", "ar"},
	{"ca_", "ca"},
	{"cert_", "cert"},
	{"s3_", "s3"},
	{"billing_", "billing"},
	{"metrics_", "monitoring"},
	{"audit_", "audit"},
	{"stack_", "stack"},
}

// groupOf returns the group of a tool, or "" when none matches (a test
// keeps that from happening).
func groupOf(name string) string {
	if g, ok := groupByName[name]; ok {
		return g
	}
	for _, p := range groupPrefixes {
		if strings.HasPrefix(name, p.prefix) {
			return p.group
		}
	}
	return ""
}

// mutatingOverride corrects the rule "a tool is mutating when it takes
// confirmed": cert_private_key asks for consent because it reveals a secret,
// not because it changes anything.
var mutatingOverride = map[string]bool{
	"cert_private_key": false,
}

// isMutating reports whether the tool changes state in the cloud.
func isMutating(t Tool) bool {
	if v, ok := mutatingOverride[t.Name]; ok {
		return v
	}
	props, _ := t.InputSchema["properties"].(map[string]any)
	_, gated := props["confirmed"]
	return gated
}
