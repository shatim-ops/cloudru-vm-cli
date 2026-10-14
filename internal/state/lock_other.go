//go:build !(linux || darwin || freebsd || openbsd || netbsd || dragonfly || windows)

package state

import "os"

// No file locking on this platform: Lock always succeeds.
func lockFile(*os.File) error   { return nil }
func unlockFile(*os.File) error { return nil }
