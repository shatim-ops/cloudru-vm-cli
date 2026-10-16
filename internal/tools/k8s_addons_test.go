package tools

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
)

// Плагины mk8s на поддельном API: формы ответов каталога — как у живого
// (api-probe #5). UUID выдуман.
const addonProject = "00000000-0000-0000-0000-0000000000f1"

// fakeMK8s отвечает на вызовы mk8s.api.cloud.ru: каталог из двух страниц и
// релизы cert-manager в произвольном порядке.
type fakeMK8s struct{}

func (f *fakeMK8s) RoundTrip(r *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	f.serve(rec, r)
	return rec.Result(), nil
}

func (f *fakeMK8s) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Host != "mk8s.api.cloud.ru" {
		http.Error(w, "unexpected host "+r.URL.Host, 500)
		return
	}
	q := r.URL.Query()
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v3/addons":
		if q.Get("projectId") != addonProject {
			http.Error(w, `{"message":"projectId is required"}`, 400)
			return
		}
		if q.Get("pageToken") == "" {
			_, _ = io.WriteString(w, `{"addons":[
				{"name":"cert-manager","displayName":"Cert Manager","description":"Выпуск TLS-сертификатов","categories":["Безопасность"],"isBillable":false},
				{"name":"traefik","displayName":"Traefik","description":"Ingress controller","categories":["Сети"],"isBillable":false}],
				"nextPageToken":"p2"}`)
			return
		}
		_, _ = io.WriteString(w, `{"addons":[{"name":"gpu-operator","displayName":"GPU Operator","categories":["AI","Мониторинг"],"isBillable":true}]}`)
	case r.Method == http.MethodGet && r.URL.Path == "/v3/addons/cert-manager":
		_, _ = io.WriteString(w, `{"releases":[
			{"addonVersion":"1.0.0","appVersion":"v1.13.0","addonNamespace":"cert-manager","mode":"SUPPORT_MODE_ACTIVE","requirements":{"kubeVersions":">=1.25.0-0","readyWorkers":1}},
			{"addonVersion":"1.2.0","appVersion":"v1.15.0","addonNamespace":"cert-manager","mode":"SUPPORT_MODE_EXPERIMENTAL","requirements":{"kubeVersions":">=1.28.0-0","readyWorkers":1}},
			{"addonVersion":"1.1.0","appVersion":"v1.14.0","addonNamespace":"cert-manager","configurationSchema":"{\"type\":\"object\",\"properties\":{\"replicas\":{\"type\":\"integer\"}}}","mode":"SUPPORT_MODE_ACTIVE","requirements":{"kubeVersions":">=1.26.0-0","readyWorkers":"1"}}]}`)
	default:
		http.Error(w, `{"message":"not found"}`, 404)
	}
}

// addonRegistry — реестр с ключом, проектом и поддельным mk8s вместо
// http.DefaultTransport (клиент mk8s ходит через него).
func addonRegistry(t *testing.T, f *fakeMK8s) *Registry {
	t.Helper()
	old := http.DefaultTransport
	http.DefaultTransport = f
	t.Cleanup(func() { http.DefaultTransport = old })
	return New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config:  config.Config{ProjectID: addonProject, KeyID: "k", Secret: "s"},
			Auth:    staticToken{},
			Printer: output.NewWithWriter(&buf, output.ModeText, false),
		}, nil
	})
}

