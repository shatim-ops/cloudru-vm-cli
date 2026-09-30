package deployplan

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/shatim-ops/cloudru-vm-cli/internal/pricing"
	"github.com/shatim-ops/cloudru-vm-cli/internal/provision"
)

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-`)

// Params is the input to Resolve. In auto mode, requirement fields (CPU/RAMGB/
// DiskGB) drive flavor/disk selection; in manual mode the explicit *Override
// fields are used. Empty fields fall back to documented defaults.
type Params struct {
	Name        string
	Product     string
	Mode        string // "auto" | "manual"
	CPU         int    // requirement (auto-size)
	RAMGB       int    // requirement (auto-size)
	DiskGB      int
	Ports       []int // product ports to open in the security group (e.g. 80,443)
	ComposeFile string
	ExternalIP  bool

	// Manual overrides (by name).
	Flavor   string
	Zone     string
	Image    string
	DiskType string
	Login    string
	SSHKey   string // explicit public key (full "ssh-ed25519 AAAA... comment")
}

// Resolve performs all read-only discovery and returns a costed plan. It never
// mutates Cloud.ru. The caller is expected to Save() the returned plan.
func Resolve(ctx context.Context, cloud provision.CloudAPI, projectID string, p Params) (*Plan, error) {
	if projectID == "" {
		return nil, fmt.Errorf("project_id is required")
	}
	var warnings []string
	// The SSH public key to inject (if any) is decided by the caller (the MCP
	// handler defaults it to the agent's deploy key). p.SSHKey == "" means no
	// key — fall back to a generated password.

	// --- Zone ---
	// Зона по умолчанию — из конфигурации (CLOUDRU_REGION), а не зашитая:
	// 16.09.2026 в ru.AZ-2 кончились диски, и все инструменты пришлось
	// переводить на AZ-1. Буквальная «ru.AZ-2» здесь была единственной в движке.
	zoneInput := firstNonEmpty(p.Zone, os.Getenv("CLOUDRU_REGION"), "ru.AZ-2")
	zone := provision.AvailabilityZone{ID: zoneInput, Name: zoneInput}
	zones, zErr := cloud.ListAvailabilityZones(ctx)
	if zErr == nil {
		for _, z := range zones {
			if z.ID == zoneInput || z.Name == zoneInput {
				zone = *z
				break
			}
		}
	}
	if !uuidRe.MatchString(zone.ID) {
		return nil, fmt.Errorf("could not resolve availability zone %q to a UUID", zoneInput)
	}

	// --- Flavor ---
	flavors, err := cloud.ListFlavors(ctx)
	if err != nil {
		return nil, fmt.Errorf("list flavors: %w", err)
	}
	flavor, fwarn, err := selectFlavor(flavors, p)
	if err != nil {
		return nil, err
	}
	warnings = append(warnings, fwarn...)

	// --- Image ---
	image, err := selectImage(ctx, cloud, projectID, p.Image)
	if err != nil {
		return nil, err
	}

	// --- Disk type ---
	diskGB := p.DiskGB
	if diskGB <= 0 {
		diskGB = 10
	}
	diskName, diskID := "SSD", ""
	if dts, e := cloud.ListDiskTypes(ctx, projectID); e == nil && len(dts) > 0 {
		want := firstNonEmpty(p.DiskType, "SSD")
		picked := dts[0]
		for _, dt := range dts {
			if strings.EqualFold(dt.Name, want) {
				picked = dt
				break
			}
		}
		diskName, diskID = picked.Name, picked.ID
	} else if p.DiskType != "" {
		diskName = p.DiskType
	}

	// --- Subnet (zone-matched) ---
	subnets, err := cloud.ListSubnets(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list subnets: %w", err)
	}
	subnetID := pickSubnet(subnets, zone.ID)
	if subnetID == "" {
		return nil, fmt.Errorf("no subnet found for project/zone %s — create a network+subnet in the Cloud.ru console first", zone.Name)
	}

	// --- Security group (zone-name-matched, then default, then first) ---
	sgID := ""
	if sgs, e := cloud.ListSecurityGroups(ctx, projectID); e == nil {
		sgID = pickSecurityGroup(sgs, zone.Name)
	}
	if sgID == "" {
		warnings = append(warnings, "no security group resolved — VM will use the project default")
	}
	if len(p.Ports) > 0 && sgID != "" {
		warnings = append(warnings, fmt.Sprintf("ports %v will be opened (ingress, 0.0.0.0/0) on the shared security group of this zone — other VMs later placed in it would share those open ports", p.Ports))
	}

	// --- Floating IP (variant A: attach at creation) ---
	wantFIP := p.ExternalIP
	attachFIPID := ""
	if wantFIP {
		if fips, e := cloud.ListFloatingIPs(ctx, projectID); e == nil {
			for _, f := range fips {
				if f.Available() {
					attachFIPID = f.ID
					break
				}
			}
			if attachFIPID == "" {
				warnings = append(warnings, "no free floating IP — a new one will be requested at apply; if the quota is exhausted the VM is still created WITHOUT a public IP (attach manually via the Cloud.ru console)")
			}
		}
	}

	// --- Access: image metadata fields + SSH model ---
	metaFields, metaStatic, login, keyName, sErr := resolveAccess(ctx, cloud, image.ID, p)
	if sErr != nil {
		warnings = append(warnings, "could not read image metadata template: "+sErr.Error())
	}

	// --- Cost ---
	cost := pricing.EstimateFor(flavor.Name, flavor.VCPUs, flavor.RAMGB, diskGB, diskName)

	name := p.Name
	if name == "" {
		base := firstNonEmpty(p.Product, "vm")
		name = sanitizeName(base) + "-" + time.Now().Format("20060102-1504")
	}

	now := time.Now()
	plan := &Plan{
		PlanID:    newPlanID(),
		CreatedAt: now,
		ExpiresAt: now.Add(planTTL),
		Product:   p.Product,
		Mode:      firstNonEmpty(p.Mode, "auto"),
		ProjectID: projectID,
		VM: VMSpec{
			Name:   name,
			Flavor: FlavorSpec{ID: flavor.ID, Name: flavor.Name, VCPUs: flavor.VCPUs, RAMGB: flavor.RAMGB},
			Image:  RefSpec{ID: image.ID, Name: image.Name},
			Zone:   RefSpec{ID: zone.ID, Name: zone.Name},
			Disk:   DiskSpec{SizeGB: diskGB, Type: diskName, TypeID: diskID},
		},
		Network: NetworkSpec{
			SubnetID:        subnetID,
			SecurityGroupID: sgID,
			FloatingIP:      wantFIP,
			AttachFIPID:     attachFIPID,
		},
		Ports:           p.Ports,
		SSH:             SSHSpec{Login: login, KeyName: keyName},
		Cost:            cost,
		Warnings:        warnings,
		ComposeRequired: p.ComposeFile != "",
		ComposeFile:     p.ComposeFile,
		MetaFields:      metaFields,
		MetaStatic:      metaStatic,
		PublicKey:       p.SSHKey,
		SourceParams:    p,
	}
	return plan, nil
}

// selectFlavor validates an explicit flavor by name, or auto-sizes from CPU/RAM,
// or falls back to a sane default. Free-tier flavors are skipped for auto-size.
func selectFlavor(flavors []*provision.Flavor, p Params) (provision.Flavor, []string, error) {
	var warnings []string
	byName := func(name string) *provision.Flavor {
		for _, f := range flavors {
			if f.Name == name {
				return f
			}
		}
		return nil
	}

	if p.Flavor != "" {
		if f := byName(p.Flavor); f != nil {
			return *f, warnings, nil
		}
		return provision.Flavor{}, warnings, fmt.Errorf("flavor %q not found (use the list_flavors tool to see options)", p.Flavor)
	}

	// Auto-size from requirements.
	if p.CPU > 0 || p.RAMGB > 0 {
		var cands []*provision.Flavor
		for _, f := range flavors {
			if f.FreeTier || strings.Contains(strings.ToLower(f.Name), "free") {
				continue
			}
			if f.VCPUs >= p.CPU && f.RAMGB >= p.RAMGB {
				cands = append(cands, f)
			}
		}
		if len(cands) > 0 {
			sort.Slice(cands, func(i, j int) bool {
				if cands[i].VCPUs != cands[j].VCPUs {
					return cands[i].VCPUs < cands[j].VCPUs
				}
				return cands[i].RAMGB < cands[j].RAMGB
			})
			return *cands[0], warnings, nil
		}
		warnings = append(warnings, fmt.Sprintf("no flavor satisfies %d vCPU / %d GB — falling back to default", p.CPU, p.RAMGB))
	}

	if f := byName("lowcost10-2-4"); f != nil {
		return *f, warnings, nil
	}
	// Last resort: smallest non-free flavor.
	var fallback *provision.Flavor
	for _, f := range flavors {
		if f.FreeTier {
			continue
		}
		if fallback == nil || f.VCPUs < fallback.VCPUs {
			fallback = f
		}
	}
	if fallback != nil {
		return *fallback, warnings, nil
	}
	return provision.Flavor{}, warnings, fmt.Errorf("no usable flavor available")
}

// selectImage resolves an image by name or UUID (or default Ubuntu 24.04 non-GPU).
//
// An explicit choice is looked up in TWO places. A bare /api/v1/images call
// returns only the shared catalogue: images built inside the project — exactly
// the ones image_create produces — are invisible there (see the comment on
// CloudRuAdapter.ListImages). Without the second lookup the engine cannot
// deploy what it has itself just built, and it says "not found" about an image
// that list_images plainly shows.
//
// The default branch deliberately stays on the shared catalogue: it guesses a
// base OS, and a project's own product images have no business being guessed.
func selectImage(ctx context.Context, cloud provision.CloudAPI, projectID, want string) (provision.Image, error) {
	images, err := cloud.ListImages(ctx)
	if err != nil {
		return provision.Image{}, fmt.Errorf("list images: %w", err)
	}
	if want != "" {
		for _, img := range images {
			if img.Name == want || img.ID == want {
				return *img, nil
			}
		}
		// Ошибка выборки по проекту глотается намеренно: у токена может не быть
		// прав на проектные образы, и тогда честный ответ — «такого образа нет»,
		// а не «не удалось прочитать проект».
		if own, e := cloud.ListImagesInProject(ctx, projectID); e == nil {
			for _, img := range own {
				if img.Name == want || img.ID == want {
					return *img, nil
				}
			}
		}
		return provision.Image{}, fmt.Errorf(
			"image %q not found in the shared catalogue or in project %s (use the list_images tool to see options)",
			want, projectID)
	}
	// Default: prefer ubuntu 24.04 non-gpu.
	for _, img := range images {
		n := strings.ToLower(img.Name)
		if strings.Contains(n, "ubuntu") && strings.Contains(n, "24.04") && !strings.Contains(n, "gpu") {
			return *img, nil
		}
	}
	if len(images) > 0 {
		return *images[0], nil
	}
	return provision.Image{}, fmt.Errorf("no images available")
}

// resolveAccess inspects the image's metadata template (mirroring the proven
// Python reference) and returns:
//   - dynamic: fields we fill at apply time (name/hostname/linux_password/public_key)
//   - static:  fields copied verbatim from the image's defaults (the API rejects
//     the request with image_metadata_fields_missing if these are omitted)
//   - login and a human label for the SSH model.
func resolveAccess(ctx context.Context, cloud provision.CloudAPI, imageID string, p Params) ([]string, map[string]string, string, string, error) {
	login := firstNonEmpty(p.Login, "user1")
	static := map[string]string{}

	detail, err := cloud.GetImage(ctx, imageID)
	if err != nil {
		// Fallback for Ubuntu-style images if the template can't be read.
		fields := []string{"name", "hostname", "linux_password"}
		key := "generated-password"
		if p.SSHKey != "" {
			fields = append(fields, "public_key")
			key = "captain-key"
		}
		return fields, static, login, key, err
	}

	var dynamic []string
	hasPasswordField := false
	for _, mf := range detail.MetadataFields {
		if mf.MandatoryGroup != "" && mf.DefaultValue == "" {
			// Mandatory field we must supply a value for.
			switch mf.Name {
			case "name", "hostname", "linux_password":
				dynamic = append(dynamic, mf.Name)
				if mf.Name == "linux_password" {
					hasPasswordField = true
				}
			case "public_key":
				if p.SSHKey != "" {
					dynamic = append(dynamic, "public_key")
				}
				// else: skip — an empty public_key triggers a 500.
			}
		} else if mf.DefaultValue != "" && mf.Name != "vmm_enabled" && mf.Name != "guest_agent_ready" {
			// Optional field with a default — the API still wants it sent.
			static[mf.Name] = mf.DefaultValue
		}
	}

	keyName := "generated-password"
	if p.SSHKey != "" {
		keyName = "ssh-key (deploy)"
	} else if !hasPasswordField {
		keyName = "image-default"
	}
	return dynamic, static, login, keyName, nil
}

func pickSubnet(subnets []*provision.Subnet, zoneID string) string {
	for _, s := range subnets {
		if s.AvailabilityZoneID == zoneID {
			return s.ID
		}
	}
	if len(subnets) > 0 {
		return subnets[0].ID
	}
	return ""
}

func pickSecurityGroup(sgs []*provision.SecurityGroup, zoneName string) string {
	if zoneName != "" {
		for _, sg := range sgs {
			if strings.Contains(sg.Name, zoneName) {
				return sg.ID
			}
		}
	}
	for _, sg := range sgs {
		if strings.EqualFold(sg.Name, "default") {
			return sg.ID
		}
	}
	if len(sgs) > 0 {
		return sgs[0].ID
	}
	return ""
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func sanitizeName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == ' ':
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "vm"
	}
	if len(out) > 32 {
		out = out[:32]
	}
	return out
}
