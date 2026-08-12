package mcp

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/shatim-ops/cloudru-vm-cli/internal/deployplan"
	"github.com/shatim-ops/cloudru-vm-cli/internal/engine"
	"github.com/shatim-ops/cloudru-vm-cli/internal/pricing"
	"github.com/shatim-ops/cloudru-vm-cli/internal/provision"
	"github.com/shatim-ops/cloudru-vm-cli/internal/state"
)

// authedEngine builds an engine and sets a fresh bearer token on its cloud
// client, so any handler can make Cloud.ru API calls.
func (s *Server) authedEngine(ctx context.Context, overrides map[string]any) (*engine.Engine, error) {
	eng, err := s.newEngine(overrides)
	if err != nil {
		return nil, err
	}
	if eng == nil {
		return nil, fmt.Errorf("engine unavailable")
	}
	tok, err := eng.Auth.Token(ctx)
	if err != nil {
		return nil, err
	}
	eng.Cloud.SetToken(tok.Value)
	return eng, nil
}

// handleDeployPlan resolves a plan (read-only) and returns it with a cost
// estimate and a plan_id. Creates nothing. If base_plan_id is given, it starts
// from that plan's parameters and overlays only the provided fields (a patch),
// so the captain can tweak one value without restating everything.
func (s *Server) handleDeployPlan(ctx context.Context, args map[string]any) (string, bool) {
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}

	// Base parameters: a fresh request, or a patch on an existing plan.
	var params deployplan.Params
	if base := stringArg(args, "base_plan_id"); base != "" {
		bp, lErr := deployplan.Load(eng.StateDir, base)
		if lErr != nil {
			return errResult(lErr), true
		}
		params = bp.SourceParams
	} else {
		params.ExternalIP = true // default for a fresh plan
	}

	// Overlay only the fields actually present in this call.
	overlayString(args, "name", &params.Name)
	overlayString(args, "product", &params.Product)
	overlayString(args, "mode", &params.Mode)
	overlayInt(args, "cpu", &params.CPU)
	overlayInt(args, "ram_gb", &params.RAMGB)
	overlayInt(args, "disk_gb", &params.DiskGB)
	overlayString(args, "compose_file", &params.ComposeFile)
	overlayString(args, "flavor", &params.Flavor)
	overlayString(args, "zone", &params.Zone)
	overlayString(args, "image", &params.Image)
	overlayString(args, "disk_type", &params.DiskType)
	overlayString(args, "login", &params.Login)
	overlayString(args, "ssh_key", &params.SSHKey)
	if v, ok := args["external_ip"].(bool); ok {
		params.ExternalIP = v
	}
	if _, ok := args["ports"]; ok {
		params.Ports = parseInts(args["ports"])
	}

	// Default to the AGENT's deploy key so the agent can SSH in to install
	// software (the image is pubkey-only; password SSH is disabled). The
	// captain's own key is added to authorized_keys post-install (see SKILL).
	// An explicit ssh_key arg overrides this.
	if params.SSHKey == "" {
		params.SSHKey = eng.SSHPubKey
	}

	plan, err := deployplan.Resolve(ctx, eng.Cloud, eng.Config.ProjectID, params)
	if err != nil {
		return errResult(err), true
	}
	if err := deployplan.Save(eng.StateDir, plan); err != nil {
		return errResult(err), true
	}

	// Compact, captain-facing view (omit internal fields like meta_fields/public_key).
	view := map[string]any{
		"plan_id":          plan.PlanID,
		"expires_at":       plan.ExpiresAt,
		"product":          plan.Product,
		"mode":             plan.Mode,
		"vm":               plan.VM,
		"network":          map[string]any{"floating_ip": plan.Network.FloatingIP, "open_ports": plan.Ports},
		"ssh":              plan.SSH,
		"cost":             plan.Cost,
		"warnings":         plan.Warnings,
		"compose_required": plan.ComposeRequired,
		"next":             "Show this plan and its monthly cost to the captain. On explicit approval, call deploy_apply with this plan_id and confirmed=true.",
	}
	return toJSON(view), false
}