func TestK8sAddons_SearchAndFilters(t *testing.T) {
	r := addonRegistry(t, &fakeMK8s{})
	m, text, isErr := callNamed(t, r, "k8s_addons", nil)
	if isErr || m["total"] != float64(3) || m["shown"] != float64(3) {
		t.Fatalf("whole catalogue: %s", text)
	}
	rows := m["addons"].([]any)
	first := rows[0].(map[string]any)
	if first["name"] != "cert-manager" || first["display_name"] != "Cert Manager" || first["billable"] != false {
		t.Errorf("row: %v", first)
	}
	// Установки в evo пока нет: строка не зовёт k8s_add_addon.
	if _, ok := first["deploy_with"]; ok || strings.Contains(text, "k8s_add_addon") {
		t.Errorf("rows name an install tool: %s", text)
	}
	if strings.Contains(text, "description") || strings.Contains(text, "Выпуск TLS") {
		t.Errorf("rows carry the description: %s", text)
	}
	if cats := m["categories"].([]any); len(cats) != 4 || cats[0] != "AI" {
		t.Errorf("categories: %v", cats)
	}

	names := func(args map[string]any) string {
		t.Helper()
		m, text, isErr := callNamed(t, r, "k8s_addons", args)
		if isErr {
			t.Fatalf("%v: %s", args, text)
		}
		var out []string
		for _, row := range m["addons"].([]any) {
			out = append(out, row.(map[string]any)["name"].(string))
		}
		return strings.Join(out, ",")
	}
	for _, c := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"query": "CERT"}, "cert-manager"},
		{map[string]any{"query": "ingress"}, "traefik"},       // описание
		{map[string]any{"query": "tls-серт"}, "cert-manager"}, // описание по-русски, без учёта регистра
		{map[string]any{"category": "сети"}, "traefik"},
		{map[string]any{"category": "Мониторинг"}, "gpu-operator"},
		{map[string]any{"billable": true}, "gpu-operator"},
		{map[string]any{"billable": false}, "cert-manager,traefik"},
		{map[string]any{"query": "nothing-like-it"}, ""},
	} {
		if got := names(c.args); got != c.want {
			t.Errorf("%v: %q, want %q", c.args, got, c.want)
		}
	}
}

func TestK8sAddonVersions(t *testing.T) {
	r := addonRegistry(t, &fakeMK8s{})
	m, text, isErr := callNamed(t, r, "k8s_addon_versions", map[string]any{"name": "Cert-Manager"})
	if isErr {
		t.Fatal(text)
	}
	rels := m["releases"].([]any)
	var order []string
	for _, x := range rels {
		order = append(order, x.(map[string]any)["version"].(string))
	}
	if strings.Join(order, " ") != "v1.15.0 v1.14.0 v1.13.0" {
		t.Errorf("order: %v", order)
	}
	// Последняя — новейшая ACTIVE, не EXPERIMENTAL v1.15.0.
	if m["latest"] != "v1.14.0" || m["addon"] != "cert-manager" {
		t.Errorf("latest: %v", m["latest"])
	}
	r14 := rels[1].(map[string]any)
	if r14["latest"] != true || r14["mode"] != "ACTIVE" || r14["kube_versions"] != ">=1.26.0-0" || r14["ready_workers"] != float64(1) ||
		r14["namespace"] != "cert-manager" || r14["has_schema"] != true || r14["schema_bytes"] != float64(62) || r14["addon_version"] != "1.1.0" {
		t.Errorf("release: %v", r14)
	}
	if rels[0].(map[string]any)["mode"] != "EXPERIMENTAL" || rels[0].(map[string]any)["latest"] != false {
		t.Errorf("experimental release: %v", rels[0])
	}
	if strings.Contains(text, "configuration_schema") {
		t.Errorf("schema without full=true: %s", text)
	}
	if _, ok := m["deploy_with"]; ok {
		t.Errorf("deploy_with without an install tool: %s", text)
	}

	m, _, _ = callNamed(t, r, "k8s_addon_versions", map[string]any{"name": "cert-manager", "full": true})
	schema, _ := m["releases"].([]any)[1].(map[string]any)["configuration_schema"].(map[string]any)
	if schema["type"] != "object" {
		t.Errorf("full=true: schema %v", m["releases"].([]any)[1])
	}

	m, _, isErr = callNamed(t, r, "k8s_addon_versions", map[string]any{"name": "cert-manger"})
	hint, _ := m["hint"].(string)
	if !isErr || m["reason"] != clierrors.CodeNotFound || !strings.Contains(hint, "evo k8s addons") || !strings.Contains(hint, "cert-manager") {
		t.Errorf("unknown addon: %v", m)
	}
	if m, _, isErr := callNamed(t, r, "k8s_addon_versions", nil); !isErr || !strings.HasPrefix(m["error"].(string), clierrors.CodeUsage) {
		t.Errorf("no name: %v", m)
	}
}

func TestK8sAddonTools_GroupReadOnly(t *testing.T) {
	r := New(nilFactory)
	for _, name := range []string{"k8s_addons", "k8s_addon_versions"} {
		tl, ok := r.Lookup(name)
		if !ok || tl.Group != "k8s" || tl.Mutating {
			t.Errorf("%s: group %q mutating %v", name, tl.Group, tl.Mutating)
		}
	}
	// Установка убрана из v1.0 (BACKLOG [k8s-addons]).
	if _, ok := r.Lookup("k8s_add_addon"); ok {
		t.Error("k8s_add_addon is registered")
	}
}
