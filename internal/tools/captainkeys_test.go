package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCaptainKeys(t *testing.T) {
	dir := t.TempDir()
	keyDir := filepath.Join(dir, ".cloudru-vm")
	if err := os.MkdirAll(keyDir, 0700); err != nil {
		t.Fatal(err)
	}

	// Missing file → no keys, no error.
	if got := loadCaptainKeys(dir); got != nil {
		t.Fatalf("missing file: want nil, got %v", got)
	}

	content := `# captain keys
ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample captain@mac

-----BEGIN OPENSSH PRIVATE KEY-----
garbage-that-must-never-ship
ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQ backup@key
`
	if err := os.WriteFile(filepath.Join(keyDir, captainKeysFile), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	got := loadCaptainKeys(dir)
	want := []string{
		"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample captain@mac",
		"ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQ backup@key",
	}
	if len(got) != len(want) {
		t.Fatalf("want %d keys, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("key %d: want %q, got %q", i, want[i], got[i])
		}
	}
}

func TestShellQuoteMCP(t *testing.T) {
	if got := shellQuoteMCP(`k'ey`); got != `'k'\''ey'` {
		t.Errorf("quote: got %q", got)
	}
}
