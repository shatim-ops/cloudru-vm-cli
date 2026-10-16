package tools

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/output"
)

type fakeOrg struct {
	status int
	body   string
}

func (f fakeOrg) RoundTrip(r *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	if r.URL.Host != "organization.api.cloud.ru" || r.URL.Path != "/v3/projects" {
		http.Error(rec, "unexpected "+r.URL.String(), 500)
		return rec.Result(), nil
	}
	rec.WriteHeader(f.status)
	_, _ = io.WriteString(rec, f.body)
	return rec.Result(), nil
}

func orgRegistry(t *testing.T, f fakeOrg) *Registry {
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

// Ключ уровня проекта: Organization API отвечает 400 «user not found».
// org_projects отвечал NOT_FOUND (код 4), evo project list — пояснением
// (QA r3 C3, cloud-e2e readonly #23). Теперь оба — без ошибки.
func TestOrgProjects_ProjectLevelKeyIsNotAnError(t *testing.T) {
	r := orgRegistry(t, fakeOrg{400, `{"code":3,"message":"(user_id) is invalid argument: user not found"}`})
	m, text, isErr := callNamed(t, r, "org_projects", nil)
	if isErr {
		t.Fatalf("isError for a project-level key: %s", text)
	}
	if v, ok := m["projects"]; !ok || v != nil {
		t.Errorf("projects = %v, want null", v)
	}
	if m["active"] != addonProject || m["note"] != orgProjectsNote || m["http_status"] != float64(400) {
		t.Errorf("answer %s", text)
	}
	if _, ok := m["reason"]; ok {
		t.Errorf("a reason in a non-error answer: %s", text)
	}
}

func TestOrgProjects_List(t *testing.T) {
	r := orgRegistry(t, fakeOrg{200, `{"projects":[{"id":"p1","name":"dev","status":"ACTIVE","organization_unit_id":"u"}]}`})
	m, text, isErr := callNamed(t, r, "org_projects", nil)
	if isErr {
		t.Fatalf("error: %s", text)
	}
	if ps, _ := m["projects"].([]any); len(ps) != 1 {
		t.Errorf("projects: %s", text)
	}
}
