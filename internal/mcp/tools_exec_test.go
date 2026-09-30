package mcp

import (
	"strings"
	"testing"
)

// vm_exec runs code the caller wrote, on someone's machine. Everything that
// stands between a call and that happening is tested here by breaking it.

func TestVMExecRequiresConfirmation(t *testing.T) {
	s := newTestServer()
	got := callTool(t, s.handleVMExec, map[string]any{
		"vm_id": "11111111-1111-1111-1111-111111111111", "script": "echo привет",
	})
	if got["status"] != "confirmation_required" {
		t.Fatalf("скрипт запущен без подтверждения: %v", got)
	}
	// Оператор утверждает конкретный код, значит он должен его видеть.
	will, _ := got["will_run"].(map[string]any)
	if will == nil || will["script_head"] == "" {
		t.Errorf("в предпросмотре нет самого скрипта: %v", got)
	}
}

func TestVMUploadRequiresConfirmation(t *testing.T) {
	s := newTestServer()
	got := callTool(t, s.handleVMUpload, map[string]any{
		"vm_id": "11111111-1111-1111-1111-111111111111",
		"files": []map[string]any{{"path": "build.sh", "content": "echo x"}},
	})
	if got["status"] != "confirmation_required" {
		t.Fatalf("файлы записаны без подтверждения: %v", got)
	}
}

// Проверка аргументов идёт до подтверждения и до облака: бессмысленный вызов
// не должен доходить ни до того, ни до другого.
func TestVMExecОтвергаетМусор(t *testing.T) {
	s := newTestServer()
	случаи := map[string]map[string]any{
		"пустой скрипт":    {"vm_id": "x", "script": "   "},
		"без скрипта":      {"vm_id": "x"},
		"таймаут ноль":     {"vm_id": "x", "script": "echo", "timeout_sec": 0},
		"таймаут огромный": {"vm_id": "x", "script": "echo", "timeout_sec": 99999},
	}
	for имя, args := range случаи {
		t.Run(имя, func(t *testing.T) {
			if got := callTool(t, s.handleVMExec, args); got["status"] == "confirmation_required" {
				t.Errorf("мусорный вызов дошёл до подтверждения: %v", got)
			}
		})
	}
}

// run_id становится путём на ВМ: всё, что похоже на выход из каталога, надо
// отвергать до подключения.
func TestVMExecStatusОтвергаетПуть(t *testing.T) {
	s := newTestServer()
	for _, id := range []string{"", "../../etc/passwd", "run 1", "run'1", "a/b", "run\n1"} {
		t.Run(id, func(t *testing.T) {
			got := callTool(t, s.handleVMExecStatus, map[string]any{"vm_id": "x", "run_id": id})
			if got["status"] == "running" || got["status"] == "done" {
				t.Errorf("run_id %q принят: %v", id, got)
			}
		})
	}
}

// Загрузка кладёт файлы в домашний каталог. Абсолютный путь и «..» увели бы их
// куда угодно, а привилегия на запись в /etc должна быть явной.
func TestVMUploadОтвергаетПути(t *testing.T) {
	s := newTestServer()
	плохие := []string{"/etc/passwd", "../../root/.ssh/authorized_keys", "a/../../b", ""}
	for _, p := range плохие {
		t.Run(p, func(t *testing.T) {
			got := callTool(t, s.handleVMUpload, map[string]any{
				"vm_id": "x", "files": []map[string]any{{"path": p, "content": "x"}},
			})
			if got["status"] == "confirmation_required" {
				t.Errorf("путь %q дошёл до подтверждения: %v", p, got)
			}
		})
	}
	// Пустой список файлов — тоже бессмыслица.
	if got := callTool(t, s.handleVMUpload, map[string]any{"vm_id": "x", "files": []map[string]any{}}); got["status"] == "confirmation_required" {
		t.Error("пустой список файлов принят")
	}
}

// Разбор состояния решает, считать ли сборку законченной. Ошибка здесь это
// либо вечный опрос, либо «успех» на середине сборки.
func TestРазборСостоянияЗапуска(t *testing.T) {
	маркер := "---CLOUDRU-EXEC---"
	случаи := []struct {
		имя       string
		вывод     string
		done      bool
		код       int
		хвостЕсть bool
	}{
		{"ещё идёт", маркер + "\n=== 2. пакеты ===\n", false, -1, true},
		{"успех", "0\n" + маркер + "\n=== СБОРКА ЗАВЕРШЕНА ===\n", true, 0, true},
		{"провал", "1\n" + маркер + "\nПРОДУКТ НЕ ОТВЕЧАЕТ\n", true, 1, true},
		{"код не ноль", "137\n" + маркер + "\nубит\n", true, 137, true},
		{"файла кода нет и лога нет", маркер + "\n", false, -1, false},
		{"мусор вместо кода", "не число\n" + маркер + "\nлог\n", true, -1, true},
	}
	for _, с := range случаи {
		t.Run(с.имя, func(t *testing.T) {
			st := parseRunOutput("run-1", с.вывод)
			if st.Done != с.done {
				t.Errorf("Done = %v, хочу %v", st.Done, с.done)
			}
			if st.Done && st.ExitCode != с.код {
				t.Errorf("код = %d, хочу %d", st.ExitCode, с.код)
			}
			if (st.Tail != "") != с.хвостЕсть {
				t.Errorf("хвост = %q", st.Tail)
			}
			if !st.Done && st.Status != "running" {
				t.Errorf("статус = %q при незавершённом запуске", st.Status)
			}
		})
	}
}

// Предпросмотр показывает начало скрипта, но не весь: сборочный скрипт на
// восемь килобайт в окне подтверждения нечитаем.
func TestОбрезкаПредпросмотра(t *testing.T) {
	длинный := strings.Repeat("строка\n", 100)
	got := headLines(длинный, 20)
	if strings.Count(got, "\n") > 22 {
		t.Errorf("предпросмотр не обрезан: %d строк", strings.Count(got, "\n"))
	}
	if !strings.Contains(got, "more lines") {
		t.Error("не сказано, что показано не всё")
	}
	// Короткий скрипт показывается целиком и без приписки.
	if got := headLines("одна\nдве\n", 20); strings.Contains(got, "more lines") {
		t.Errorf("короткий скрипт обрезан: %q", got)
	}
}

// Новые тулы обязаны быть объявлены: обработчик без объявления вызвать нельзя.
func TestНовыеТулыОбъявлены(t *testing.T) {
	s := newTestServer()
	есть := map[string]bool{}
	for _, tl := range s.ListTools() {
		есть[tl.Name] = true
	}
	for _, имя := range []string{"vm_exec", "vm_exec_status", "vm_upload", "vm_download"} {
		if !есть[имя] {
			t.Errorf("тул %s не объявлен", имя)
		}
		if _, ок := s.handlers[имя]; !ок {
			t.Errorf("у тула %s нет обработчика", имя)
		}
	}
}
