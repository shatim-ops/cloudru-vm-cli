package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// Дерево k8s: addons и addon-versions на поддельном mk8s (формы каталога —
// как у живого API, api-probe #5).

// addonAPI отвечает вместо mk8s.api.cloud.ru.
type addonAPI struct{}

func (addonAPI) RoundTrip(r *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	switch {
	case r.URL.Host != "mk8s.api.cloud.ru" || r.URL.Query().Get("projectId") == "" && r.URL.Query().Get("project_id") == "":
		http.Error(w, `{"message":"unexpected"}`, 400)
	case r.URL.Path == "/v3/addons":
		_, _ = io.WriteString(w, `{"addons":[
			{"name":"cert-manager","displayName":"Cert Manager","description":"TLS-сертификаты","categories":["Безопасность"],"isBillable":false},
			{"name":"traefik","displayName":"Traefik Ingress","categories":["Сети"],"isBillable":false},
			{"name":"gpu-operator","displayName":"GPU Operator","categories":["AI","Мониторинг"],"isBillable":true}]}`)
	case r.URL.Path == "/v3/addons/cert-manager":
		_, _ = io.WriteString(w, `{"releases":[
			{"addonVersion":"1.0.0","appVersion":"v1.13.0","addonNamespace":"cert-manager","mode":"SUPPORT_MODE_END_OF_SUPPORT","requirements":{"kubeVersions":">=1.25.0-0"}},
			{"addonVersion":"1.1.0","appVersion":"v1.14.0","addonNamespace":"cert-manager","configurationSchema":"{\"type\":\"object\",\"properties\":{\"replicas\":{\"type\":\"integer\"}}}","mode":"SUPPORT_MODE_ACTIVE","requirements":{"kubeVersions":">=1.26.0-0","readyWorkers":1}}]}`)
	default:
		http.Error(w, `{"message":"not found"}`, 404)
	}
	return w.Result(), nil
}

func withAddonAPI(t *testing.T) {
	t.Helper()
	t.Setenv("CLOUDRU_PROJECT_ID", pvProject)
	oldRT := http.DefaultTransport
	http.DefaultTransport = addonAPI{}
	old := toolEngine
	toolEngine = func(o map[string]any) (*engine.Engine, error) {
		e, err := buildEngine(o)
		if err != nil {
			return nil, err
		}
		e.Auth = previewToken{}
		return e, nil
	}
	t.Cleanup(func() { toolEngine, http.DefaultTransport = old, oldRT })
}

func TestK8sAddons_RussianTable(t *testing.T) {
	russian(t)
	withAddonAPI(t)
	out, errOut, code := execCLI(t, "k8s", "addons")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	t.Logf("\n%s", out)
	assertHas(t, out, []string{
		"ИМЯ           ОТОБРАЖАЕМОЕ ИМЯ  КАТЕГОРИИ       ПЛАТНЫЙ\n",
		"cert-manager  Cert Manager      Безопасность    нет\n",
		"gpu-operator  GPU Operator      AI, Мониторинг  да\n",
		"Показаны 3 из 3. Версии: evo k8s addon-versions <имя>. Установка плагинов через evo пока недоступна: устанавливайте в консоли Cloud.ru.\n",
	})

	out, _, _ = execCLI(t, "k8s", "addons", "tls")
	assertHas(t, out, []string{"cert-manager", "Показаны 1 из 3."})
	out, _, _ = execCLI(t, "k8s", "addons", "--category", "сети")
	assertHas(t, out, []string{"traefik", "Показаны 1 из 3."})
	out, _, _ = execCLI(t, "k8s", "addons", "--billable")
	assertHas(t, out, []string{"gpu-operator", "Показаны 1 из 3."})

	out, _, code = execCLI(t, "k8s", "addons", "windows")
	if code != 0 {
		t.Fatalf("nothing found: exit %d", code)
	}
	assertHas(t, out, []string{"Плагины не найдены.", "evo k8s addons <подстрока>", "Категории (--category): AI, Безопасность, Мониторинг, Сети."})
}

func TestK8sAddons_JSON(t *testing.T) {
	withAddonAPI(t)
	out, errOut, code := execCLI(t, "--json", "k8s", "addons", "cert")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var got struct {
		Addons []map[string]any `json:"addons"`
		Total  int              `json:"total"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil || got.Total != 3 || len(got.Addons) != 1 {
		t.Fatalf("%v: %s", err, out)
	}
	if _, ok := got.Addons[0]["deploy_with"]; ok || got.Addons[0]["name"] != "cert-manager" {
		t.Errorf("row: %v", got.Addons[0])
	}
}

func TestK8sAddonVersions_RussianTable(t *testing.T) {
	russian(t)
	withAddonAPI(t)
	out, errOut, code := execCLI(t, "k8s", "addon-versions", "cert-manager")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	t.Logf("\n%s", out)
	assertHas(t, out, []string{
		"cert-manager — Cert Manager\n",
		"ВЕРСИЯ     ВЕРСИЯ ПЛАГИНА  ПОДДЕРЖКА              KUBERNETES  УЗЛОВ  NAMESPACE     СХЕМА НАСТРОЕК\n",
		"v1.14.0 *  1.1.0           активная               >=1.26.0-0  1      cert-manager  62 Б\n",
		"v1.13.0    1.0.0           поддержка завершается  >=1.25.0-0  —      cert-manager  —\n",
		"* рекомендуемая версия (новейшая активная). Установка — пока в консоли Cloud.ru.\n",
	})
	if strings.Contains(out, "Схема настроек v1.14.0") {
		t.Errorf("schema without --full:\n%s", out)
	}
	out, _, _ = execCLI(t, "k8s", "addon-versions", "cert-manager", "--full")
	assertHas(t, out, []string{"Схема настроек v1.14.0:\n{\n  \"type\": \"object\","})

	// Неизвестный плагин — код 4 и подсказка с поиском по каталогу.
	isolateHome(t)
	code, _, errOut = runCLI(t, "", "k8s", "addon-versions", "cert-manger")
	if code != clerrors.ExitNotFound {
		t.Fatalf("unknown addon: exit %d, want 4: %s", code, errOut)
	}
	assertHas(t, errOut, []string{`плагин "cert-manger" не найден`, "evo k8s addons <подстрока>", "Похожие: cert-manager."})
}

// Установки плагинов в v1.0 нет (BACKLOG [k8s-addons]): ни команды, ни
// инструмента.
func TestK8sAddAddon_Removed(t *testing.T) {
	_, _, code := execCLI(t, "k8s", "add-addon", "00000000-0000-0000-0000-0000000000e1", "cert-manager")
	if code == 0 {
		t.Error("evo k8s add-addon exists")
	}
	out, _, _ := execCLI(t, "k8s", "--help")
	if strings.Contains(out, "add-addon") {
		t.Errorf("k8s --help names add-addon:\n%s", out)
	}
}

// isolateHome keeps runCLI (the real run, stderr with hints) away from the
// profiles of the machine running the tests.
func isolateHome(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("CLOUDRU_CONFIG_DIR", dir)
	t.Setenv("CLOUDRU_KEY_ID", "")
	t.Setenv("CLOUDRU_SECRET", "")
}
