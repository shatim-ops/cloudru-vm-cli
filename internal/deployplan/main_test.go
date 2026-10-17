package deployplan

import (
	"os"
	"testing"
	"time"
)

// Fake clouds answer with documentation addresses (203.0.113.0/24) that
// never open port 22: the wait for SSH is cut to a moment.
func TestMain(m *testing.M) {
	sshPortWait = 10 * time.Millisecond
	os.Exit(m.Run())
}
