package engine

import (
	"context"
	crand "crypto/rand"
	"fmt"
	"sort"
	"strings"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/provision"
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

// resolveNetwork picks a subnet and a security group for the zone: by the
// zone id the list returns nested (availability_zone.id), else by a name
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
	if want := e.Config.SecurityGroup; want != "" {
		for _, g := range sgs {
			if g.ID != want && g.Name != want {
				continue
			}
			// A group of another zone keeps the VM from booting, and deploy
			// would open the compose ports in it. A group without a zone in
			// the API answer is taken as is: nothing to check it against.
			if g.AvailabilityZoneID != "" && g.AvailabilityZoneID != zoneID {
				return n, clerrors.New(clerrors.CodeConfigInvalid,
					i18n.Tf("security group %q from security_group is not in zone %s", "группа безопасности %q из security_group не в зоне %s", want, zoneText(n.ZoneName, zoneID)),
					i18n.T("Name a group of this zone in security_group, or remove the key: evo then takes the zone's default group (SSH-access_<zone>).",
						"Укажите в security_group группу этой зоны или уберите ключ: тогда evo возьмёт группу зоны по умолчанию (SSH-access_<зона>)."))
			}
			n.SecurityGroupID, n.SecurityGroup = g.ID, g.Name
			return n, nil
		}
		return n, clerrors.New(clerrors.CodeConfigInvalid,
			i18n.Tf("security group %q from security_group is not in the project", "группы безопасности %q из security_group нет в проекте", want),
			i18n.T("Check the name with 'evo tools call list_security_groups', or remove security_group from .evo.yaml.",
				"Проверьте имя ('evo tools call list_security_groups') или уберите security_group из .evo.yaml."))
	}
	if g := provision.PickSecurityGroup(sgs, zoneID, n.ZoneName); g >= 0 {
		n.SecurityGroupID, n.SecurityGroup = sgs[g].ID, sgs[g].Name
		if others := zoneGroups(sgs, zoneID, n.ZoneName); len(others) > 1 && !provision.IsDefaultGroup(sgs[g], n.ZoneName) {
			e.Printer.Warning(i18n.Tf("several security groups in zone %s (%s), none is the zone's default group: took %s; deploy opens the compose ports in it. Set security_group in .evo.yaml to choose",
				"в зоне %s несколько групп безопасности (%s), группы по умолчанию среди них нет: взята %s, в ней deploy откроет порты compose. Выбрать другую — security_group в .evo.yaml",
				zoneText(n.ZoneName, zoneID), strings.Join(others, ", "), sgs[g].Name))
		}
	} else {
		return n, clerrors.New(clerrors.CodeConfigInvalid,
			i18n.Tf("no security group for zone %s in the project", "в проекте нет группы безопасности для зоны %s", zoneText(n.ZoneName, zoneID)),
			i18n.T("Create a security group whose name contains the zone name (e.g. 'web_ru.AZ-1') in the Cloud.ru console.",
				"Создайте в консоли Cloud.ru группу безопасности, в имени которой есть имя зоны (например, 'web_ru.AZ-1')."))
	}
	return n, nil
}

// zoneGroups lists the names of the zone's groups, for the warning.
func zoneGroups(sgs []*provision.SecurityGroup, zoneID, zoneName string) []string {
	var out []string
	for _, g := range sgs {
		if inZone(g.AvailabilityZoneID, g.Name, zoneID, zoneName) {
			out = append(out, g.Name)
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out
}

// inZone: the zone by ID, else by a name containing the zone name.
func inZone(itemZoneID, name, zoneID, zoneName string) bool {
	return (itemZoneID != "" && itemZoneID == zoneID) || (zoneName != "" && strings.Contains(name, zoneName))
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
