package mcp

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"
)

// vm_download пишет на ДИСК МАШИНЫ ОПЕРАТОРА то, что прислала ВМ. Каждая из
// границ ниже — место, где чужой путь или обрезанная передача превратились бы
// в файл не там или не тот.

func TestVMDownloadТребуетАргументы(t *testing.T) {
	s := newTestServer()
	dir := t.TempDir()
	случаи := []struct {
		имя  string
		args map[string]any
		хочу string
	}{
		{"без paths", map[string]any{"vm_id": "x", "out_dir": dir}, "'paths' is required"},
		{"без out_dir", map[string]any{"vm_id": "x", "paths": []any{"/var/lib/a.json"}}, "'out_dir' is required"},
		{"out_dir не каталог", map[string]any{"vm_id": "x", "paths": []any{"/a"}, "out_dir": dir + "/нет"}, "not an existing directory"},
		{"путь с ..", map[string]any{"vm_id": "x", "paths": []any{"/var/../etc/shadow"}, "out_dir": dir}, "contains '..'"},
		{"путь-каталог", map[string]any{"vm_id": "x", "paths": []any{"/var/lib/"}, "out_dir": dir}, "does not name a file"},
		{"перевод строки", map[string]any{"vm_id": "x", "paths": []any{"/a\n/b"}, "out_dir": dir}, "control character"},
		{"лимит выше потолка", map[string]any{"vm_id": "x", "paths": []any{"/a"}, "out_dir": dir, "max_bytes": float64(1 << 40)}, "max_bytes must be within"},
	}
	for _, c := range случаи {
		got := callTool(t, s.handleVMDownload, c.args)
		if got["status"] != "error" || !strings.Contains(got["error"].(string), c.хочу) {
			t.Errorf("%s: ожидалась ошибка с %q, получено %v", c.имя, c.хочу, got)
		}
	}
}

// Размер сверяется с тем, что ВМ назвала ДО передачи: обрезанный отчёт скана
// не должен сойти за полный, а «файл поменялся между двумя вызовами» — за
// тот файл, который просили.
func TestDecodeDownloadСверяетРазмер(t *testing.T) {
	тело := []byte(`{"Results":[]}`)
	b64 := base64.StdEncoding.EncodeToString(тело)

	if got, err := decodeDownload(b64+"\n", len(тело)); err != nil || string(got) != string(тело) {
		t.Fatalf("ровная передача не принята: %v", err)
	}
	if _, err := decodeDownload(b64, len(тело)+1); err == nil || !strings.Contains(err.Error(), "size mismatch") {
		t.Errorf("расхождение размера пропущено: %v", err)
	}
	if _, err := decodeDownload(b64[:len(b64)-3], len(тело)); err == nil {
		t.Error("обрезанный base64 принят")
	}
	if _, err := decodeDownload("это не base64!", 5); err == nil {
		t.Error("мусор вместо base64 принят")
	}
}

func TestCheckRemotePathНеПускаетОбход(t *testing.T) {
	for _, плохой := range []string{"", "/", "/var/lib/.", "/var/lib/..", "../x", "/a/../b", "/x\r", "/ok/"} {
		if err := checkRemotePath(плохой); err == nil {
			t.Errorf("путь %q принят", плохой)
		}
	}
	for _, хороший := range []string{"/var/lib/netbox-scan/os.json", "factory/out.log", "/a b/c.json"} {
		if err := checkRemotePath(хороший); err != nil {
			t.Errorf("путь %q отвергнут: %v", хороший, err)
		}
	}
}

// Локальное имя — только базовое имя удалённого файла. Удалённый путь
// выбирает тот, кто пишет скрипт на ВМ, а каталог назначения — оператор;
// первому нельзя дать возможность выбирать за второго.
func TestVMDownloadКладётПоБазовомуИмени(t *testing.T) {
	dir := t.TempDir()
	if _, err := os.Stat(dir); err != nil {
		t.Fatal(err)
	}
	// Проверяем инвариант напрямую: сборка локального пути не должна
	// содержать каталогов из удалённого.
	local := localNameFor(dir, "/var/lib/netbox-scan/ckz/os.json")
	if !strings.HasPrefix(local, dir+string(os.PathSeparator)) || strings.Contains(strings.TrimPrefix(local, dir), "netbox-scan") {
		t.Errorf("локальный путь %q ушёл за пределы каталога или унёс удалённые каталоги", local)
	}
}
