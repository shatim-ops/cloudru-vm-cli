package mcp

import (
	"context"
	"fmt"

	"github.com/dzhechko/cloudru-vm-cli/internal/engine"
	"github.com/dzhechko/cloudru-vm-cli/internal/nlb"
	"github.com/dzhechko/cloudru-vm-cli/internal/provision"
)

// pickVMInterface returns the ID of a VM's network interface to use as an NLB
// target — the primary one if flagged, else the first with a non-empty ID.
func pickVMInterface(det *provision.VMDetail) string {
	if det == nil {
		return ""
	}
	for _, ni := range det.Interfaces {
		if ni.Primary && ni.ID != "" {
			return ni.ID
		}
	}
	for _, ni := range det.Interfaces {
		if ni.ID != "" {
			return ni.ID
		}
	}
	return ""
}

func (s *Server) nlbClient(ctx context.Context) (*nlb.Client, error) {
	cli, _, err := s.nlbClientEng(ctx)
	return cli, err
}

// nlbClientEng also returns the engine (subnet resolution). Uses authedEngine so
// eng.Cloud carries a bearer (newEngine would 403 "RBAC: access denied").
func (s *Server) nlbClientEng(ctx context.Context) (*nlb.Client, *engine.Engine, error) {
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	tok, err := eng.Auth.Token(ctx)
	if err != nil {
		return nil, nil, err
	}
	return nlb.New(tok.Value, eng.Config.ProjectID, ""), eng, nil
}

// --- Load balancers: read ---

func (s *Server) handleLbList(ctx context.Context, args map[string]any) (string, bool) {
	cli, err := s.nlbClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	items, err := cli.ListNLB(ctx)
	if err != nil {
		return errResult(err), true
	}
	out := make([]map[string]any, 0, len(items))
	for _, m := range items {
		row := map[string]any{"id": m["id"], "name": m["name"], "status": m["status"], "vpc_id": m["vpc_id"]}
		if ext, ok := m["external_address"].(map[string]any); ok {
			row["external_ip"] = ext["ipv4"]
		}
		out = append(out, row)
	}
	return toJSON(map[string]any{"load_balancers": out}), false
}

func (s *Server) handleLbGet(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "id")
	if id == "" {
		return errResult(fmt.Errorf("'id' is required (from lb_list)")), true
	}
	cli, err := s.nlbClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	m, err := cli.GetNLB(ctx, id)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(m), false
}

func (s *Server) handleLbOperation(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "operation_id")
	if id == "" {
		return errResult(fmt.Errorf("'operation_id' is required")), true
	}
	cli, err := s.nlbClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	if w := pgInt(args, "wait_seconds", 0); w > 0 {
		m, err := waitForOpJSON(ctx, w, func(ctx context.Context) (map[string]any, error) {
			op, err := cli.GetOperation(ctx, id)
			if err != nil {
				return nil, err
			}
			return opToMap(op), nil
		})
		if err != nil {
			return errResult(err), true
		}
		return toJSON(m), false
	}
	op, err := cli.GetOperation(ctx, id)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(op), false
}

// --- Target groups ---

func (s *Server) handleTgList(ctx context.Context, args map[string]any) (string, bool) {
	cli, err := s.nlbClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	items, err := cli.ListTargetGroups(ctx)
	if err != nil {
		return errResult(err), true
	}
	out := make([]map[string]any, 0, len(items))
	for _, m := range items {
		out = append(out, map[string]any{"id": m["id"], "name": m["name"], "status": m["status"], "vpc_id": m["vpc_id"]})
	}
	return toJSON(map[string]any{"target_groups": out}), false
}

func (s *Server) handleTgGet(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "id")
	if id == "" {
		return errResult(fmt.Errorf("'id' is required (from tg_list)")), true
	}
	cli, err := s.nlbClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	m, err := cli.GetTargetGroup(ctx, id)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(m), false
}

