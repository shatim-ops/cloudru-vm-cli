package deployplan

import (
	"context"
	"errors"
	"strings"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// deploy_plan из образа общего каталога, которого проект не видит (GET
// /images?project_id= его не отдаёт): плана нет — иначе deploy_apply
// получит 422 wrong_image уже после одобрения. ID выдуманы.

type projectViewCloud struct {
	provision.CloudAPI
	shared    []*provision.Image
	own       []*provision.Image
	hidden    map[string]bool
	viewErr   error
	projected []provision.ImageFilter
}

func (c *projectViewCloud) ListAvailabilityZones(context.Context) ([]*provision.AvailabilityZone, error) {
	return []*provision.AvailabilityZone{{ID: "00000000-0000-0000-0000-00000000a002", Name: "ru.AZ-2"}}, nil
}
func (c *projectViewCloud) ListFlavors(context.Context) ([]*provision.Flavor, error) {
	return []*provision.Flavor{{ID: "00000000-0000-0000-0000-00000000f001", Name: "lowcost10-2-4", VCPUs: 2, RAMGB: 4}}, nil
}
func (c *projectViewCloud) ListImages(_ context.Context, f ...provision.ImageFilter) ([]*provision.Image, error) {
	if len(f) == 0 || f[0].ProjectID == "" {
		return c.shared, nil
	}
	c.projected = append(c.projected, f[0])
	if c.viewErr != nil {
		return nil, c.viewErr
	}
	var out []*provision.Image
	for _, img := range append(append([]*provision.Image{}, c.shared...), c.own...) {
		if !c.hidden[img.ID] && strings.Contains(strings.ToLower(img.Name), strings.ToLower(f[0].Name)) {
			out = append(out, img)
		}
	}
	return out, nil
}
func (c *projectViewCloud) ListImagesInProject(context.Context, string) ([]*provision.Image, error) {
	return c.own, nil
}

const (
	pvUbuntu = "00000000-0000-0000-0000-00000000c001"
	pvAstra  = "00000000-0000-0000-0000-00000000c002"
	pvOwn    = "00000000-0000-0000-0000-00000000c003"
)

func newProjectViewCloud() *projectViewCloud {
	return &projectViewCloud{
		shared: []*provision.Image{
			{ID: pvUbuntu, Name: "Ubuntu-24.04", Type: "public"},
			{ID: pvAstra, Name: "astra_1.8.1", Type: "marketplace"},
		},
		own:    []*provision.Image{{ID: pvOwn, Name: "golden-astra"}},
		hidden: map[string]bool{pvAstra: true},
	}
}

func TestResolve_ImageNotInProjectRefused(t *testing.T) {
	for _, ref := range []string{"astra_1.8.1", pvAstra} {
		cloud := newProjectViewCloud()
		plan, err := Resolve(context.Background(), cloud, "proj-1", Params{Image: ref})
		if plan != nil || clierrors.Reason(err) != clierrors.CodeNotFound || clierrors.ExitCode(err) != clierrors.ExitNotFound ||
			!strings.Contains(err.Error(), `"astra_1.8.1"`) {
			t.Fatalf("%s: plan %v, err %v", ref, plan != nil, err)
		}
		if len(cloud.projected) != 1 || cloud.projected[0].ProjectID != "proj-1" || cloud.projected[0].Name != "astra_1.8.1" {
			t.Errorf("%s: project view queries %+v", ref, cloud.projected)
		}
	}
}

// Доступный образ, образ проекта и непрочитанный каталог проекта — без
// отказа.
func TestSelectImage_AvailableOwnAndUnknown(t *testing.T) {
	ctx := context.Background()
	cloud := newProjectViewCloud()
	for _, ref := range []string{"Ubuntu-24.04", "", "golden-astra"} {
		if _, err := selectImage(ctx, cloud, "proj-1", ref); err != nil {
			t.Errorf("%q: %v", ref, err)
		}
	}
	cloud.projected = nil
	if _, err := selectImage(ctx, cloud, "proj-1", "golden-astra"); err != nil || len(cloud.projected) != 0 {
		t.Errorf("own image checked against the project view: %v %+v", err, cloud.projected)
	}
	cloud.viewErr = errors.New("503")
	if img, err := selectImage(ctx, cloud, "proj-1", "astra_1.8.1"); err != nil || img.ID != pvAstra {
		t.Errorf("project view failing: %v %+v", err, img)
	}
}

// PriceCalculation: у подделки нет API цены — превью берёт оценку по карте.
func (*projectViewCloud) PriceCalculation(context.Context, string, provision.PriceCalculationRequest) (*provision.PriceCalculation, error) {
	return nil, errNoPriceAPI
}
