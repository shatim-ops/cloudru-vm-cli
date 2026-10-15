package engine

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shatim-ops/cloudru-vm-cli/internal/output"
	"github.com/shatim-ops/cloudru-vm-cli/internal/provision"
)

// A catalog shaped like the real one: marketplace and GPU builds around the
// plain distribution images, empty os/status fields.
func catalogCloud() *mockCloudAPI {
	return &mockCloudAPI{
		listAvailabilityZonesFn: func(context.Context) ([]*provision.AvailabilityZone, error) {
			return []*provision.AvailabilityZone{{ID: "az-1-id", Name: "ru.AZ-1"}, {ID: "az-2-id", Name: "ru.AZ-2"}}, nil
		},
		listImagesFn: func(context.Context) ([]*provision.Image, error) {
			return []*provision.Image{
				{ID: "talos", Name: "Talos_Linux"},
				{ID: "gpu", Name: "NVLINK-Ubuntu-24.04-gpu"},
				{ID: "desk", Name: "Ubuntu_24_04_Desktop"},
				{ID: "deb11err", Name: "debian-11_error"},
				{ID: "deb12", Name: "debian-12"},
				{ID: "docker", Name: "Docker"},
				{ID: "u2204", Name: "ubuntu-22.04"},
				{ID: "u2404", Name: "Ubuntu-24.04"},
				{ID: "u2004", Name: "ubuntu-20.04"},
				{ID: "nginx", Name: "nginx-ubuntu-22.04"},
			}, nil
		},
	}
}

func TestDeployableImages_PlainUbuntuNewestFirstThenDebian(t *testing.T) {
	imgs, _ := catalogCloud().ListImages(context.Background())
	var got []string
	for _, img := range deployableImages(imgs) {
		got = append(got, img.ID)
	}
	if strings.Join(got, ",") != "u2404,u2204,u2004,deb12" {
		t.Errorf("order = %v", got)
	}
}

func initEngine(t *testing.T) (*Engine, string, *bytes.Buffer) {
	t.Helper()
	eng, dir := newTestEngine(t, catalogCloud(), newMockDeployer())
	var out bytes.Buffer
	eng.Printer = output.NewWithWriter(&out, output.ModePlain, false)
	return eng, filepath.Join(dir, ".evo.yaml"), &out
}

// --non-interactive: first zone, newest plain Ubuntu, choice printed; no
// UUID is baked into the binary.
func TestInit_NonInteractiveTakesFirstZoneAndPreferredImage(t *testing.T) {
	withPromptStdin(t, strings.NewReader("2\n"), true) // a terminal, but the flag wins
	eng, path, out := initEngine(t)

	if err := eng.Init(context.Background(), InitOptions{Path: path, NonInteractive: true}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg := string(data)
	for _, want := range []string{"region: az-1-id  # ru.AZ-1", "image_id: u2404  # Ubuntu-24.04",
		"project_id: 00000000-0000-0000-0000-000000000001"} {
		if !strings.Contains(cfg, want) {
			t.Errorf("config lacks %q:\n%s", want, cfg)
		}
	}
	for _, want := range []string{"Zone: ru.AZ-1 (az-1-id)", "Image: Ubuntu-24.04 (u2404)"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("choice not printed (%q):\n%s", want, out.String())
		}
	}
}

// Without a terminal init does not ask either.
func TestInit_NoTTYDoesNotAsk(t *testing.T) {
	withPromptStdin(t, strings.NewReader(""), false)
	eng, path, out := initEngine(t)
	if err := eng.Init(context.Background(), InitOptions{Path: path}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if strings.Contains(out.String(), "Choose") {
		t.Errorf("asked without a terminal:\n%s", out.String())
	}
}

func TestInit_InteractiveChoice(t *testing.T) {
	withPromptStdin(t, strings.NewReader("2\n"), true)
	eng, path, out := initEngine(t)
	// Each prompt reads one line; the second gets EOF → default.
	if err := eng.Init(context.Background(), InitOptions{Path: path}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "region: az-2-id") {
		t.Errorf("zone choice ignored:\n%s", data)
	}
	if !strings.Contains(out.String(), "1) ru.AZ-1") || !strings.Contains(out.String(), "2) ru.AZ-2") {
		t.Errorf("options not listed:\n%s", out.String())
	}
}

func TestInit_InvalidChoice(t *testing.T) {
	withPromptStdin(t, strings.NewReader("7\n"), true)
	eng, path, _ := initEngine(t)
	if err := eng.Init(context.Background(), InitOptions{Path: path}); err == nil {
		t.Fatal("want an error for an out-of-range choice")
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("config written despite the invalid choice")
	}
}

func TestInit_ExistingConfigUntouched(t *testing.T) {
	eng, path, _ := initEngine(t)
	if err := os.WriteFile(path, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	eng.Cloud = nil // must not reach the API
	if err := eng.Init(context.Background(), InitOptions{Path: path, NonInteractive: true}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if data, _ := os.ReadFile(path); string(data) != "mine" {
		t.Errorf("config overwritten: %q", data)
	}
}
