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

// Ревью Fable 17.10: Ctrl-C во время POST boot-FIP (ctx отменён), а
// платформа адрес создала — поиск по имени идёт мимо отменённого ctx, FIP
// находится и освобождается, а не остаётся платным без записи.
type cancelledBootFIPCloud struct {
	lostBootFIPCloud
	cancel context.CancelFunc
}

func (c *cancelledBootFIPCloud) CreateFloatingIP(ctx context.Context, req provision.CreateFloatingIPRequest) (*provision.FloatingIP, error) {
	c.fips = append(c.fips, &provision.FloatingIP{ID: "fip-cancelled", Name: req.Name, IPAddress: "203.0.113.8", State: "available"})
	c.cancel()
	return nil, ctx.Err()
}
func (c *cancelledBootFIPCloud) ListFloatingIPs(ctx context.Context, _ string) ([]*provision.FloatingIP, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return c.fips, nil
}

func TestDeploy_BootFIPFoundAfterCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cloud := &cancelledBootFIPCloud{cancel: cancel}
	dir := t.TempDir()
	var out bytes.Buffer
	e := &Engine{Config: config.Config{ProjectID: "p", ImageID: "img"}, Cloud: cloud,
		Printer: output.NewWithWriter(&out, output.ModePlain, false), StateDir: dir, Timings: fastTimings}
	_, _ = e.provisionVM(ctx, DeployOptions{Flavor: "flavor", NonInteractive: true, Yes: true}, &state.File{Version: 1},
		state.DefaultPath(dir), &compose.Project{Hash: "h"}, "az")
	if len(cloud.released) != 1 || cloud.released[0] != "fip-cancelled" {
		t.Fatalf("FIP, созданный до Ctrl-C, не освобождён: %v; вывод:\n%s", cloud.released, out.String())
	}
}