// overlayString sets *dst from args[key] only if the key is present.
func overlayString(args map[string]any, key string, dst *string) {
	if _, ok := args[key]; ok {
		*dst = stringArg(args, key)
	}
}

// overlayInt sets *dst from args[key] only if the key is present.
func overlayInt(args map[string]any, key string, dst *int) {
	if _, ok := args[key]; ok {
		*dst = intArg(args, key, *dst)
	}
}

// handleDeployApply executes a previously-resolved plan, gated on confirmation.
func (s *Server) handleDeployApply(ctx context.Context, args map[string]any) (string, bool) {
	planID := stringArg(args, "plan_id")
	if planID == "" {
		return errResult(fmt.Errorf("plan_id is required")), true
	}
	confirmed, _ := args["confirmed"].(bool)
	if !confirmed {
		return toJSON(map[string]any{
			"status":  "confirmation_required",
			"message": "deploy_apply requires confirmed=true. Obtain the captain's explicit approval of the plan and its cost first, then call again with confirmed=true.",
		}), true
	}

	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	plan, err := deployplan.Load(eng.StateDir, planID)
	if err != nil {
		return errResult(err), true
	}
	res, err := deployplan.Apply(ctx, eng.Cloud, plan)
	if err != nil {
		return errResult(err), true
	}
	deployplan.Delete(eng.StateDir, planID)

	// Record the VM in local state so status/list/destroy work on it.
	if res.VMID != "" {
		statePath := state.DefaultPath(eng.StateDir)
		if sf, e := state.Read(statePath); e == nil {
			now := time.Now()
			sf.UpsertDeployment(state.DeploymentRecord{
				ID:        res.VMID,
				VMID:      res.VMID,
				VMIP:      res.PublicIP,
				VMFlavor:  plan.VM.Flavor.Name,
				Region:    plan.VM.Zone.ID,
				SSHUser:   plan.SSH.Login,
				Status:    res.Status,
				CreatedAt: now,
				UpdatedAt: now,
			})
			_ = state.Write(statePath, sf)
		}
	}

	// If we injected the agent's own deploy key, hand back an ssh command that
	// uses it (and tolerates the reused-IP host-key change on ephemeral VMs).
	if res.PublicIP != "" && plan.PublicKey == eng.SSHPubKey {
		keyPath := filepath.Join(eng.StateDir, ".cloudru-vm", "id_ed25519")
		res.SSH = fmt.Sprintf("ssh -i %s -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null %s@%s", keyPath, plan.SSH.Login, res.PublicIP)
		note := "Connect with the ssh command above (agent deploy key)."
		if keys := loadCaptainKeys(eng.StateDir); len(keys) > 0 {
			if kErr := appendCaptainKeys(ctx, res.PublicIP, plan.SSH.Login, eng.SSHKeyPEM, keys); kErr != nil {
				note += fmt.Sprintf(" WARNING: captain key auto-append failed (%v) — append their public key to ~/.ssh/authorized_keys on the VM manually.", kErr)
			} else {
				note += fmt.Sprintf(" Captain SSH access configured automatically (%d key(s) from .cloudru-vm/%s).", len(keys), captainKeysFile)
			}
		} else {
			note += " No .cloudru-vm/" + captainKeysFile + " configured — to give the captain SSH access, append their public key to ~/.ssh/authorized_keys on the VM after install."
		}
		if res.Note == "" {
			res.Note = note
		} else {
			res.Note = res.Note + "; " + note
		}
	}
	return toJSON(res), false
}

