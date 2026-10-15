package state

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	clierr "github.com/shatim-ops/evo-cli/internal/errors"
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

// errStateLocked matches Lock's "held by another process" error (CLIError.Is
// compares codes).
var errStateLocked = clierr.New(clierr.CodeStateLocked, "", "")

// lockRetryEvery is how often LockWait retries a held lock.
var lockRetryEvery = 500 * time.Millisecond

// LockWait is Lock for a short write that must not be dropped: the record of
// a VM that already exists. It waits up to maxWait for a deploy or destroy
// in the same directory to finish instead of failing at once, then returns
// Lock's error. It ignores the caller's context on purpose: a cancelled MCP
// call still has to record the VM it created.
func LockWait(path string, maxWait time.Duration) (unlock func(), err error) {
	deadline := time.Now().Add(maxWait)
	for {
		unlock, err = Lock(path)
		if err == nil || !errors.Is(err, errStateLocked) || !time.Now().Before(deadline) {
			return unlock, err
		}
		time.Sleep(lockRetryEvery)
	}
}

// Update applies change to the state file at path under the lock: read,
// change, write — so a concurrent deploy or destroy in the same directory
// cannot drop the change, or have its own dropped.
func Update(path string, maxWait time.Duration, change func(*File) error) error {
	unlock, err := LockWait(path, maxWait)
	if err != nil {
		return err
	}
	defer unlock()
	sf, err := Read(path)
	if err != nil {
		return err
	}
	if err := change(sf); err != nil {
		return err
	}
	return Write(path, sf)
}

// ErrFileLocked is returned by FileLock when another holder kept the lock
// for the whole wait.
var ErrFileLocked = errors.New("held by another process")

// FileLock takes an exclusive OS file lock on lockPath (created 0600 in a
// private directory), waiting up to maxWait for another holder to release
// it. Like Lock, it goes away with the process. Other files than state.json
// use it: the stack jobs, which two MCP servers (stdio and HTTP) may crank
// at once.
func FileLock(lockPath string, maxWait time.Duration) (unlock func(), err error) {
	if err := config.MkdirPrivate(filepath.Dir(lockPath)); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(maxWait)
	for {
		err = lockFile(f)
		if err == nil {
			return func() {
				_ = unlockFile(f)
				_ = f.Close()
			}, nil
		}
		if !errors.Is(err, errLocked) || !time.Now().Before(deadline) {
			_ = f.Close()
			if errors.Is(err, errLocked) {
				return nil, ErrFileLocked
			}
			return nil, err
		}
		time.Sleep(lockRetryEvery)
	}
}
