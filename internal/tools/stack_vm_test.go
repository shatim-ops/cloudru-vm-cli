package tools

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/stack"
)

// Шаг vm стека: provision ответил ошибкой, но ВМ уже создана (error_creating,
// таймаут ожидания — ответ называет vm_id) или ответ потерян (ВМ видна в
// vm_list по имени). Раньше шаг повторял provision до 4 раз — до четырёх
// платных ВМ, которых stack_destroy не видит (матрица команд, 16.10.2026).
func TestStackVMStep_FailedProvisionIsNotRepeated(t *testing.T) {
	for _, tc := range []struct {
		name   string
		answer map[string]any // ответ-ошибка provision
		listed []any          // что видит vm_list
		wantID string
	}{
		{"ответ называет ВМ", map[string]any{"status": "error", "vm_id": "vm-1", "error": "VM vm-1 entered state error_creating"}, []any{}, "vm-1"},
		{"ответ потерян, ВМ видна по имени", map[string]any{"status": "error", "error": "API_UNREACHABLE: connection reset"}, []any{map[string]any{"id": "vm-9", "name": "web"}}, "vm-9"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stackHome(t)
			r := newTestServer()
			j := planJob(t, `{"name":"s","vm":{"name":"web"}}`)
			var creates atomic.Int32
			r.handlers["provision"] = func(context.Context, map[string]any) (string, bool) {
				creates.Add(1)
				return toJSON(tc.answer), true
			}
			r.handlers["vm_list"] = fakeOK(map[string]any{"vms": tc.listed})
			r.handleStackApply(context.Background(), map[string]any{"job_id": j.ID, "confirmed": true})
			for range 4 {
				r.handleStackStatus(context.Background(), map[string]any{"job_id": j.ID, "wait_seconds": float64(1)})
			}
			if n := creates.Load(); n != 1 {
				t.Fatalf("provision вызван %d раз, want 1 (лишние платные ВМ)", n)
			}
			loaded, err := stack.LoadJob(j.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got := loaded.Steps[0].ResourceID; got != tc.wantID {
				t.Fatalf("ResourceID шага vm = %q, want %q: stack_destroy не удалит ВМ", got, tc.wantID)
			}
		})
	}
}
