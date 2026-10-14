package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	clerrors "github.com/shatim-ops/cloudru-vm-cli/internal/errors"
)

func TestEnvVarProvider_RejectedCredentialsAreAuthError(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "denied", status)
		}))

		oldEndpoint := IAMEndpoint
		IAMEndpoint = srv.URL
		t.Setenv("CLOUDRU_KEY_ID", "k")
		t.Setenv("CLOUDRU_SECRET", "s")

		_, err := NewEnvVarProvider(srv.Client()).Token(context.Background())
		IAMEndpoint = oldEndpoint
		srv.Close()

		if got := clerrors.ExitCode(err); got != clerrors.ExitAuth {
			t.Errorf("status %d: exit code = %d (err %v), want %d", status, got, err, clerrors.ExitAuth)
		}
	}
}

func TestNewEnvVarProvider_DefaultClientHasTimeout(t *testing.T) {
	p := NewEnvVarProvider(nil)
	if p.client.Timeout == 0 {
		t.Error("default IAM client has no timeout")
	}
}
