package engine

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/compose"
	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// R7: POST boot-FIP применён сервером, ответ потерян; CreateVM затем
// отказывает — deploy находит FIP по имени и освобождает его.
type lostBootFIPCloud struct {
	interruptCloud
	fips     []*provision.FloatingIP
	released []string
}

func (c *lostBootFIPCloud) CreateFloatingIP(_ context.Context, req provision.CreateFloatingIPRequest) (*provision.FloatingIP, error) {
	c.fips = append(c.fips, &provision.FloatingIP{ID: "fip-lost", Name: req.Name, IPAddress: "203.0.113.7", State: "available"})
	return nil, errors.New("cannot reach Cloud.ru API: connection reset")
}
func (c *lostBootFIPCloud) ListFloatingIPs(context.Context, string) ([]*provision.FloatingIP, error) {
	return c.fips, nil
}
func (c *lostBootFIPCloud) ListVMsInProject(context.Context, string) ([]*provision.VM, error) {
	return nil, nil
}
func (c *lostBootFIPCloud) CreateVM(context.Context, provision.CreateVMRequest) (*provision.VM, error) {
	return nil, errors.New("API returned status 422: quota")
}
func (c *lostBootFIPCloud) DeleteFloatingIP(_ context.Context, id, _ string) error {
	c.released = append(c.released, id)
	return nil
}

func TestQAr3_Deploy_LostBootFIPAnswer(t *testing.T) {
	cloud := &lostBootFIPCloud{}
	dir := t.TempDir()
	var out bytes.Buffer
	e := &Engine{Config: config.Config{ProjectID: "p", ImageID: "img"}, Cloud: cloud,
		Printer: output.NewWithWriter(&out, output.ModePlain, false), StateDir: dir, Timings: fastTimings}
	_, err := e.provisionVM(context.Background(), DeployOptions{Flavor: "flavor", NonInteractive: true, Yes: true}, &state.File{Version: 1},
		state.DefaultPath(dir), &compose.Project{Hash: "h"}, "az")
	if err == nil || !strings.Contains(err.Error(), "422") {
		t.Fatalf("provisionVM = %v", err)
	}
	if len(cloud.released) == 0 {
		t.Fatalf("FIP %q создан (ответ потерян), ВМ нет — FIP не освобождён и не записан; вывод:\n%s", cloud.fips[0].Name, out.String())
	}
}
