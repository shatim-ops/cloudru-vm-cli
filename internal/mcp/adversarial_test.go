package mcp

import (
	"strings"
	"testing"
)

// Гейт подтверждений — граница, за которой действие происходит в чужой
// инфраструктуре и стоит денег. Здесь проверяется, что её нельзя обойти
// подбором аргументов.

// kubectl разрешён без подтверждения по ПЕРВОМУ слову. Но у нескольких
// разрешённых глаголов есть подкоманды, которые меняют кластер: `rollout
// restart` перезапускает поды продукта, `rollout undo` откатывает релиз,
// `auth reconcile` переписывает RBAC. Пройти гейтом под видом чтения они не
// должны.
func TestГейтНеОбходитсяПодкомандойKubectl(t *testing.T) {
	s := newTestServer()
	мутирующие := []string{
		"rollout restart deployment/qdrant",
		"rollout undo deployment/qdrant",
		"rollout pause deployment/qdrant",
		"rollout resume deployment/qdrant",
		"auth reconcile -f rbac.yaml",
		"config set-credentials злоумышленник --token=x",
	}
	for _, args := range мутирующие {
		got := callTool(t, s.handleK8sKubectl, map[string]any{"cluster_id": "c-1", "args": args})
		if got["status"] != "confirmation_required" {
			t.Errorf("%q прошло без подтверждения: %v", args, got)
		}
	}
}

// А настоящие чтения подтверждения требовать не должны, иначе гейт превратится
// в шум и его начнут подтверждать не глядя.
func TestЧтенияKubectlОстаютсяБезГейта(t *testing.T) {
	s := newTestServer()
	чтения := []string{
		"get pods -A",
		"describe pod qdrant-0",
		"logs deploy/qdrant",
		"rollout status deployment/qdrant",
		"rollout history deployment/qdrant",
		"auth can-i create pods",
		"top nodes",
		"events",
	}
	for _, args := range чтения {
		got := callTool(t, s.handleK8sKubectl, map[string]any{"cluster_id": "c-1", "args": args})
		if got["status"] == "confirmation_required" {
			t.Errorf("%q — чтение, подтверждение лишнее: %v", args, got)
		}
	}
}

// Аргумент, начинающийся с дефиса, kubectl примет за флаг: гейт одобрил одно,
// а исполнится другое. Тот же приём подменяет kubeconfig на чужой.
func TestФлагиВместоАргументовKubectl(t *testing.T) {
	s := newTestServer()
	for _, args := range []string{
		"--kubeconfig=/tmp/чужой get pods",
		"-n kube-system delete ns qdrant",
		"--help",
	} {
		got := callTool(t, s.handleK8sKubectl, map[string]any{"cluster_id": "c-1", "args": args})
		if !отказПроверки(got) {
			t.Errorf("%q прошло свободно: %v", args, got)
		}
	}
}

// Имя релиза и пространство имён уходят в argv helm. Значение, начинающееся с
// дефиса, helm разберёт как флаг: подтверждали установку, а получили другое
// действие.
func TestФлагиВместоИмёнHelm(t *testing.T) {
	s := newTestServer()
	случаи := []map[string]any{
		{"cluster_id": "c-1", "action": "install", "release": "--dry-run", "chart": "r/c", "confirmed": true},
		{"cluster_id": "c-1", "action": "install", "release": "r", "chart": "--set=evil", "confirmed": true},
		{"cluster_id": "c-1", "action": "install", "release": "r", "chart": "r/c", "namespace": "--all", "confirmed": true},
		{"cluster_id": "c-1", "action": "test", "release": "-n kube-system", "confirmed": true},
		{"cluster_id": "c-1", "action": "status", "release": "--help"},
	}
	for _, args := range случаи {
		got := callTool(t, s.handleK8sHelm, args)
		if !отказПроверки(got) {
			t.Errorf("%v прошло без отказа проверки: %v", args, got)
		}
	}
}

// values_file и chart_path — пути на диске, которые тоже уходят в argv.
func TestПутиНачинающиесяСДефисаОтвергаются(t *testing.T) {
	s := newTestServer()
	for _, args := range []map[string]any{
		{"action": "lint", "chart_path": "-rf"},
		{"action": "lint", "chart_path": ".", "values_file": "--set=evil"},
	} {
		got := callTool(t, s.handleK8sHelm, args)
		if !отказПроверки(got) {
			t.Errorf("%v прошло без отказа проверки: %v", args, got)
		}
	}
}

// Срок подписанной ссылки: отрицательный и нулевой не должны молча
// превращаться в максимум. Ссылка, живущая дольше ожидаемого, — это доступ к
// продукту, который считали закрытым.
func TestСрокПодписаннойСсылки(t *testing.T) {
	s := newTestServer()
	for _, secs := range []any{-1, 0, 604801, 999999999} {
		got := callTool(t, s.handleS3Presign, map[string]any{
			"bucket": "b", "key": "k", "expires_seconds": secs,
		})
		if !отказПроверки(got) {
			t.Errorf("срок %v принят: %v", secs, got)
		}
	}
}

// Ключ объекта с «..» или ведущим слэшем — попытка выйти за префикс продукта в
// бакете, куда складываются артефакты всех продуктов.
func TestКлючОбъектаБезПобега(t *testing.T) {
	s := newTestServer()
	for _, key := range []string{"../чужой/образ.raw", "/abs/путь", "a/../../b"} {
		got := callTool(t, s.handleS3PutFile, map[string]any{
			"bucket": "b", "key": key, "path": "/etc/hostname",
		})
		if !отказПроверки(got) {
			t.Errorf("ключ %q принят: %v", key, got)
		}
	}
}

// отказПроверки отличает «мы отвергли ввод» от «мы дошли до облака и там
// не получилось». Без этого различия проверки маскируются отказом подставного
// движка, и тест зеленеет при полном отсутствии валидации.
func отказПроверки(ответ map[string]any) bool {
	текст, _ := ответ["error"].(string)
	if текст == "" {
		return false
	}
	for _, чужое := range []string{"no engine in tests", "credentials not configured", "CLOUDRU_"} {
		if strings.Contains(текст, чужое) {
			return false
		}
	}
	return true
}
