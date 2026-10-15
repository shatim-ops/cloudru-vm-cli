package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/deploy"
)

// User SSH access used to be a manual post-install step ("append their
// public key to authorized_keys") that was routinely forgotten — VMs came up
// reachable only by the agent. deploy_apply now closes that loop itself:
// every public key listed in <state>/.evo/user_authorized_keys is
// appended (deduplicated) to ~/.ssh/authorized_keys on the new VM over the
// agent's own deploy key. The file is one authorized_keys-format line per key;
// '#' comments and blank lines are skipped. Missing/empty file = no-op, and
// any failure is reported in the deploy note rather than failing the deploy —
// the VM is already up, access repair must not destroy it.

const userKeysFile = "user_authorized_keys"

// legacyUserKeysFile is the file's name before v1.0; it is read when the new
// one is absent, until v1.1 (BACKLOG [10]).
const legacyUserKeysFile = "captain_authorized_keys"

// loadUserKeys returns the user public keys configured for auto-append and
// the name of the file they came from.
func loadUserKeys(stateDir string) ([]string, string) {
	dir := config.DataDir(stateDir)
	name := userKeysFile
	raw, err := os.ReadFile(filepath.Join(dir, name))
	if os.IsNotExist(err) {
		name = legacyUserKeysFile
		raw, err = os.ReadFile(filepath.Join(dir, name))
	}
	if err != nil {
		return nil, userKeysFile
	}
	var keys []string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Only authorized_keys-format PUBLIC keys. A pasted private key or
		// arbitrary text must never reach a VM's authorized_keys.
		if !isPublicKeyLine(line) {
			continue
		}
		keys = append(keys, line)
	}
	return keys, name
}

// isPublicKeyLine reports whether line looks like an authorized_keys entry.
func isPublicKeyLine(line string) bool {
	for _, prefix := range []string{"ssh-ed25519 ", "ssh-rsa ", "ecdsa-sha2-", "sk-ssh-ed25519@", "sk-ecdsa-sha2-"} {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

// appendUserKeys SSHes into the freshly created VM with the agent's deploy
// key and appends each user key to authorized_keys (idempotent). The VM's
// sshd may lag VM-active by a few seconds, so connection is retried briefly.
func appendUserKeys(ctx context.Context, host, login string, agentKeyPEM []byte, keys []string) error {
	if len(keys) == 0 {
		return nil
	}

	var script strings.Builder
	script.WriteString("mkdir -p ~/.ssh && chmod 700 ~/.ssh && touch ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys")
	for _, k := range keys {
		q := shellQuoteMCP(k)
		script.WriteString(fmt.Sprintf(" && { grep -qxF %s ~/.ssh/authorized_keys || echo %s >> ~/.ssh/authorized_keys; }", q, q))
	}

	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(10 * time.Second):
			}
		}
		d := deploy.NewSSHDeployer()
		if err := d.Connect(ctx, host, 22, login, agentKeyPEM, ""); err != nil {
			lastErr = err
			continue
		}
		_, err := d.Exec(ctx, script.String())
		_ = d.Close()
		if err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return lastErr
}

// shellQuoteMCP single-quotes s for safe interpolation into a shell command.
func shellQuoteMCP(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
