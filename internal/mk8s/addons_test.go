package mk8s

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// Все ID выдуманы; формы ответов — как у живого API (api-probe #5).
const fakeProject = "00000000-0000-0000-0000-0000000000f1"

type addonAPI struct {
	mu   sync.Mutex
	reqs []*http.Request
}

func (a *addonAPI) last() *http.Request {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.reqs[len(a.reqs)-1]
}

func newAddonAPI(t *testing.T, h func(w http.ResponseWriter, r *http.Request)) (*Client, *addonAPI) {
	t.Helper()
	rec := &addonAPI{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.reqs = append(rec.reqs, r)
		rec.mu.Unlock()
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	return New("tok", fakeProject, srv.URL), rec
}

// Каталог читается по страницам до пустого nextPageToken; projectId — в
// camelCase и только он (без него API отвечает 400).
func TestListAddons_Paged(t *testing.T) {
	cli, rec := newAddonAPI(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/v3/addons" || q.Get("projectId") != fakeProject || q.Get("project_id") != "" || q.Get("pageSize") != "100" {
			http.Error(w, `{"message":"bad request"}`, 400)
			return
		}
		switch q.Get("pageToken") {
		case "":
			_, _ = io.WriteString(w, `{"addons":[{"name":"cert-manager","displayName":"Cert Manager","description":"TLS","categories":["Безопасность"],"isBillable":false,"createdAt":"2024-01-01T00:00:00Z"}],"nextPageToken":"p2"}`)
		case "p2":
			_, _ = io.WriteString(w, `{"addons":[{"name":"traefik","displayName":"Traefik","categories":["Сети"],"isBillable":true}],"nextPageToken":""}`)
		default:
			http.Error(w, "unexpected token", 400)
		}
	})
	got, err := cli.ListAddons(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "cert-manager" || got[0].Categories[0] != "Безопасность" || !got[1].IsBillable || got[0].IsBillable {
		t.Fatalf("addons: %+v", got)
	}
	if n := len(rec.reqs); n != 2 {
		t.Errorf("%d requests, want 2", n)
	}
	if h := rec.last().Header.Get("Authorization"); h != "Bearer tok" {
		t.Errorf("Authorization: %q", h)
	}
}

// Сервер, который отдаёт один и тот же токен, не зацикливает клиент.
func TestListAddons_RepeatedTokenStops(t *testing.T) {
	cli, rec := newAddonAPI(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"addons":[{"name":"a"}],"nextPageToken":"same"}`)
	})
	if _, err := cli.ListAddons(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := len(rec.reqs); n != 2 {
		t.Errorf("%d requests, want 2 (stop at the repeated token)", n)
	}
}

func TestListAddons_Error(t *testing.T) {
	cli, _ := newAddonAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = io.WriteString(w, `{"message":"projectId is required"}`)
	})
	if _, err := cli.ListAddons(context.Background()); err == nil || !strings.Contains(err.Error(), "400") || !strings.Contains(err.Error(), "projectId is required") {
		t.Fatalf("err = %v", err)
	}
}

// Релизы: схема — строкой, readyWorkers — числом или строкой (int64 в
// protojson), режим с префиксом SUPPORT_MODE_.
func TestGetAddonReleases(t *testing.T) {
	cli, rec := newAddonAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("pageToken") == "" {
			_, _ = io.WriteString(w, `{"releases":[{"addonVersion":"1.0.0","appVersion":"v1.14.0","addonNamespace":"cert-manager","configurationSchema":"{\"type\":\"object\"}","mode":"SUPPORT_MODE_ACTIVE","requirements":{"kubeVersions":">=1.26.0-0","readyWorkers":1}}],"nextPageToken":"n"}`)
			return
		}
		// Живой API 16.10: addonNamespace повторяет addonVersion.
		_, _ = io.WriteString(w, `{"releases":[{"addonVersion":"0.9.0-sbc.0","addonNamespace":"0.9.0-sbc.0","appVersion":"v1.13.0","mode":"SUPPORT_MODE_END_OF_LIFE","requirements":{"readyWorkers":"2"}}]}`)
	})
	got, err := cli.GetAddonReleases(context.Background(), "cert-manager")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("releases: %+v", got)
	}
	r := got[0]
	if r.AppVersion != "v1.14.0" || r.AddonNamespace != "cert-manager" || r.ConfigurationSchema != `{"type":"object"}` ||
		r.Requirements.KubeVersions != ">=1.26.0-0" || r.Requirements.ReadyWorkers != 1 || NormalizeMode(r.Mode) != ModeActive {
		t.Errorf("release 0: %+v", r)
	}
	if got[1].AddonNamespace != "" {
		t.Errorf("a version-shaped addonNamespace kept: %q", got[1].AddonNamespace)
	}
	if got[1].Requirements.ReadyWorkers != 2 {
		t.Errorf("readyWorkers as a string: %+v", got[1].Requirements)
	}
	if p := rec.reqs[0].URL.Path; p != "/v3/addons/cert-manager" {
		t.Errorf("path %q", p)
	}
}

// Имя плагина из аргументов агента не уводит запрос на другой путь.
func TestGetAddonReleases_EscapesName(t *testing.T) {
	cli, rec := newAddonAPI(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"releases":[]}`)
	})
	_, _ = cli.GetAddonReleases(context.Background(), "../clusters")
	if p := rec.last().URL.EscapedPath(); p != "/v3/addons/..%2Fclusters" {
		t.Errorf("escaped path %q", p)
	}
}
