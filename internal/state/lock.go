package state

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/shatim-ops/cloudru-vm-cli/internal/config"
	clierr "github.com/shatim-ops/cloudru-vm-cli/internal/errors"
)

// errLocked is what lockFile returns when another holder has the lock.
var errLocked = errors.New("locked")

// Lock takes the exclusive lock of the state file at path for one command
// that changes cloud resources (deploy, destroy). Each of them reads the
// state at the start and writes it back minutes later; two at once in one
// directory would create two VMs, or the last writer would drop the other's
// record — a VM billed and forgotten. The lock is an OS file lock: it goes
// away with the process, so a crash or kill -9 leaves nothing stale.
func Lock(path string) (unlock func(), err error) {
	dir := filepath.Dir(path)
	if err := config.MkdirPrivate(dir); err != nil { // .evo/ also holds the SSH key
		return nil, clierr.Wrap(clierr.CodeStateCorrupted, "failed to create state directory",
			"Check write permissions for "+dir, err)
	}
	lockPath := filepath.Join(dir, "state.lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, clierr.Wrap(clierr.CodeStateCorrupted, "failed to open the state lock",
			"Check write permissions for "+dir, err)
	}
	if err := lockFile(f); err != nil {
		_ = f.Close()
		if errors.Is(err, errLocked) {
			return nil, clierr.New(clierr.CodeStateLocked,
				"another evo deploy or destroy is running in this directory",
				"Wait for it to finish; the lock ("+lockPath+") is released when that process exits.")
		}
		return nil, clierr.Wrap(clierr.CodeStateCorrupted, "failed to lock the state", "Check "+lockPath, err)
	}
	return func() {
		_ = unlockFile(f)
		_ = f.Close()
	}, nil
}
