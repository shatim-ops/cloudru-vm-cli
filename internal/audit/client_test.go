package audit

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// The spec's answer is {logs:[…]}; evo read only "events" and reported no
// events at all (api-contract §4.2). Both are read.
func TestList_ReadsLogsAndEvents(t *testing.T) {
	for name, body := range map[string]string{
		"logs":   `{"logs":[{"id":"a"},{"id":"b"}],"nextPageToken":"n"}`,
		"events": `{"events":[{"id":"a"},{"id":"b"}]}`,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		c := New("tok", "p", srv.URL)
		page, err := c.List(context.Background(), ListOptions{})
		srv.Close()
		if err != nil || len(page.Events) != 2 {
			t.Errorf("%s: %d events, %v", name, len(page.Events), err)
		}
	}
}

// Размер страницы — limit (спецификация): pageSize живой API отвергает 415
// ещё до проверки доступа (реестр AUD-2). Больше 500 не отправляется.
func TestList_SendsLimit(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"logs":[]}`))
	}))
	defer srv.Close()
	if _, err := New("tok", "p", srv.URL).List(context.Background(), ListOptions{PageSize: 1000}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "limit=500") || strings.Contains(got, "pageSize") {
		t.Errorf("query %q, want limit=500 and no pageSize", got)
	}
}

// 401 при действующем токене — аудит не включён (обращение в поддержку,
// документация «Получить аудит-логи»), а не AUTH_FAILED.
func TestList_401IsAuditNotEnabled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":16,"message":"unauthenticated"}`))
	}))
	defer srv.Close()
	_, err := New("tok", "p", srv.URL).List(context.Background(), ListOptions{})
	var ce *clierrors.CLIError
	if !errors.As(err, &ce) || ce.Code != clierrors.CodeAPIError || !strings.Contains(err.Error(), "401") || !strings.Contains(ce.Hint, "Cloud.ru") {
		t.Errorf("error = %#v", err)
	}
}