// handleListFlavors lists flavors with a monthly cost estimate each.
func (s *Server) handleListFlavors(ctx context.Context, args map[string]any) (string, bool) {
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	flavors, err := eng.Cloud.ListFlavors(ctx)
	if err != nil {
		return errResult(err), true
	}
	out := make([]map[string]any, 0, len(flavors))
	for _, f := range flavors {
		est := pricing.EstimateFor(f.Name, f.VCPUs, f.RAMGB, 10, "SSD")
		out = append(out, map[string]any{
			"name":            f.Name,
			"vcpus":           f.VCPUs,
			"ram_gb":          f.RAMGB,
			"free_tier":       f.FreeTier,
			"est_monthly_rub": est.TotalMonthlyRub,
		})
	}
	return toJSON(map[string]any{
		"flavors": out,
		"note":    "est_monthly_rub is a rate-card estimate incl. 10GB SSD, not a billed price",
	}), false
}

// handleListDiskTypes lists disk types (id + name).
func (s *Server) handleListDiskTypes(ctx context.Context, args map[string]any) (string, bool) {
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	dts, err := eng.Cloud.ListDiskTypes(ctx, eng.Config.ProjectID)
	if err != nil {
		return errResult(err), true
	}
	out := make([]map[string]any, 0, len(dts))
	for _, d := range dts {
		out = append(out, map[string]any{"id": d.ID, "name": d.Name})
	}
	return toJSON(map[string]any{"disk_types": out}), false
}

// handleListSubnets lists subnets (id, name, zone).
func (s *Server) handleListSubnets(ctx context.Context, args map[string]any) (string, bool) {
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	subnets, err := eng.Cloud.ListSubnets(ctx, eng.Config.ProjectID)
	if err != nil {
		return errResult(err), true
	}
	out := make([]map[string]any, 0, len(subnets))
	for _, s := range subnets {
		out = append(out, map[string]any{"id": s.ID, "name": s.Name, "availability_zone_id": s.AvailabilityZoneID})
	}
	return toJSON(map[string]any{"subnets": out}), false
}

// handleListSecurityGroups lists security groups (id, name, zone).
func (s *Server) handleListSecurityGroups(ctx context.Context, args map[string]any) (string, bool) {
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	sgs, err := eng.Cloud.ListSecurityGroups(ctx, eng.Config.ProjectID)
	if err != nil {
		return errResult(err), true
	}
	out := make([]map[string]any, 0, len(sgs))
	for _, g := range sgs {
		out = append(out, map[string]any{"id": g.ID, "name": g.Name, "availability_zone_id": g.AvailabilityZoneID})
	}
	return toJSON(map[string]any{"security_groups": out}), false
}

// handleOpenPorts adds ingress rules for the given ports to a security group.
func (s *Server) handleOpenPorts(ctx context.Context, args map[string]any) (string, bool) {
	sgRef := stringArg(args, "security_group")
	ports := parseInts(args["ports"])
	if sgRef == "" || len(ports) == 0 {
		return errResult(fmt.Errorf("security_group and ports are required")), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status":  "confirmation_required",
			"message": fmt.Sprintf("open_ports would open %v on security group %q (shared per zone — affects every VM in it). Get the captain's approval, then call again with confirmed=true.", ports, sgRef),
		}), true
	}

	proto := stringArg(args, "protocol")
	if proto == "" {
		proto = "tcp"
	}
	cidr := stringArg(args, "cidr")
	if cidr == "" {
		cidr = "0.0.0.0/0"
	}

	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	sgs, err := eng.Cloud.ListSecurityGroups(ctx, eng.Config.ProjectID)
	if err != nil {
		return errResult(err), true
	}
	var target *provision.SecurityGroup
	for _, g := range sgs {
		if g.ID == sgRef || g.Name == sgRef {
			target = g
			break
		}
	}
	if target == nil {
		return errResult(fmt.Errorf("security group %q not found", sgRef)), true
	}

	existing := map[string]bool{}
	for _, r := range target.Rules {
		if r.Direction == "ingress" && r.PortRange != "" {
			existing[r.PortRange] = true
		}
	}

	var opened, skipped, failed []string
	for _, p := range ports {
		pr := fmt.Sprintf("%d:%d", p, p)
		if existing[pr] {
			skipped = append(skipped, strconv.Itoa(p))
			continue
		}
		rule := provision.SGRule{
			Direction:      "ingress",
			EtherType:      "IPv4",
			IPProtocol:     proto,
			PortRange:      pr,
			RemoteIPPrefix: cidr,
			Description:    "opened via open_ports",
		}
		if e := eng.Cloud.AddSecurityGroupRule(ctx, target.ID, eng.Config.ProjectID, rule); e != nil {
			failed = append(failed, strconv.Itoa(p))
		} else {
			opened = append(opened, strconv.Itoa(p))
		}
	}
	return toJSON(map[string]any{
		"status":         "ok",
		"security_group": target.Name,
		"protocol":       proto,
		"cidr":           cidr,
		"opened":         opened,
		"already_open":   skipped,
		"failed":         failed,
	}), false
}

