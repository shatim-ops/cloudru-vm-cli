package tools

import (
	"context"
	"fmt"
	"net/url"
	"sort"

	"github.com/shatim-ops/evo-cli/internal/dbaas"
	"github.com/shatim-ops/evo-cli/internal/engine"
)

const kafkaHost = "https://kafka.api.cloud.ru"

// firstArg returns the first non-empty string arg among keys.
func firstArg(args map[string]any, keys ...string) string {
	for _, k := range keys {
		if v := stringArg(args, k); v != "" {
			return v
		}
	}
	return ""
}

func boolArg(args map[string]any, key string, def bool) bool {
	if v, ok := args[key].(bool); ok {
		return v
	}
	return def
}

// findDbaasSpec picks a spec by id, or by cpu+memory, else the first (cheapest
// after the caller sorted). Works for Kafka/Redis specs (cpu/memory fields).
func findDbaasSpec(specs []map[string]any, specID string, cpu, ram int) map[string]any {
	if specID != "" {
		for _, sp := range specs {
			if id, _ := sp["id"].(string); id == specID {
				return sp
			}
		}
		return nil
	}
	if cpu > 0 && ram > 0 {
		for _, sp := range specs {
			if int(toF(sp["cpu"])) == cpu && int(toF(sp["memory"])) == ram {
				return sp
			}
		}
		return nil
	}
	if len(specs) > 0 {
		// Default to the CHEAPEST (fewest cpu, then memory) — the API returns
		// specs unsorted, so specs[0] is arbitrary.
		sorted := make([]map[string]any, len(specs))
		copy(sorted, specs)
		sort.SliceStable(sorted, func(i, j int) bool {
			ci, cj := toF(sorted[i]["cpu"]), toF(sorted[j]["cpu"])
			if ci != cj {
				return ci < cj
			}
			return toF(sorted[i]["memory"]) < toF(sorted[j]["memory"])
		})
		return sorted[0]
	}
	return nil
}

func (s *Registry) kafkaClientEng(ctx context.Context) (*dbaas.Client, *engine.Engine, error) {
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	tok, err := eng.Auth.Token(ctx)
	if err != nil {
		return nil, nil, err
	}
	return dbaas.New(tok.Value, eng.Config.ProjectID, kafkaHost), eng, nil
}

func kafkaQuery() url.Values {
	q := url.Values{}
	q.Set("product_type", "kafka")
	return q
}

func (s *Registry) handleKafkaList(ctx context.Context, args map[string]any) (string, bool) {
	cli, _, err := s.kafkaClientEng(ctx)
	if err != nil {
		return errResult(err), true
	}
	items, err := cli.ListClusters(ctx, kafkaQuery())
	if err != nil {
		return errResult(err), true
	}
	out := make([]map[string]any, 0, len(items))
	for _, m := range items {
		out = append(out, map[string]any{
			"id": m["id"], "name": m["name"], "status": m["status"],
			"version": m["version_id"], "brokers": m["brokers"], "health": m["health"],
		})
	}
	return toJSON(map[string]any{"clusters": out}), false
}

func (s *Registry) handleKafkaGet(ctx context.Context, args map[string]any) (string, bool) {
	id := firstArg(args, "cluster_id", "id")
	if id == "" {
		return usageResult(fmt.Errorf("'cluster_id' is required (from kafka_list)")), true
	}
	cli, _, err := s.kafkaClientEng(ctx)
	if err != nil {
		return errResult(err), true
	}
	m, err := cli.GetCluster(ctx, id)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(m), false
}

func (s *Registry) handleKafkaVersions(ctx context.Context, args map[string]any) (string, bool) {
	cli, _, err := s.kafkaClientEng(ctx)
	if err != nil {
		return errResult(err), true
	}
	items, err := cli.ListVersions(ctx, kafkaQuery())
	if err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{"versions": items}), false
}

func (s *Registry) handleKafkaSpecs(ctx context.Context, args map[string]any) (string, bool) {
	cli, eng, err := s.kafkaClientEng(ctx)
	if err != nil {
		return errResult(err), true
	}
	versionID := stringArg(args, "version_id")
	if versionID == "" {
		if vers, e := cli.ListVersions(ctx, kafkaQuery()); e == nil && len(vers) > 0 {
			versionID, _ = vers[len(vers)-1]["id"].(string)
		}
	}
	items, err := cli.ListSpecifications(ctx, versionID, eng.Config.Region)
	if err != nil {
		return errResult(err), true
	}
	out, trunc := compactSpecs(items, specMenuCap)
	res := map[string]any{"resolved_version_id": versionID, "specifications": out}
	if trunc > 0 {
		res["specifications_truncated"] = trunc
	}
	return toJSON(res), false
}

