package cli

import (
	stderrors "errors"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

func TestHTTPAuthToken(t *testing.T) {
	cases := []struct {
		addr, mode, env string
		wantToken       string
		wantCode        string // "" = no error
	}{
		{":8080", "bearer", "tok", "tok", ""},
		{"0.0.0.0:8080", "bearer", "tok", "tok", ""},
		{":8080", "bearer", "", "", clerrors.CodeConfigInvalid},
		{"127.0.0.1:8080", "none", "", "", ""},
		{"localhost:8080", "none", "", "", ""},
		{"[::1]:8080", "none", "", "", ""},
		{"127.0.0.1:8080", "none", "tok", "", ""}, // none ignores the token
		{":8080", "none", "", "", clerrors.CodeUsage},
		{"0.0.0.0:8080", "none", "", "", clerrors.CodeUsage},
		{"10.0.0.5:8080", "none", "", "", clerrors.CodeUsage},
		{"8080", "bearer", "tok", "", clerrors.CodeUsage},
		{":8080", "oauth", "tok", "", clerrors.CodeUsage},
	}
	for _, c := range cases {
		got, err := httpAuthToken(c.addr, c.mode, c.env)
		if c.wantCode == "" {
			if err != nil || got != c.wantToken {
				t.Errorf("httpAuthToken(%q, %q): %q, %v", c.addr, c.mode, got, err)
			}
			continue
		}
		var ce *clerrors.CLIError
		if !stderrors.As(err, &ce) || ce.Code != c.wantCode {
			t.Errorf("httpAuthToken(%q, %q): err %v, want code %s", c.addr, c.mode, err, c.wantCode)
		}
	}
}

// The server must refuse to start, not listen, on bad combinations.
func TestRun_MCPHTTPRefusesToStart(t *testing.T) {
	t.Setenv("MCP_BEARER_TOKEN", "")
	cases := []struct {
		args []string
		want int
	}{
		{[]string{"mcp", "--http", ":8080", "--auth", "none"}, clerrors.ExitGeneral},
		{[]string{"mcp", "--http", "127.0.0.1:0"}, clerrors.ExitAuth}, // bearer by default, no token
		{[]string{"mcp", "--auth", "none"}, clerrors.ExitGeneral},     // --auth without --http
	}
	for _, c := range cases {
		if got := run(c.args); got != c.want {
			t.Errorf("run(%v) = %d, want %d", c.args, got, c.want)
		}
	}
}

// --allow-project refuses every other project, the unset one included
// (QA P4); without the flag nothing is refused (D12: compatible default).
func TestCheckAllowedProject(t *testing.T) {
	if err := checkAllowedProject(nil, "p-any"); err != nil {
		t.Errorf("no allowlist: %v", err)
	}
	allowed := []string{"p-a", "p-b"}
	if err := checkAllowedProject(allowed, "p-b"); err != nil {
		t.Errorf("allowed project: %v", err)
	}
	for _, p := range []string{"p-c", ""} {
		err := checkAllowedProject(allowed, p)
		var ce *clerrors.CLIError
		if !stderrors.As(err, &ce) || ce.Code != clerrors.CodeConfigInvalid {
			t.Errorf("project %q: %v, want CONFIG_INVALID", p, err)
		}
	}
}
