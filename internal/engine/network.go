package engine

import (
	"context"
	crand "crypto/rand"
	"fmt"
	"strconv"
	"strings"

	clerrors "github.com/shatim-ops/cloudru-vm-cli/internal/errors"
	"github.com/shatim-ops/cloudru-vm-cli/internal/provision"
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
		return n, fmt.Errorf("list subnets: %w", err)
	}
	if sn := pickInZone(len(subnets), func(i int) (string, string) { return subnets[i].AvailabilityZoneID, subnets[i].Name }, zoneID, n.ZoneName); sn >= 0 {
		n.SubnetID, n.SubnetName = subnets[sn].ID, subnets[sn].Name
	} else {
		return n, clerrors.New(clerrors.CodeConfigInvalid,
			fmt.Sprintf("no subnet for zone %s in the project", zoneText(n.ZoneName, zoneID)),
			"Create a network and a subnet in this zone in the Cloud.ru console, or pick another zone with 'cloudru-vm init'.")
	}

	sgs, err := e.Cloud.ListSecurityGroups(ctx, projectID)
	if err != nil {
		return n, fmt.Errorf("list security groups: %w", err)
	}
	if g := pickInZone(len(sgs), func(i int) (string, string) { return sgs[i].AvailabilityZoneID, sgs[i].Name }, zoneID, n.ZoneName); g >= 0 {
		n.SecurityGroupID, n.SecurityGroup = sgs[g].ID, sgs[g].Name
	} else {
		return n, clerrors.New(clerrors.CodeConfigInvalid,
			fmt.Sprintf("no security group for zone %s in the project", zoneText(n.ZoneName, zoneID)),
			"Create a security group whose name contains the zone name (e.g. 'web_ru.AZ-1') in the Cloud.ru console.")
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
// VM's security group, skipping ports already open for that protocol. The
// group is shared by every VM placed in it — the deploy preview says so
// before anything is created. Failures are reported, not fatal: the VM
// exists and the ports can be opened in the console.
func (e *Engine) openPorts(ctx context.Context, projectID, sgID string, tcp, udp []int) {
	if sgID == "" || len(tcp)+len(udp) == 0 {
		return
	}
	// Keyed by protocol and range: a udp rule on 53 does not open tcp 53. A
	// rule without a protocol (or "any") covers both.
	existing := map[string]bool{}
	if sgs, err := e.Cloud.ListSecurityGroups(ctx, projectID); err == nil {
		for _, sg := range sgs {
			if sg.ID != sgID {
				continue
			}
			for _, r := range sg.Rules {
				if r.Direction != "ingress" || r.PortRange == "" {
					continue
				}
				switch proto := strings.ToLower(r.IPProtocol); proto {
				case "", "any":
					existing["tcp "+r.PortRange] = true
					existing["udp "+r.PortRange] = true
				default:
					existing[proto+" "+r.PortRange] = true
				}
			}
		}
	}
	var opened, failed []string
	for _, set := range []struct {
		proto string
		ports []int
	}{{"tcp", tcp}, {"udp", udp}} {
		for _, p := range set.ports {
			pr := fmt.Sprintf("%d:%d", p, p)
			if existing[set.proto+" "+pr] {
				continue
			}
			existing[set.proto+" "+pr] = true
			label := strconv.Itoa(p)
			if set.proto != "tcp" {
				label += "/" + set.proto
			}
			rule := provision.SGRule{
				Direction:      "ingress",
				EtherType:      "IPv4",
				IPProtocol:     set.proto,
				PortRange:      pr,
				RemoteIPPrefix: "0.0.0.0/0",
				Description:    fmt.Sprintf("cloudru-vm: port %s", label),
			}
			if err := e.Cloud.AddSecurityGroupRule(ctx, sgID, projectID, rule); err != nil {
				failed = append(failed, label)
			} else {
				opened = append(opened, label)
			}
		}
	}
	if len(opened) > 0 {
		e.Printer.StepDone("Opened ports " + strings.Join(opened, ",") + " in the security group")
	}
	if len(failed) > 0 {
		e.Printer.Warning("Could not open ports " + strings.Join(failed, ",") + " — open them in the security group in the Cloud.ru console")
	}
}

// flavorAPIName turns the flavor picked by sizing (an ID) or given by the
// user (an ID or a name) into the name the create API expects: it rejects
// UUIDs in flavor_name with 422.
func (e *Engine) flavorAPIName(ctx context.Context, ref string) (string, error) {
	flavors, err := e.Cloud.ListFlavors(ctx)
	if err != nil {
		return "", fmt.Errorf("list flavors: %w", err)
	}
	for _, f := range flavors {
		if f.ID == ref || f.Name == ref {
			return f.Name, nil
		}
	}
	return "", clerrors.New(clerrors.CodeConfigInvalid,
		fmt.Sprintf("flavor %q not found", ref),
		"Use 'flavor: auto' or a flavor name from the Cloud.ru console.")
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
