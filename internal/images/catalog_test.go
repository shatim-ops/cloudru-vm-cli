package images

import (
	"context"
	stderrors "errors"
	"fmt"
	"strings"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// Все UUID ниже выдуманы.

// fakeCloud — каталог образов, который фильтрует как сервер: name= и
// display_name= — подстрока без учёта регистра, type=, availability_zone_id=.
type fakeCloud struct {
	shared  []*provision.Image
	own     []*provision.Image
	ownErr  error
	queries []provision.ImageFilter
}

func (c *fakeCloud) ListImages(_ context.Context, filter ...provision.ImageFilter) ([]*provision.Image, error) {
	var f provision.ImageFilter
	if len(filter) > 0 {
		f = filter[0]
	}
	c.queries = append(c.queries, f)
	var out []*provision.Image
	for _, img := range c.shared {
		if f.Name != "" && !strings.Contains(strings.ToLower(img.Name), strings.ToLower(f.Name)) {
			continue
		}
		if f.DisplayName != "" && !strings.Contains(strings.ToLower(img.DisplayName), strings.ToLower(f.DisplayName)) {
			continue
		}
		if f.Type != "" && img.Type != f.Type {
			continue
		}
		if f.AvailabilityZoneID != "" && !inZone(img, f.AvailabilityZoneID, false) {
			continue
		}
		cp := *img
		out = append(out, &cp)
	}
	return out, nil
}

func (c *fakeCloud) ListImagesInProject(_ context.Context, projectID string) ([]*provision.Image, error) {
	if c.ownErr != nil {
		return nil, c.ownErr
	}
	out := make([]*provision.Image, 0, len(c.own))
	for _, img := range c.own {
		cp := *img
		out = append(out, &cp)
	}
	return out, nil
}

func (c *fakeCloud) GetImage(_ context.Context, id string) (*provision.ImageDetail, error) {
	for _, img := range append(append([]*provision.Image{}, c.shared...), c.own...) {
		if img.ID == id {
			return &provision.ImageDetail{ID: img.ID, Name: img.Name, DisplayName: img.DisplayName, Type: img.Type, AvailZones: img.AvailZones}, nil
		}
	}
	return nil, clierrors.New(clierrors.CodeVMNotFound, "resource not found: GET /api/v1/images/"+id, "")
}

const (
	zone1 = "00000000-0000-0000-0000-00000000a001"
	zone2 = "00000000-0000-0000-0000-00000000a002"
)

func ready(zones ...string) []provision.ImageAvailZone {
	var out []provision.ImageAvailZone
	for _, z := range zones {
		name := "ru.AZ-1"
		if z == zone2 {
			name = "ru.AZ-2"
		}
		out = append(out, provision.ImageAvailZone{AvailabilityZoneID: z, AvailabilityZoneName: name, State: "created"})
	}
	return out
}

func img(n int, name, display, typ string, zones []provision.ImageAvailZone) *provision.Image {
	return &provision.Image{ID: fmt.Sprintf("00000000-0000-0000-0000-%012d", n), Name: name, DisplayName: display, Type: typ, AvailZones: zones}
}

func testCatalog() *fakeCloud {
	off := false
	return &fakeCloud{
		shared: []*provision.Image{
			img(1, "AstraLinuxSmolensk1.8.3KES", "Astra Linux Смоленск 1.8.3 KES", "marketplace", ready(zone1, zone2)),
			img(2, "alse-base-1-7-7", "Astra Linux SE «Базовый» 1.7.7", "marketplace", ready(zone1)),
			img(3, "astra-voronej", "astra-voronej-cc11079", "marketplace", []provision.ImageAvailZone{{AvailabilityZoneID: zone1, State: "error"}}),
			img(4, "Ubuntu-24.04", "Ubuntu 24.04", "public", ready(zone1, zone2)),
			img(5, "ubuntu-test2204", "Ubuntu test", "public", []provision.ImageAvailZone{{AvailabilityZoneID: zone1, State: "created", Enabled: &off}}),
			img(6, "Ubuntu-22.04", "Ubuntu 22.04", "public", ready(zone2)),
			img(7, "astra_1.8.1", "Astra Linux SE «Смоленск» 1.8.1 (KES)", "marketplace", ready(zone1, zone2)),
		},
		own: []*provision.Image{
			// Образ проекта в сборке: показывается и без --all.
			{ID: "00000000-0000-0000-0000-000000000099", Name: "astra-golden", AvailZones: []provision.ImageAvailZone{{AvailabilityZoneID: zone1, State: "creating"}}},
		},
	}
}

func names(list []*provision.Image) []string {
	out := make([]string, len(list))
	for i, img := range list {
		out[i] = img.Name
	}
	return out
}

// Запрос уходит на сервер дважды — name= и display_name=, ответы
// сливаются по id без дублей; неготовые образы каталога скрыты, образ
// проекта виден; порядок — public, marketplace, private, внутри по
// отображаемому имени.
func TestSearch_QueryMergeHideSort(t *testing.T) {
	cloud := testCatalog()
	cat := Catalog{Cloud: cloud, ProjectID: "00000000-0000-0000-0000-0000000000f1"}
	res, err := cat.Search(context.Background(), "ASTRA", Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(cloud.queries) != 2 || cloud.queries[0].Name != "ASTRA" || cloud.queries[1].DisplayName != "ASTRA" ||
		cloud.queries[0].DisplayName != "" || cloud.queries[1].Name != "" {
		t.Fatalf("server queries: %+v", cloud.queries)
	}
	got := strings.Join(names(res.Images), ",")
	want := "alse-base-1-7-7,astra_1.8.1,AstraLinuxSmolensk1.8.3KES,astra-golden"
	if got != want {
		t.Errorf("images:\n got %s\nwant %s", got, want)
	}
	if res.Total != 5 || res.Hidden != 1 {
		t.Errorf("total %d hidden %d, want 5 and 1", res.Total, res.Hidden)
	}
	if res.Images[3].Type != "private" {
		t.Errorf("project image type %q, want private", res.Images[3].Type)
	}

	all, err := cat.Search(context.Background(), "astra", Filter{All: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Images) != 5 || all.Hidden != 0 {
		t.Errorf("--all: %v hidden %d", names(all.Images), all.Hidden)
	}

	// Без запроса — один вызов без name/display_name; public первыми.
	cloud.queries = nil
	every, err := cat.Search(context.Background(), "", Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(cloud.queries) != 1 || cloud.queries[0].Name != "" || cloud.queries[0].DisplayName != "" {
		t.Errorf("queries without a query: %+v", cloud.queries)
	}
	if got := strings.Join(names(every.Images), ","); got != "Ubuntu-22.04,Ubuntu-24.04,alse-base-1-7-7,astra_1.8.1,AstraLinuxSmolensk1.8.3KES,astra-golden" {
		t.Errorf("order: %s", got)
	}
}

func TestSearch_TypeAndZone(t *testing.T) {
	cloud := testCatalog()
	cat := Catalog{Cloud: cloud, ProjectID: "00000000-0000-0000-0000-0000000000f1"}

	res, err := cat.Search(context.Background(), "", Filter{Type: "public"})
	if err != nil {
		t.Fatal(err)
	}
	if cloud.queries[0].Type != "public" {
		t.Errorf("type not sent to the server: %+v", cloud.queries[0])
	}
	if got := strings.Join(names(res.Images), ","); got != "Ubuntu-22.04,Ubuntu-24.04" {
		t.Errorf("public: %s", got)
	}

	cloud.queries = nil
	res, err = cat.Search(context.Background(), "", Filter{Type: "private"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cloud.queries) != 0 {
		t.Errorf("private must not read the shared catalogue: %+v", cloud.queries)
	}
	if got := strings.Join(names(res.Images), ","); got != "astra-golden" {
		t.Errorf("private: %s", got)
	}

	res, err = cat.Search(context.Background(), "ubuntu", Filter{ZoneID: zone2})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(names(res.Images), ","); got != "Ubuntu-22.04,Ubuntu-24.04" {
		t.Errorf("zone 2: %s", got)
	}

	if _, err := cat.Search(context.Background(), "", Filter{Type: "gold"}); clierrors.Reason(err) != clierrors.CodeUsage {
		t.Errorf("unknown type: %v", err)
	}

	// Образы проекта не прочитались — каталог всё равно показан, с предупреждением.
	cloud.ownErr = stderrors.New("boom")
	res, err = cat.Search(context.Background(), "astra", Filter{})
	if err != nil || res.Warning == "" || len(res.Images) != 3 {
		t.Errorf("own images failing: %v %+v", err, res)
	}
}

func TestResolve(t *testing.T) {
	cat := Catalog{Cloud: testCatalog(), ProjectID: "00000000-0000-0000-0000-0000000000f1"}
	ctx := context.Background()
	for ref, want := range map[string]string{
		"00000000-0000-0000-0000-000000000004": "Ubuntu-24.04",               // UUID
		"astralinuxsmolensk1.8.3kes":           "AstraLinuxSmolensk1.8.3KES", // точное имя без учёта регистра
		"ubuntu 22.04":                         "Ubuntu-22.04",               // точное отображаемое имя
		"alse-base":                            "alse-base-1-7-7",            // единственная подстрока
		"voronej":                              "astra-voronej",              // единственная, хоть и неготовая
		"  golden ":                            "astra-golden",               // образ проекта
	} {
		got, err := cat.Resolve(ctx, ref)
		if err != nil {
			t.Errorf("%q: %v", ref, err)
			continue
		}
		if got.Name != want {
			t.Errorf("%q → %s, want %s", ref, got.Name, want)
		}
	}

	_, err := cat.Resolve(ctx, "astra")
	var amb *AmbiguousError
	if !stderrors.As(err, &amb) || clierrors.Reason(err) != clierrors.CodeUsage || clierrors.ExitCode(err) != clierrors.ExitGeneral {
		t.Fatalf("ambiguous: %v", err)
	}
	if len(amb.Candidates) != 4 {
		t.Errorf("candidates (the usable ones): %v", names(amb.Candidates))
	}
	msg := err.Error()
	for _, w := range []string{"AstraLinuxSmolensk1.8.3KES — Astra Linux Смоленск 1.8.3 KES — marketplace", "astra-golden — astra-golden — private"} {
		if !strings.Contains(msg, w) {
			t.Errorf("message lacks %q:\n%s", w, msg)
		}
	}
	var ce *clierrors.CLIError
	if stderrors.As(err, &ce); ce == nil || !strings.Contains(ce.Hint, "evo image list astra") {
		t.Errorf("hint: %+v", ce)
	}

	for _, ref := range []string{"windows", "00000000-0000-0000-0000-00000000dead"} {
		_, err = cat.Resolve(ctx, ref)
		if clierrors.Reason(err) != clierrors.CodeNotFound || clierrors.ExitCode(err) != clierrors.ExitNotFound {
			t.Errorf("%s: %v (exit %d)", ref, err, clierrors.ExitCode(err))
		}
		if stderrors.As(err, &ce); !strings.Contains(ce.Hint, "evo image list") {
			t.Errorf("not-found hint: %q", ce.Hint)
		}
	}
	if _, err := cat.Resolve(ctx, " "); clierrors.Reason(err) != clierrors.CodeUsage {
		t.Errorf("empty: %v", err)
	}
}

// Неоднозначность называет не больше десяти кандидатов.
func TestResolve_AmbiguityCapsCandidates(t *testing.T) {
	cloud := &fakeCloud{}
	for i := range 14 {
		cloud.shared = append(cloud.shared, img(i+1, fmt.Sprintf("alt-%02d", i), "", "marketplace", ready(zone1)))
	}
	_, err := Catalog{Cloud: cloud}.Resolve(context.Background(), "alt")
	var amb *AmbiguousError
	if !stderrors.As(err, &amb) || len(amb.Candidates) != MaxCandidates {
		t.Fatalf("%v", err)
	}
	if !strings.Contains(err.Error(), "4") || strings.Count(err.Error(), "\n  alt-") != MaxCandidates {
		t.Errorf("message:\n%s", err.Error())
	}
}

func TestZoneShort(t *testing.T) {
	for in, want := range map[string]string{"ru.AZ-1": "AZ-1", "AZ-2": "AZ-2", "": "", "x.": "x."} {
		if got := ZoneShort(in); got != want {
			t.Errorf("%q → %q", in, got)
		}
	}
}