// targetSpecs builds target_vm_specs from a `targets` array and/or vm_id+interface_id.
func targetSpecs(args map[string]any) []map[string]any {
	var specs []map[string]any
	if raw, ok := args["targets"].([]any); ok {
		for _, t := range raw {
			if m, ok := t.(map[string]any); ok {
				if id, _ := m["id"].(string); id != "" {
					specs = append(specs, map[string]any{"id": id, "interface_id": m["interface_id"]})
				}
			}
		}
	}
	if vm := stringArg(args, "vm_id"); vm != "" {
		specs = append(specs, map[string]any{"id": vm, "interface_id": stringArg(args, "interface_id")})
	}
	return specs
}

func (s *Server) handleTgCreate(ctx context.Context, args map[string]any) (string, bool) {
	name := stringArg(args, "name")
	if name == "" {
		return errResult(fmt.Errorf("'name' is required")), true
	}
	specs := targetSpecs(args)
	if len(specs) == 0 {
		return errResult(fmt.Errorf("provide targets (array of {id[, interface_id]}) or vm_id")), true
	}
	cli, eng, err := s.nlbClientEng(ctx)
	if err != nil {
		return errResult(err), true
	}
	// Auto-resolve interface_id from the VM when the caller only gave vm_id — the
	// agent has no standalone tool to fetch a VM's network interface, and without
	// this it flails (ssh/raw API). NLB targets need the VM's regular VPC-subnet
	// interface; GetVMDetail returns it.
	for i, sp := range specs {
		if ifid, _ := sp["interface_id"].(string); ifid == "" {
			vmid, _ := sp["id"].(string)
			det, e := eng.Cloud.GetVMDetail(ctx, vmid)
			if e != nil {
				return errResult(fmt.Errorf("resolve interface for vm %s: %w", vmid, e)), true
			}
			ifid := pickVMInterface(det)
			if ifid == "" {
				return errResult(fmt.Errorf("vm %s has no usable network interface (NLB targets need a private VPC-subnet VM — provision with no_public_ip)", vmid)), true
			}
			specs[i]["interface_id"] = ifid
		}
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status":  "confirmation_required",
			"message": fmt.Sprintf("tg_create creates target group %q with %d target(s). Call again with confirmed=true.", name, len(specs)),
			"targets": specs,
		}), true
	}
	body := map[string]any{"project_id": cli.ProjectID(), "name": name, "target_vm_specs": specs}
	op, err := cli.CreateTargetGroup(ctx, body)
	if err != nil {
		return errResult(fmt.Errorf("create target group: %w", err)), true
	}
	return toJSON(map[string]any{"status": "creating", "operation_id": op.ID, "note": "Async — poll lb_operation(operation_id) until done=true; then tg_list for the id."}), false
}

func (s *Server) handleTgDelete(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "id")
	if id == "" {
		return errResult(fmt.Errorf("'id' is required (from tg_list)")), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{"status": "confirmation_required", "message": fmt.Sprintf("tg_delete deletes target group %s. Call again with confirmed=true.", id)}), true
	}
	cli, err := s.nlbClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	op, err := cli.DeleteTargetGroup(ctx, id)
	if err != nil {
		return errResult(fmt.Errorf("delete target group: %w", err)), true
	}
	return toJSON(map[string]any{"status": "deleting", "id": id, "operation_id": op.ID}), false
}

// --- Load balancer create/delete ---