func (s *Registry) handleKafkaCreate(ctx context.Context, args map[string]any) (string, bool) {
	name := stringArg(args, "name")
	if name == "" {
		return usageResult(fmt.Errorf("'name' is required (2-63 chars)")), true
	}
	cli, eng, err := s.kafkaClientEng(ctx)
	if err != nil {
		return errResult(err), true
	}

	versionID := stringArg(args, "version_id")
	if versionID == "" {
		vers, e := cli.ListVersions(ctx, kafkaQuery())
		if e != nil {
			return errResult(fmt.Errorf("list versions: %w", e)), true
		}
		if len(vers) == 0 {
			return errResult(fmt.Errorf("no kafka versions available")), true
		}
		versionID, _ = vers[len(vers)-1]["id"].(string)
	}

	specs, err := cli.ListSpecifications(ctx, versionID, eng.Config.Region)
	if err != nil {
		return errResult(fmt.Errorf("list specifications: %w", err)), true
	}
	spec := findDbaasSpec(specs, stringArg(args, "specification_id"), pgInt(args, "cpu", 0), pgInt(args, "ram", 0))
	if spec == nil {
		menu, trunc := compactSpecs(specs, specMenuCap)
		out := map[string]any{"status": "need_specification", "resolved_version_id": versionID,
			"message": "kafka_create needs a specification: pass specification_id, or cpu+ram matching one of these.", "available_specifications": menu}
		if trunc > 0 {
			out["specifications_truncated"] = trunc
		}
		return toJSON(out), false
	}
	specID, _ := spec["id"].(string)

	storage := pgInt(args, "storage_gib", 0)
	if storage == 0 {
		storage = int(toF(spec["min_storage_gigs"]))
	}
	if storage == 0 {
		storage = 40
	}
	brokers := pgInt(args, "brokers", 0)
	if brokers == 0 {
		brokers = int(toF(spec["min_brokers"]))
	}
	if brokers == 0 {
		brokers = 1
	}

	subnetID := stringArg(args, "subnet_id")
	if subnetID == "" {
		subs, e := eng.Cloud.ListSubnets(ctx, eng.Config.ProjectID)
		if e != nil || len(subs) == 0 {
			return errResult(fmt.Errorf("resolve subnet: pass subnet_id explicitly")), true
		}
		subnetID = subs[0].ID
	}

	willCreate := map[string]any{"name": name, "version_id": versionID, "specification_id": specID,
		"cpu": spec["cpu"], "memory_gb": spec["memory"], "storage_gib": storage, "brokers": brokers, "subnet_id": subnetID}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{"status": "confirmation_required",
			"message":     "kafka_create provisions a BILLABLE Managed Kafka cluster (async, ~minutes). Get the user's approval, then call again with confirmed=true.",
			"will_create": willCreate}), true
	}

	body := map[string]any{
		"name": name, "project_id": cli.ProjectID(), "version_id": versionID,
		"specification_id": specID, "subnet_id": subnetID, "storage_gib": storage, "brokers": brokers,
		"ui_enabled": boolArg(args, "ui_enabled", false), "cruise_control_enabled": false,
		"cruise_control_rebalance_auto_enabled": false,
	}
	if d := stringArg(args, "description"); d != "" {
		body["description"] = d
	}
	op, err := cli.CreateCluster(ctx, body)
	if err != nil {
		return errResult(fmt.Errorf("create cluster: %w", err)), true
	}
	return toJSON(map[string]any{"status": "creating", "operation_id": op.ID, "cluster_id": op.ResourceID,
		"created": willCreate, "note": "Async (~minutes). Poll kafka_operation(operation_id); then kafka_get for status READY + kafka_hosts."}), false
}

func (s *Registry) handleKafkaDelete(ctx context.Context, args map[string]any) (string, bool) {
	id := firstArg(args, "cluster_id", "id")
	if id == "" {
		return usageResult(fmt.Errorf("'cluster_id' is required (from kafka_list)")), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{"status": "confirmation_required",
			"message": fmt.Sprintf("kafka_delete PERMANENTLY deletes Kafka cluster %s and its data. Confirm, then call again with confirmed=true.", id)}), true
	}
	cli, _, err := s.kafkaClientEng(ctx)
	if err != nil {
		return errResult(err), true
	}
	op, err := cli.DeleteCluster(ctx, id)
	if err != nil {
		return errResult(fmt.Errorf("delete cluster: %w", err)), true
	}
	return toJSON(map[string]any{"status": "deleting", "id": id, "operation_id": op.ID}), false
}

func (s *Registry) handleKafkaOperation(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "operation_id")
	if id == "" {
		return usageResult(fmt.Errorf("'operation_id' is required")), true
	}
	cli, _, err := s.kafkaClientEng(ctx)
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
