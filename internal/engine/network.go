package engine

import (
	"context"
	crand "crypto/rand"
	"fmt"
	"strings"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/sgrules"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// vmNetwork is where a new VM is plugged in. The compute create API 500s
// without an explicit interface, so deploy always resolves one.
type vmNetwork struct {
	ZoneName        string
	SubnetID        string
	SubnetName      string
	SecurityGroupID string
	SecurityGroup   string
}

// resolveNetwork picks a subnet and a security group for the zone. The API
// leaves availability_zone_id empty on both, so they are matched by a name
// that contains the zone name ("Default_ru.AZ-1", "SSH-access_ru.AZ-1"),
// the same rule as the MCP vm_create path. A security group from another
// zone keeps the VM from booting, hence no blind "first one" for groups
// unless the project has a single candidate.
func (e *Engine) resolveNetwork(ctx context.Context, projectID, zoneID string) (vmNetwork, error) {
	var n vmNetwork
	if zones, err := e.Cloud.ListAvailabilityZones(ctx); err == nil {
		for _, z := range zones {
			if z.ID == zoneID {
				n.ZoneName = z.Name
				break
			}
		}
	}

	subnets, err := e.Cloud.ListSubnets(ctx, projectID)
	if err != nil {
		return n, fmt.Errorf(i18n.T("list subnets: %w", "список подсетей: %w"), err)
	}
	if sn := pickInZone(len(subnets), func(i int) (string, string) { return subnets[i].AvailabilityZoneID, subnets[i].Name }, zoneID, n.ZoneName); sn >= 0 {
		n.SubnetID, n.SubnetName = subnets[sn].ID, subnets[sn].Name
	} else {
		return n, clerrors.New(clerrors.CodeConfigInvalid,
			i18n.Tf("no subnet for zone %s in the project", "в проекте нет подсети для зоны %s", zoneText(n.ZoneName, zoneID)),
			i18n.T("Create a network and a subnet in this zone in the Cloud.ru console, or pick another zone with 'evo init'.",
				"Создайте сеть и подсеть в этой зоне в консоли Cloud.ru или выберите другую зону ('evo init')."))
	}

	sgs, err := e.Cloud.ListSecurityGroups(ctx, projectID)
	if err != nil {
		return n, fmt.Errorf(i18n.T("list security groups: %w", "список групп безопасности: %w"), err)
	}
	if g := pickInZone(len(sgs), func(i int) (string, string) { return sgs[i].AvailabilityZoneID, sgs[i].Name }, zoneID, n.ZoneName); g >= 0 {
		n.SecurityGroupID, n.SecurityGroup = sgs[g].ID, sgs[g].Name
	} else {
		return n, clerrors.New(clerrors.CodeConfigInvalid,
			i18n.Tf("no security group for zone %s in the project", "в проекте нет группы безопасности для зоны %s", zoneText(n.ZoneName, zoneID)),
			i18n.T("Create a security group whose name contains the zone name (e.g. 'web_ru.AZ-1') in the Cloud.ru console.",
				"Создайте в консоли Cloud.ru группу безопасности, в имени которой есть имя зоны (например, 'web_ru.AZ-1')."))
	}
	return n, nil
}

// pickInZone returns the index of the item in the zone: by zone ID, then by
// a name containing the zone name, then the only item if there is exactly
// one. -1 when nothing fits.
func pickInZone(count int, at func(int) (zoneID, name string), zoneID, zoneName string) int {
	for i := 0; i < count; i++ {
		if id, _ := at(i); id != "" && id == zoneID {
			return i
		}
	}
	if zoneName != "" {
		for i := 0; i < count; i++ {
			if _, name := at(i); strings.Contains(name, zoneName) {
				return i
			}
		}
	}
	if count == 1 {
		return 0
	}
	return -1
}

func zoneText(name, id string) string {
	if name != "" {
		return name
	}
	return id
}

// openPorts adds ingress rules for the compose ports (tcp and udp) to the
// VM's security group, skipping ports already open for that protocol, and
// records in rec the rules it created — destroy deletes those. The group is
// shared by every VM placed in it — the deploy preview says so before
// anything is created. Failures are reported, not fatal: the VM exists and
// the ports can be opened in the console.
func (e *Engine) openPorts(ctx context.Context, projectID, sgID string, tcp, udp []int, rec *state.DeploymentRecord) {
	res := sgrules.Open(ctx, e.Cloud, projectID, sgID, tcp, udp,
		func(label string) string { return "evo: port " + label }, rec)
	if len(res.Opened) > 0 {
		e.Printer.StepDone(i18n.Tf("Opened ports %s in the security group", "Открыты порты %s в группе безопасности", strings.Join(res.Opened, ",")))
	}
	if len(res.Failed) > 0 {
		e.Printer.Warning(i18n.Tf("Could not open ports %s — open them in the security group in the Cloud.ru console",
			"Не удалось открыть порты %s — откройте их в группе безопасности в консоли Cloud.ru", strings.Join(res.Failed, ",")))
	}
	if len(res.Untracked) > 0 {
		e.Printer.Warning(i18n.Tf("Ports %s were opened, but the API returned no rule ID: destroy will not close them — delete those rules in the Cloud.ru console after destroy",
			"Порты %s открыты, но API не вернул ID правил: destroy их не закроет — после destroy удалите эти правила в консоли Cloud.ru",
			strings.Join(res.Untracked, ",")))
	}
}

// flavorAPIName turns the flavor picked by sizing (an ID) or given by the
// user (an ID or a name) into the name the create API expects: it rejects
// UUIDs in flavor_name with 422.
func (e *Engine) flavorAPIName(ctx context.Context, ref string) (string, error) {
	flavors, err := e.Cloud.ListFlavors(ctx)
	if err != nil {
		return "", fmt.Errorf(i18n.T("list flavors: %w", "список flavor: %w"), err)
	}
	for _, f := range flavors {
		if f.ID == ref || f.Name == ref {
			return f.Name, nil
		}
	}
	return "", clerrors.New(clerrors.CodeConfigInvalid,
		i18n.Tf("flavor %q not found", "flavor %q не найден", ref),
		i18n.T("Use 'flavor: auto' or a flavor name from the Cloud.ru console.",
			"Укажите 'flavor: auto' или имя flavor из консоли Cloud.ru ('evo vm flavors')."))
}

// randomPassword satisfies the image's complexity rules: one of each class,
// the rest from all of them, crypto/rand throughout.
func randomPassword(n int) string {
	const (
		lower   = "abcdefghijkmnpqrstuvwxyz"
		upper   = "ABCDEFGHJKLMNPQRSTUVWXYZ"
		digit   = "23456789"
		special = "!@#$%_-"
	)
	all := lower + upper + digit + special
	rb := make([]byte, n)
	_, _ = crand.Read(rb)
	out := make([]byte, n)
	for i := range out {
		set := all
		switch i {
		case 0:
			set = lower
		case 1:
			set = upper
		case 2:
			set = digit
		case 3:
			set = special
		}
		out[i] = set[int(rb[i])%len(set)]
	}
	return string(out)
}
