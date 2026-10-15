package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// errResult carries the code and the hint of a CLIError in the chain, so
// the CLI keeps the exit code and the hint, and an agent sees them too.
func TestErrResult_ReasonAndHint(t *testing.T) {
	for _, tt := range []struct {
		err               error
		msg, reason, hint string
	}{
		{clierrors.Wrap(clierrors.CodeAPIUnreachable, "failed to reach IAM endpoint", "Check your network connection and try again", errors.New("dial")),
			"API_UNREACHABLE: failed to reach IAM endpoint", "API_UNREACHABLE", "Check your network connection and try again"},
		{fmt.Errorf("release floating IP: %w", clierrors.New(clierrors.CodeAuthFailed, "denied", "")),
			"release floating IP: AUTH_FAILED: denied", "AUTH_FAILED", ""},
		{errors.New("plain"), "plain", "", ""},
	} {
		var got map[string]any
		if err := json.Unmarshal([]byte(errResult(tt.err)), &got); err != nil {
			t.Fatal(err)
		}
		reason, _ := got["reason"].(string)
		hint, _ := got["hint"].(string)
		if got["status"] != "error" || got["error"] != tt.msg || reason != tt.reason || hint != tt.hint {
			t.Errorf("%v: %v", tt.err, got)
		}
		if _, ok := got["hint"]; !ok && tt.hint != "" || ok && tt.hint == "" {
			t.Errorf("%v: hint present = %v", tt.err, ok)
		}
	}
}
