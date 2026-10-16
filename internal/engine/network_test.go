package engine

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// The create API answers 500 to UUIDs and to a request without an interface
// (seen live on 2026-10-14), so deploy must send names and a zone-matched
// subnet and security group, then open the compose ports.
func TestDeploy_CreateRequestUsesNamesAndZoneNetwork(t *testing.T) {
	cloud := &mockCloudAPI{
		listAvailabilityZonesFn: func(context.Context) ([]*provision.AvailabilityZone, error) {
			return []*provision.AvailabilityZone{{ID: "ru-moscow-1", Name: "ru.AZ-1"}, {ID: "az-2", Name: "ru.AZ-2"}}, nil
		},
		// The API leaves availability_zone_id empty: match by name.
		listSubnetsFn: func(context.Context, string) ([]*provision.Subnet, error) {
			return []*provision.Subnet{{ID: "sn-2", Name: "Default_ru.AZ-2"}, {ID: "sn-1", Name: "Default_ru.AZ-1"}}, nil
		},
		listSecurityGroupsFn: func(context.Context, string) ([]*provision.SecurityGroup, error) {
			return []*provision.SecurityGroup{
				{ID: "sg-other", Name: "inspector-fileserver-sg"},
				{ID: "sg-2", Name: "SSH-access_ru.AZ-2"},
				{ID: "sg-1", Name: "SSH-access_ru.AZ-1", Rules: []provision.SGRule{{Direction: "ingress", PortRange: "5432:5432"}}},
			}, nil
		},
	}
	eng, dir := newTestEngine(t, cloud, newMockDeployer())
	eng.Config.Region = "ru-moscow-1"
	if err := eng.Deploy(context.Background(), newDeployOpts(t, dir)); err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	req := cloud.createReq
	if req.FlavorID != "" || req.ImageID != "" {
		t.Errorf("UUID fields sent: flavor_id=%q image_id=%q", req.FlavorID, req.ImageID)
	}
	// Sizing picks a flavor by ID; the API wants its name (422 on a UUID).
	if req.FlavorName != "small" || req.ImageName == "" {
		t.Errorf("names: flavor_name=%q (want small) image_name=%q", req.FlavorName, req.ImageName)
	}
	if len(req.Interfaces) != 1 {
		t.Fatalf("interfaces = %+v, want one", req.Interfaces)
	}
	in := req.Interfaces[0]
	if in.SubnetID != "sn-1" || len(in.SecurityGroups) != 1 || in.SecurityGroups[0] != "sg-1" {
		t.Errorf("interface = %+v, want subnet sn-1 and group sg-1 of ru.AZ-1", in)
	}
	for k, v := range req.ImageMetadata {
		if v == "" {
			t.Errorf("empty image metadata %q sent", k)
		}
	}

	// Host ports from compose: 5432 is already open in sg-1, 8080 is not.
	if len(cloud.addedRules) != 1 || cloud.addedRules[0].PortRange != "8080:8080" {
		t.Errorf("rules added = %+v, want only 8080:8080", cloud.addedRules)
	}
}

func TestResolveNetwork_NoSubnetInZone(t *testing.T) {
	cloud := &mockCloudAPI{
		listAvailabilityZonesFn: func(context.Context) ([]*provision.AvailabilityZone, error) {
			return []*provision.AvailabilityZone{{ID: "z3", Name: "ru.AZ-3"}}, nil
		},
		listSubnetsFn: func(context.Context, string) ([]*provision.Subnet, error) {
			return []*provision.Subnet{{ID: "sn-1", Name: "Default_ru.AZ-1"}, {ID: "sn-2", Name: "Default_ru.AZ-2"}}, nil
		},
	}
	eng, _ := newTestEngine(t, cloud, newMockDeployer())
	if _, err := eng.resolveNetwork(context.Background(), "p", "z3"); err == nil {
		t.Fatal("want an error when no subnet matches the zone, a VM in another zone's subnet does not boot")
	}
}

// Основная подсеть зоны (признак default из API) — раньше первой по порядку;
// подсеть без DNS-серверов берётся, но с предупреждением: ВМ не разрешит
// имена (документация, «Temporary failure in name resolution»).
func TestResolveNetwork_SubnetDefaultAndDNSWarning(t *testing.T) {
	empty := []any{}
	cloud := &mockCloudAPI{
		listAvailabilityZonesFn: func(context.Context) ([]*provision.AvailabilityZone, error) {
			return []*provision.AvailabilityZone{{ID: "z2", Name: "ru.AZ-2"}}, nil
		},
		listSubnetsFn: func(context.Context, string) ([]*provision.Subnet, error) {
			return []*provision.Subnet{
				{ID: "sn-a", Name: "app", AvailabilityZoneID: "z2"},
				{ID: "sn-main", Name: "main", AvailabilityZoneID: "z2", Default: true, DNSServers: &empty},
			}, nil
		},
		listSecurityGroupsFn: func(context.Context, string) ([]*provision.SecurityGroup, error) {
			return []*provision.SecurityGroup{{ID: "sg", Name: "SSH-access_ru.AZ-2", AvailabilityZoneID: "z2"}}, nil
		},
	}
	eng, _ := newTestEngine(t, cloud, newMockDeployer())
	var buf bytes.Buffer
	eng.Printer = output.NewWithWriter(&buf, output.ModePlain, false)
	n, err := eng.resolveNetwork(context.Background(), "p", "z2")
	if err != nil || n.SubnetID != "sn-main" {
		t.Fatalf("subnet = %q, %v; want the zone's main subnet", n.SubnetID, err)
	}
	if !strings.Contains(buf.String(), "DNS") {
		t.Errorf("no DNS warning: %q", buf.String())
	}
}

