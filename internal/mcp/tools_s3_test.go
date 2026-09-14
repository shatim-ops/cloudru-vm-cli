package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/shatim-ops/cloudru-vm-cli/internal/s3"
)

// статКлиент подменяет единственное, что нужно защите от перезаписи, — ответ на
// вопрос «такой ключ уже есть?».
type статКлиент struct {
	obj    s3.Object
	exists bool
	err    error
}

func (c статКлиент) Stat(context.Context, string, string) (s3.Object, bool, error) {
	return c.obj, c.exists, c.err
}

// Запись НОВОГО ключа ничего не разрушает и подтверждения не требует — иначе
// каждый шаг публикации упирался бы в гейт, который всё равно подтверждает сам
// оркестратор.
func TestS3НовыйКлючПишетсяБезВопросов(t *testing.T) {
	blocked, _ := refuseOverwrite(context.Background(), статКлиент{}, map[string]any{}, "b", "k")
	if blocked {
		t.Fatal("запись нового ключа не должна блокироваться")
	}
}

// А замена существующего разрушает: партнёр уже держит подписанную ссылку, и
// под тем же sha256 в документе приёмки начнут отдаваться другие байты.
func TestS3ПерезаписьТребуетЯвногоФлага(t *testing.T) {
	cli := статКлиент{obj: s3.Object{Key: "k", Size: 42, LastModified: "2026-09-14T10:00:00Z"}, exists: true}

	blocked, res := refuseOverwrite(context.Background(), cli, map[string]any{}, "b", "k")
	if !blocked {
		t.Fatal("существующий ключ должен отвергаться без overwrite=true")
	}
	for _, want := range []string{"already exists", "overwrite=true", "42"} {
		if !strings.Contains(res, want) {
			t.Errorf("в отказе нет %q: %s", want, res)
		}
	}

	blocked, _ = refuseOverwrite(context.Background(), cli, map[string]any{"overwrite": true}, "b", "k")
	if blocked {
		t.Fatal("с overwrite=true замена должна проходить")
	}
}

// Сбой проверки существования — не повод писать вслепую: «не смогли узнать» и
// «ключа нет» это разные вещи.
func TestS3СбойПроверкиНеПревращаетсяВРазрешение(t *testing.T) {
	cli := статКлиент{err: errors.New("сеть отвалилась")}
	blocked, res := refuseOverwrite(context.Background(), cli, map[string]any{}, "b", "k")
	if !blocked {
		t.Fatal("при ошибке проверки писать нельзя")
	}
	if !strings.Contains(res, "сеть отвалилась") {
		t.Fatalf("причина потерялась: %s", res)
	}
}

func TestS3PutFileТребуетСуществующийФайл(t *testing.T) {
	s := newTestServer()
	got := callTool(t, s.handleS3PutFile, map[string]any{
		"bucket": "b", "key": "k", "path": t.TempDir() + "/нет-такого.raw",
	})
	if !strings.Contains(got["error"].(string), "cannot read") {
		t.Fatalf("ждали отказ по несуществующему файлу, получили %v", got)
	}
}

// Каталог вместо файла — обычная опечатка в пути к артефакту; сказать об этом
// надо словами, а не ошибкой S3 на середине заливки.
func TestS3PutFileОтвергаетКаталог(t *testing.T) {
	s := newTestServer()
	got := callTool(t, s.handleS3PutFile, map[string]any{
		"bucket": "b", "key": "k", "path": t.TempDir(),
	})
	if !strings.Contains(got["error"].(string), "directory") {
		t.Fatalf("ждали отказ по каталогу, получили %v", got)
	}
}

func TestS3ОбязательныеПоля(t *testing.T) {
	s := newTestServer()
	for _, c := range []struct {
		name string
		fn   func(context.Context, map[string]any) (string, bool)
		args map[string]any
	}{
		{"put_file без path", s.handleS3PutFile, map[string]any{"bucket": "b", "key": "k"}},
		{"presign без key", s.handleS3Presign, map[string]any{"bucket": "b"}},
	} {
		got := callTool(t, c.fn, c.args)
		if _, ok := got["error"]; !ok {
			t.Errorf("%s: ждали отказ, получили %v", c.name, got)
		}
	}
}