// parseInts extracts a []int from a JSON array of numbers, a []int, or a
// comma-separated string.
func parseInts(v any) []int {
	var out []int
	switch t := v.(type) {
	case []any:
		for _, e := range t {
			switch n := e.(type) {
			case float64:
				out = append(out, int(n))
			case int:
				out = append(out, n)
			case string:
				if x, err := strconv.Atoi(strings.TrimSpace(n)); err == nil {
					out = append(out, x)
				}
			}
		}
	case []int:
		out = t
	case string:
		for _, part := range strings.Split(t, ",") {
			if x, err := strconv.Atoi(strings.TrimSpace(part)); err == nil {
				out = append(out, x)
			}
		}
	}
	return out
}

// handleReleaseFloatingIP deletes a floating IP, refusing if it's in use.
func (s *Server) handleReleaseFloatingIP(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "id")
	ip := stringArg(args, "ip_address")
	if id == "" && ip == "" {
		return errResult(fmt.Errorf("provide either 'id' or 'ip_address'")), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status":  "confirmation_required",
			"message": "release_floating_ip requires confirmed=true. Get the captain's approval to release this IP first, then call again with confirmed=true.",
		}), true
	}

	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	projectID := eng.Config.ProjectID

	// Resolve to a concrete FIP (id, address, state).
	var fipID, fipIP, state string
	if id != "" {
		f, e := eng.Cloud.GetFloatingIP(ctx, id)
		if e != nil {
			return errResult(fmt.Errorf("floating IP %q not found: %w", id, e)), true
		}
		fipID, fipIP, state = id, f.IPAddress, f.State
	} else {
		fips, e := eng.Cloud.ListFloatingIPs(ctx, projectID)
		if e != nil {
			return errResult(e), true
		}
		for _, f := range fips {
			if f.IPAddress == ip {
				fipID, fipIP, state = f.ID, f.IPAddress, f.State
				break
			}
		}
		if fipID == "" {
			return errResult(fmt.Errorf("no floating IP with address %q in this project", ip)), true
		}
	}

	// Safety: never release an IP that is attached to a VM.
	if state == "in_use" {
		return toJSON(map[string]any{
			"status":  "refused",
			"message": fmt.Sprintf("floating IP %s (%s) is in use by a VM — detach it or destroy the VM first; not released", fipIP, fipID),
		}), true
	}

	if err := eng.Cloud.DeleteFloatingIP(ctx, fipID, projectID); err != nil {
		return errResult(fmt.Errorf("release floating IP: %w", err)), true
	}
	return toJSON(map[string]any{
		"status":   "ok",
		"released": fipIP,
		"id":       fipID,
		"message":  fmt.Sprintf("floating IP %s released", fipIP),
	}), false
}

// handleListFloatingIPs lists floating IPs (address, state, availability).
func (s *Server) handleListFloatingIPs(ctx context.Context, args map[string]any) (string, bool) {
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	fips, err := eng.Cloud.ListFloatingIPs(ctx, eng.Config.ProjectID)
	if err != nil {
		return errResult(err), true
	}
	out := make([]map[string]any, 0, len(fips))
	for _, f := range fips {
		out = append(out, map[string]any{
			"id":         f.ID,
			"ip_address": f.IPAddress,
			"state":      f.State,
			"available":  f.Available(),
		})
	}
	return toJSON(map[string]any{"floating_ips": out}), false
}