func TestRandomPassword_HasEveryClass(t *testing.T) {
	for i := 0; i < 50; i++ {
		p := randomPassword(16)
		if len(p) != 16 ||
			!strings.ContainsAny(p, "abcdefghijkmnpqrstuvwxyz") ||
			!strings.ContainsAny(p, "ABCDEFGHJKLMNPQRSTUVWXYZ") ||
			!strings.ContainsAny(p, "23456789") ||
			!strings.ContainsAny(p, "!@#$%_-") {
			t.Fatalf("weak password %q", p)
		}
	}
}

// Группа безопасности для новой ВМ не зависит от порядка списка API: сперва
// Default зоны, затем единственная, затем первая по имени. Раньше бралась
// первая по порядку — в тестовом проекте это была группа другого сервиса,
// и deploy открыл в ней порт 80 для 0.0.0.0/0 (QA r4 Q3).
func TestResolveNetwork_SecurityGroupChoice(t *testing.T) {
	zones := func(context.Context) ([]*provision.AvailabilityZone, error) {
		return []*provision.AvailabilityZone{{ID: "z2", Name: "ru.AZ-2"}}, nil
	}
	subnets := func(context.Context, string) ([]*provision.Subnet, error) {
		return []*provision.Subnet{{ID: "sn-2", Name: "Default_ru.AZ-2", AvailabilityZoneID: "z2"}}, nil
	}
	groups := []*provision.SecurityGroup{
		{ID: "sg-other", Name: "inspector-fileserver-sg", AvailabilityZoneID: "z2"},
		{ID: "sg-def", Name: "Default_ru.AZ-2", AvailabilityZoneID: "z2"},
		{ID: "sg-ssh", Name: "SSH-access_ru.AZ-2", AvailabilityZoneID: "z2"},
		{ID: "sg-az1", Name: "Default_ru.AZ-1", AvailabilityZoneID: "z1"},
	}
	for _, tc := range []struct {
		name, configured string
		groups           []*provision.SecurityGroup
		want             string
		wantErr          bool
	}{
		{"Default зоны, а не первая по порядку", "", groups, "sg-def", false},
		// Группа зоны по умолчанию у Cloud.ru — SSH-access_<зона> (документация
		// «Группы безопасности»), а не чужая группа, первая по алфавиту:
		// 16.10.2026 deploy открывал порты в inspector-fileserver-sg.
		{"SSH-access_<зона> — группа по умолчанию", "", []*provision.SecurityGroup{groups[2], groups[0]}, "sg-ssh", false},
		// Признак default из API сильнее имён.
		{"признак default из API", "", []*provision.SecurityGroup{
			groups[0], groups[1], {ID: "sg-flag", Name: "web", AvailabilityZoneID: "z2", Default: true},
		}, "sg-flag", false},
		{"без группы по умолчанию — первая по имени", "", []*provision.SecurityGroup{
			{ID: "sg-web", Name: "web", AvailabilityZoneID: "z2"}, groups[0],
		}, "sg-other", false},
		{"security_group по имени", "SSH-access_ru.AZ-2", groups, "sg-ssh", false},
		{"security_group по UUID", "sg-other", groups, "sg-other", false},
		{"security_group, которой нет", "nope", groups, "", true},
		// Ревью Grok на #67: явная группа другой зоны не берётся.
		{"security_group из другой зоны", "Default_ru.AZ-1", groups, "", true},
		// «DefaultWeb» — не группа Default зоны: берётся первая по имени.
		{"префикс Default — не Default", "", []*provision.SecurityGroup{
			{ID: "sg-dweb", Name: "DefaultWeb", AvailabilityZoneID: "z2"},
			{ID: "sg-app", Name: "app", AvailabilityZoneID: "z2"},
		}, "sg-app", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sgs := tc.groups
			cloud := &mockCloudAPI{
				listAvailabilityZonesFn: zones,
				listSubnetsFn:           subnets,
				listSecurityGroupsFn: func(context.Context, string) ([]*provision.SecurityGroup, error) {
					return sgs, nil
				},
			}
			eng, _ := newTestEngine(t, cloud, newMockDeployer())
			eng.Config.SecurityGroup = tc.configured
			n, err := eng.resolveNetwork(context.Background(), "p", "z2")
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want an error, got group %s", n.SecurityGroupID)
				}
				return
			}
			if err != nil || n.SecurityGroupID != tc.want {
				t.Fatalf("group = %q, %v; want %s", n.SecurityGroupID, err, tc.want)
			}
		})
	}
}
