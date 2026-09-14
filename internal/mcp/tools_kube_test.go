package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeHelm ставит в PATH подставной helm, который печатает свои аргументы по
// одному на строку. Проверки ниже смотрят именно на аргументы: вся цена ошибки
// в этом хендлере — собранная команда, а не разбор ответа.
func fakeHelm(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	// printf, а не echo: `echo "-n"` съедает аргумент, приняв его за свой флаг,
	// и проверка «флаг доехал» ложно падает.
	script := "#!/usr/bin/env bash\nprintf '%s\\n' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "helm"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
}

// fakeChart — каталог, который проходит проверку chart_path.
func fakeChart(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Chart.yaml"), []byte("name: x\nversion: 1.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestHelmНеизвестноеДействиеОтвергается(t *testing.T) {
	s := newTestServer()
	got := callTool(t, s.handleK8sHelm, map[string]any{"cluster_id": "c-1", "action": "rollback"})
	if !strings.Contains(got["error"].(string), "unknown action") {
		t.Fatalf("ждали отказ по неизвестному действию, получили %v", got)
	}
}

// lint и template не трогают кластер, и требовать для них cluster_id значило бы
// заставить фабрику поднимать кластер, чтобы проверить чарт.
func TestHelmЛокальныеДействияБезКластера(t *testing.T) {
	fakeHelm(t)
	s := newTestServer()
	chart := fakeChart(t)

	for _, action := range []string{"lint", "template"} {
		got := callTool(t, s.handleK8sHelm, map[string]any{"action": action, "chart_path": chart})
		if got["status"] != "ok" {
			t.Fatalf("%s без кластера должен отработать, получили %v", action, got)
		}
	}
}

// Всё остальное без кластера выполнить нельзя, и сказать об этом надо до того,
// как хендлер полезет за kubeconfig.
func TestHelmОстальныеДействияТребуютКластер(t *testing.T) {
	s := newTestServer()
	for _, action := range []string{"install", "status", "list", "test"} {
		got := callTool(t, s.handleK8sHelm, map[string]any{"action": action, "release": "r", "chart": "repo/c"})
		msg, _ := got["error"].(string)
		if !strings.Contains(msg, "cluster_id is required") {
			t.Fatalf("%s без cluster_id: ждали отказ, получили %v", action, got)
		}
	}
}

// helm test запускает под, описанный ЧУЖИМ чартом, в нашем кластере — это
// исполнение чужого кода, а не чтение. Гейт обязан сработать до облака.
func TestHelmTestПодГейтом(t *testing.T) {
	s := newTestServer()
	got := callTool(t, s.handleK8sHelm, map[string]any{
		"cluster_id": "c-1", "action": "test", "release": "r",
	})
	if got["status"] != "confirmation_required" {
		t.Fatalf("без confirmed ждали запрос подтверждения, получили %v", got)
	}
	if !strings.Contains(got["message"].(string), "chart author") {
		t.Fatalf("в запросе подтверждения не сказано, чей код исполняется: %v", got)
	}
}

func TestHelmМутацииПодГейтом(t *testing.T) {
	s := newTestServer()
	for _, action := range []string{"install", "upgrade", "uninstall", "delete"} {
		got := callTool(t, s.handleK8sHelm, map[string]any{
			"cluster_id": "c-1", "action": action, "release": "r", "chart": "repo/c",
		})
		if got["status"] != "confirmation_required" {
			t.Fatalf("%s без confirmed: ждали подтверждение, получили %v", action, got)
		}
	}
}

// Чтения кластера гейта не требуют — но и до облака они дойти в тестах не
// должны, поэтому смотрим только на то, что подтверждения не запросили.
func TestHelmЧтенияБезГейта(t *testing.T) {
	s := newTestServer()
	for _, action := range []string{"list", "status"} {
		got := callTool(t, s.handleK8sHelm, map[string]any{
			"cluster_id": "c-1", "action": action, "release": "r",
		})
		if got["status"] == "confirmation_required" {
			t.Fatalf("%s не меняет кластер, подтверждение лишнее: %v", action, got)
		}
	}
}

func TestHelmИсточникЧартаОдин(t *testing.T) {
	s := newTestServer()
	got := callTool(t, s.handleK8sHelm, map[string]any{
		"action": "template", "chart": "repo/c", "chart_path": fakeChart(t),
	})
	if !strings.Contains(got["error"].(string), "not both") {
		t.Fatalf("два источника чарта должны отвергаться, получили %v", got)
	}
}

func TestHelmНесуществующийЧартПуть(t *testing.T) {
	s := newTestServer()
	got := callTool(t, s.handleK8sHelm, map[string]any{
		"action": "template", "chart_path": filepath.Join(t.TempDir(), "нет-такого"),
	})
	if !strings.Contains(got["error"].(string), "chart_path") {
		t.Fatalf("ждали отказ по chart_path, получили %v", got)
	}
}

// Файл, который не чарт и не .tgz, — почти наверняка опечатка в пути; принять
// его значит получить невнятную ошибку helm вместо внятной своей.
func TestHelmЧартПутьНеАрхивИНеКаталог(t *testing.T) {
	s := newTestServer()
	f := filepath.Join(t.TempDir(), "values.yaml")
	if err := os.WriteFile(f, []byte("a: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := callTool(t, s.handleK8sHelm, map[string]any{"action": "lint", "chart_path": f})
	if !strings.Contains(got["error"].(string), "neither a chart directory") {
		t.Fatalf("ждали отказ по типу chart_path, получили %v", got)
	}
}

// Главная проверка сборки команды: значения, --set и версия ядра обязаны
// доехать до helm. Порядок -f важен: файл с диска раньше inline, чтобы inline
// перекрывал файл, а не наоборот.
func TestHelmTemplateПередаётЗначенияИВерсиюЯдра(t *testing.T) {
	fakeHelm(t)
	s := newTestServer()
	chart := fakeChart(t)
	vf := filepath.Join(t.TempDir(), "prod.yaml")
	if err := os.WriteFile(vf, []byte("replicaCount: 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := callTool(t, s.handleK8sHelm, map[string]any{
		"action": "template", "chart_path": chart, "release": "qdrant",
		"values_file": vf, "values": "baseRegistry: probe\n",
		"set":          []any{"image.tag=1.19.1", "ingress.enabled=false"},
		"kube_version": "1.31.0", "namespace": "qdrant",
	})
	if got["status"] != "ok" {
		t.Fatalf("template не отработал: %v", got)
	}
	out := got["output"].(string)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	joined := strings.Join(lines, " ")

	for _, want := range []string{"template", "qdrant", chart, "--kube-version", "1.31.0", "-n", "--set", "image.tag=1.19.1", "ingress.enabled=false"} {
		if !strings.Contains(joined, want) {
			t.Errorf("в команде нет %q: %s", want, joined)
		}
	}
	// Файл с диска идёт первым из двух -f.
	var files []string
	for i, l := range lines {
		if l == "-f" && i+1 < len(lines) {
			files = append(files, lines[i+1])
		}
	}
	if len(files) != 2 {
		t.Fatalf("ждали два -f (файл и inline), получили %v", files)
	}
	if files[0] != vf {
		t.Errorf("values_file должен идти первым, чтобы inline перекрывал его: %v", files)
	}
	if data, err := os.ReadFile(files[1]); err == nil && !strings.Contains(string(data), "baseRegistry: probe") {
		t.Errorf("inline-значения не доехали до файла: %q", data)
	}
}

// Молчаливая потеря значений — самый дорогой отказ этого хендлера: helm
// отработает на дефолтах чарта и отрапортует успех. Прошлая версия глотала
// ошибку создания временного файла; несуществующий values_file обязан быть
// отказом, а не пропуском.
func TestHelmНесуществующийValuesFileЭтоОтказ(t *testing.T) {
	fakeHelm(t)
	s := newTestServer()
	got := callTool(t, s.handleK8sHelm, map[string]any{
		"action": "lint", "chart_path": fakeChart(t),
		"values_file": filepath.Join(t.TempDir(), "нет.yaml"),
	})
	if got["status"] == "ok" {
		t.Fatalf("ждали отказ по values_file, а не тихий прогон на дефолтах: %v", got)
	}
	if !strings.Contains(got["error"].(string), "values_file") {
		t.Fatalf("в ошибке не назван values_file: %v", got)
	}
}

// Временные файлы значений не должны копиться на сборочной ВМ.
func TestHelmВременныеЗначенияУбираются(t *testing.T) {
	flags, cleanup, err := helmValueArgs(map[string]any{"values": "a: 1\n"})
	if err != nil {
		t.Fatal(err)
	}
	if len(flags) != 2 {
		t.Fatalf("ждали -f и путь, получили %v", flags)
	}
	path := flags[1]
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("временный файл не создан: %v", err)
	}
	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("временный файл %s остался после cleanup", path)
	}
}

// helm test обязан звать --logs: без него причина провала остаётся в удалённом
// поде, и вендор получает «под упал» без единого объяснения. Ради этой и
// соседних проверок сборка команды вынесена в чистую функцию: до кластера они
// не доходят, а ловят ровно то, что определяет поведение тула.
func TestHelmTestПроситЛоги(t *testing.T) {
	h, _, err := helmCommand("test", "", map[string]any{"release": "qdrant", "namespace": "qdrant"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(h, " ")
	if !strings.Contains(joined, "--logs") {
		t.Fatalf("helm test без --logs: %s", joined)
	}
	if !strings.HasPrefix(joined, "test qdrant") {
		t.Fatalf("не тот релиз: %s", joined)
	}
}

func TestHelmКомандаПоДействиям(t *testing.T) {
	chart := "/tmp/qdrant"
	base := map[string]any{"release": "qdrant", "namespace": "qdrant"}

	cases := []struct {
		action string
		want   []string
		absent []string
	}{
		{"install", []string{"upgrade", "--install", "qdrant", chart, "--create-namespace", "--wait", "--timeout"}, nil},
		{"uninstall", []string{"uninstall", "qdrant", "--wait"}, []string{"--install"}},
		{"status", []string{"status", "qdrant", "-n", "qdrant"}, []string{"--wait"}},
		{"list", []string{"list", "-A"}, []string{"qdrant"}},
		{"lint", []string{"lint", chart}, []string{"--wait", "--install"}},
		{"template", []string{"template", "qdrant", chart}, []string{"--wait", "--install"}},
	}
	for _, c := range cases {
		h, _, err := helmCommand(c.action, chart, base, nil, nil)
		if err != nil {
			t.Fatalf("%s: %v", c.action, err)
		}
		joined := strings.Join(h, " ")
		for _, w := range c.want {
			if !strings.Contains(joined, w) {
				t.Errorf("%s: в команде нет %q: %s", c.action, w, joined)
			}
		}
		for _, a := range c.absent {
			if strings.Contains(joined, a) {
				t.Errorf("%s: в команде лишнее %q: %s", c.action, a, joined)
			}
		}
	}
}

// Обязательные поля называются по действию, а не общим списком: «нужен release»
// на template было бы неправдой.
func TestHelmОбязательныеПоляПоДействиям(t *testing.T) {
	for _, c := range []struct{ action, chart, want string }{
		{"install", "", "need release and chart"},
		{"test", "", "test needs release"},
		{"status", "", "status needs release"},
		{"lint", "", "lint needs chart"},
	} {
		args := map[string]any{}
		if c.action == "install" {
			args["release"] = "r" // релиз есть, чарта нет
		}
		_, _, err := helmCommand(c.action, c.chart, args, nil, nil)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: ждали %q, получили %v", c.action, c.want, err)
		}
	}
}

// wait=false нужен, когда за готовностью следит вызывающий (inspector сам ждёт
// поды и собирает улики); молча навязанный --wait скрыл бы от него причину.
func TestHelmWaitВыключаем(t *testing.T) {
	h, _, err := helmCommand("install", "c", map[string]any{"release": "r", "wait": false}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(h, " "), "--wait") {
		t.Fatalf("wait=false проигнорирован: %v", h)
	}
}

func TestHelmТаймаутПередаётся(t *testing.T) {
	h, outer, err := helmCommand("install", "c", map[string]any{"release": "r", "timeout_seconds": 600}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(h, " "), "600s") {
		t.Fatalf("timeout_seconds не доехал до helm: %v", h)
	}
	// Внешний дедлайн выше собственного, иначе процесс убьём мы, и helm не
	// успеет сказать, на чём именно встал.
	if outer.Seconds() <= 600 {
		t.Fatalf("внешний таймаут %s не выше helm-овского 600s", outer)
	}
}