func (s *Server) handleLbCreate(ctx context.Context, args map[string]any) (string, bool) {
	name := stringArg(args, "name")
	tgID := stringArg(args, "target_group_id")
	port := pgInt(args, "port", 0)
	if name == "" || tgID == "" || port == 0 {
		return errResult(fmt.Errorf("'name', 'target_group_id' and 'port' are required")), true
	}
	targetPort := pgInt(args, "target_port", port)
	hcPort := pgInt(args, "hc_port", targetPort)
	external := true
	if v, ok := args["external"].(bool); ok {
		external = v
	}

	cli, eng, err := s.nlbClientEng(ctx)
	if err != nil {
		return errResult(err), true
	}

	// VPC: explicit, else inherit from the target group (LB must share its VPC).
	vpcID := stringArg(args, "vpc_id")
	if vpcID == "" {
		tg, e := cli.GetTargetGroup(ctx, tgID)
		if e != nil {
			return errResult(fmt.Errorf("resolve vpc from target group: %w", e)), true
		}
		vpcID, _ = tg["vpc_id"].(string)
	}
	if vpcID == "" {
		return errResult(fmt.Errorf("could not resolve vpc_id — pass it explicitly")), true
	}

	// Subnet for the AZ spec (and internal address). Explicit, else first project subnet.
	subnetID := stringArg(args, "subnet_id")
	if subnetID == "" {
		subs, e := eng.Cloud.ListSubnets(ctx, eng.Config.ProjectID)
		if e != nil {
			return errResult(fmt.Errorf("resolve subnet: %w", e)), true
		}
		if len(subs) == 0 {
			return errResult(fmt.Errorf("no subnets in project — pass subnet_id explicitly")), true
		}
		subnetID = subs[0].ID
	}

	// AZ spec requires exactly one of {id, name} (nlb API 400 otherwise). Resolve
	// the human zone name (e.g. "ru.AZ-2") from the configured region UUID — zone
	// NAMES are the canonical cross-service identifier; fall back to the id.
	azSpec := map[string]any{"subnet_ids": []string{subnetID}}
	region := eng.Config.Region
	zoneName := ""
	if zones, e := eng.Cloud.ListAvailabilityZones(ctx); e == nil {
		for _, z := range zones {
			if z.ID == region {
				zoneName = z.Name
				break
			}
		}
	}
	if zoneName != "" {
		azSpec["name"] = zoneName
	} else if region != "" {
		azSpec["id"] = region
	}

	rule := map[string]any{
		"name":            "rule-" + name,
		"target_group_id": tgID,
		"listeners":       []map[string]any{{"name": fmt.Sprintf("lst-%d", port), "port": port, "target_port": targetPort}},
		"health_check": map[string]any{
			"port": hcPort, "timeout": "5s", "interval": "10s",
			"unhealthy_threshold": 3, "healthy_threshold": 3, "tcp": map[string]any{},
		},
	}
	body := map[string]any{
		"project_id":              cli.ProjectID(),
		"name":                    name,
		"vpc_id":                  vpcID,
		"rules":                   []map[string]any{rule},
		"availability_zone_specs": []map[string]any{azSpec},
	}
	if external {
		body["external_address_spec"] = map[string]any{"allocate": true}
	} else {
		body["internal_address_spec"] = map[string]any{"allocate": true, "subnet_id": subnetID}
	}

	willCreate := map[string]any{
		"name": name, "vpc_id": vpcID, "target_group_id": tgID,
		"listener": fmt.Sprintf("%d→%d", port, targetPort), "health_check_port": hcPort,
		"subnet_id": subnetID, "external": external,
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status":      "confirmation_required",
			"message":     "lb_create provisions a BILLABLE L4 load balancer (async). Get the captain's approval, then call again with confirmed=true.",
			"will_create": willCreate,
		}), true
	}
	op, err := cli.CreateNLB(ctx, body)
	if err != nil {
		return errResult(fmt.Errorf("create load balancer: %w", err)), true
	}
	return toJSON(map[string]any{"status": "creating", "operation_id": op.ID, "created": willCreate, "note": "Async — poll lb_operation(operation_id) until done=true; then lb_get for the external IP + status RUNNING."}), false
}

func (s *Server) handleLbDelete(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "id")
	if id == "" {
		return errResult(fmt.Errorf("'id' is required (from lb_list)")), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{"status": "confirmation_required", "message": fmt.Sprintf("lb_delete deletes load balancer %s. Call again with confirmed=true.", id)}), true
	}
	cli, err := s.nlbClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	op, err := cli.DeleteNLB(ctx, id)
	if err != nil {
		return errResult(fmt.Errorf("delete load balancer: %w", err)), true
	}
	return toJSON(map[string]any{"status": "deleting", "id": id, "operation_id": op.ID}), false
}
