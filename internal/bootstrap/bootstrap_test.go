package bootstrap

import (
	"errors"
	"strings"
	"testing"

	clierrors "github.com/shatim-ops/cloudru-vm-cli/internal/errors"
)

const testSSHKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestKeyForUnitTests user@test"

func validConfig() Config {
	cfg := Defaults()
	cfg.SSHPublicKey = testSSHKey
	cfg.Ports = []int{22, 80, 443}
	return cfg
}

func TestRender_ValidConfig(t *testing.T) {
	out, err := Render(validConfig())
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !strings.HasPrefix(out, "#cloud-config") {
		t.Error("rendered output should start with #cloud-config")
	}
}

func TestRender_ContainsDockerInstall(t *testing.T) {
	out, err := Render(validConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "get.docker.com") {
		t.Error("rendered output should contain Docker install command (get.docker.com)")
	}
}

func TestRender_ContainsSSHKey(t *testing.T) {
	out, err := Render(validConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, testSSHKey) {
		t.Error("rendered output should contain the SSH public key")
	}
}

func TestRender_ContainsPorts(t *testing.T) {
	out, err := Render(validConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, port := range []string{"ufw allow 22/tcp", "ufw allow 80/tcp", "ufw allow 443/tcp"} {
		if !strings.Contains(out, port) {
			t.Errorf("rendered output should contain %q", port)
		}
	}
}

func TestRender_Port22AlwaysIncluded(t *testing.T) {
	cfg := Defaults()
	cfg.SSHPublicKey = testSSHKey
	cfg.Ports = []int{80, 443} // no 22

	out, err := Render(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "ufw allow 22/tcp") {
		t.Error("port 22 should always be included even if not specified in input")
	}
}

func TestRender_EmptySSHKey(t *testing.T) {
	cfg := Defaults()
	cfg.SSHPublicKey = ""
	cfg.Ports = []int{22}

	_, err := Render(cfg)
	if err == nil {
		t.Fatal("expected error for empty SSH key")
	}

	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected CLIError, got: %T", err)
	}
	if cliErr.Code != clierrors.CodeConfigInvalid {
		t.Errorf("expected code %s, got %s", clierrors.CodeConfigInvalid, cliErr.Code)
	}
}

func TestRender_ZeroSwap(t *testing.T) {
	cfg := Defaults()
	cfg.SSHPublicKey = testSSHKey
	cfg.SwapSizeMB = 0
	cfg.Ports = []int{22}

	_, err := Render(cfg)
	if err == nil {
		t.Fatal("expected error for zero swap size")
	}

	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected CLIError, got: %T", err)
	}
	if cliErr.Code != clierrors.CodeConfigInvalid {
		t.Errorf("expected code %s, got %s", clierrors.CodeConfigInvalid, cliErr.Code)
	}
}

func TestValidate_AddsPort22(t *testing.T) {
	cfg := Defaults()
	cfg.SSHPublicKey = testSSHKey
	cfg.Ports = []int{80, 443}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}

	has22 := false
	for _, p := range cfg.Ports {
		if p == 22 {
			has22 = true
			break
		}
	}
	if !has22 {
		t.Error("Validate should add port 22 when missing")
	}
}
