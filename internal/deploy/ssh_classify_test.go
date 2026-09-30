package deploy

import (
	"fmt"
	"testing"

	clerrors "github.com/shatim-ops/cloudru-vm-cli/internal/errors"
)

// A banner or protocol failure is not an authentication failure. x/crypto/ssh
// prefixes both with "handshake failed", and the wrong label once sent a build
// operator to check keys and logins while the VM was simply not done booting.
func TestClassifyDialError(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"ssh: handshake failed: ssh: unable to authenticate, attempted methods [none publickey], no supported methods remain", clerrors.CodeSSHAuthFailed},
		{"ssh: handshake failed: EOF", clerrors.CodeSSHHandshakeFailed},
		{"ssh: handshake failed: read tcp 10.0.0.1:41000->10.0.0.2:22: i/o timeout", clerrors.CodeSSHHandshakeFailed},
		{"dial tcp 10.0.0.2:22: connect: connection refused", clerrors.CodeSSHTimeout},
	}
	for _, c := range cases {
		got := classifyDialError(fmt.Errorf("%s", c.in))
		if got.Code != c.want {
			t.Errorf("%q:\n  got  %s\n  want %s", c.in, got.Code, c.want)
		}
	}
}
