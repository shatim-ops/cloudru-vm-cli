package engine

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/dzhechko/cloudru-vm-cli/internal/output"
	"github.com/dzhechko/cloudru-vm-cli/internal/provision"
)

func TestListImages_Table(t *testing.T) {
	cloud := &mockCloudAPI{}
	dep := newMockDeployer()
	eng, _ := newTestEngine(t, cloud, dep)

	// Replace printer with a buffer we can read.
	var buf bytes.Buffer
	eng.Printer = output.NewWithWriter(&buf, output.ModePlain, false)

	err := eng.ListImages(context.Background())
	if err != nil {
		t.Fatalf("ListImages returned error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "ID") || !strings.Contains(out, "Name") {
		t.Errorf("expected table headers, got: %s", out)
	}
	if !strings.Contains(out, "Ubuntu 22.04") {
		t.Errorf("expected image name in output, got: %s", out)
	}
}

func TestListImages_JSON(t *testing.T) {
	cloud := &mockCloudAPI{}
	dep := newMockDeployer()
	eng, _ := newTestEngine(t, cloud, dep)

	var buf bytes.Buffer
	eng.Printer = output.NewWithWriter(&buf, output.ModeJSON, false)

	err := eng.ListImages(context.Background())
	if err != nil {
		t.Fatalf("ListImages returned error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, `"id"`) || !strings.Contains(out, "img-1") {
		t.Errorf("expected JSON output with image data, got: %s", out)
	}
}

func TestListImages_Empty(t *testing.T) {
	cloud := &mockCloudAPI{
		listImagesFn: func(ctx context.Context) ([]*provision.Image, error) {
			return nil, nil
		},
	}
	dep := newMockDeployer()
	eng, _ := newTestEngine(t, cloud, dep)

	var buf bytes.Buffer
	eng.Printer = output.NewWithWriter(&buf, output.ModePlain, false)

	err := eng.ListImages(context.Background())
	if err != nil {
		t.Fatalf("ListImages returned error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "No images found") {
		t.Errorf("expected 'No images found' message, got: %s", out)
	}
}
