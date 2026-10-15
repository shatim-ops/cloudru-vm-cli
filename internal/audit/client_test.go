package audit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
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
