package vpc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// The client repeats a read the server answered 503, and a write it refused
// with 429 (BACKLOG [02]: only compute used to retry).
func TestClientRetriesTransientAnswers(t *testing.T) {
	var gets, posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if gets.Add(1) == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			_, _ = w.Write([]byte(`{"vpcs":[{"id":"v1"}]}`))
		default:
			posts.Add(1)
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer srv.Close()
	c := New("tok", "p1", srv.URL)
	if _, err := c.ListVPCs(context.Background()); err != nil {
		t.Fatalf("ListVPCs after one 503: %v", err)
	}
	if gets.Load() != 2 {
		t.Fatalf("GET calls = %d, want 2", gets.Load())
	}
	_, _, _ = c.do(context.Background(), http.MethodPost, "/x", nil, map[string]string{"a": "b"})
	if posts.Load() != 1 {
		t.Fatalf("POST answered 503 was repeated: %d calls", posts.Load())
	}
}
