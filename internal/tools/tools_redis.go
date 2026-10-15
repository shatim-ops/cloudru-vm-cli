package tools

import (
	"context"
	"fmt"
	"net/url"

	"github.com/shatim-ops/evo-cli/internal/dbaas"
	"github.com/shatim-ops/evo-cli/internal/engine"
)

const redisHost = "https://redis.api.cloud.ru"

func (s *Registry) redisClientEng(ctx context.Context) (*dbaas.Client, *engine.Engine, error) {
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	tok, err := eng.Auth.Token(ctx)
	if err != nil {
		return nil, nil, err
	}
	return dbaas.New(tok.Value, eng.Config.ProjectID, redisHost), eng, nil
}

func (s *Registry) handleRedisList(ctx context.Context, args map[string]any) (string, bool) {
	cli, _, err := s.redisClientEng(ctx)
	if err != nil {
		return errResult(err), true
	}
	items, err := cli.ListClusters(ctx, url.Values{})
	if err != nil {
		return errResult(err), true
	}
	out := make([]map[string]any, 0, len(items))
	for _, m := range items {
		out = append(out, map[string]any{
			"id": m["id"], "name": m["name"], "status": m["status"],
			"type": m["type"], "shards": m["shards"], "replicas": m["replicas"], "health": m["health"],
		})
	}
	return toJSON(map[string]any{"clusters": out}), false
}

func (s *Registry) handleRedisGet(ctx context.Context, args map[string]any) (string, bool) {
	id := firstArg(args, "cluster_id", "id")
	if id == "" {
		return errResult(fmt.Errorf("'cluster_id' is required (from redis_list)")), true
	}
	cli, _, err := s.redisClientEng(ctx)
	if err != nil {
		return errResult(err), true
	}
	m, err := cli.GetCluster(ctx, id)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(m), false
}

func (s *Registry) handleRedisVersions(ctx context.Context, args map[string]any) (string, bool) {
	cli, _, err := s.redisClientEng(ctx)
	if err != nil {
		return errResult(err), true
	}
	items, err := cli.ListVersions(ctx, url.Values{})
	if err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{"versions": items}), false
}

func (s *Registry) handleRedisSpecs(ctx context.Context, args map[string]any) (string, bool) {
	cli, eng, err := s.redisClientEng(ctx)
	if err != nil {
		return errResult(err), true
	}
	versionID := stringArg(args, "version_id")
	if versionID == "" {
		if vers, e := cli.ListVersions(ctx, url.Values{}); e == nil && len(vers) > 0 {
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

func (s *Registry) handleRedisCreate(ctx context.Context, args map[string]any) (string, bool) {
	name := stringArg(args, "name")
	if name == "" {
		return errResult(fmt.Errorf("'name' is required")), true
	}
	cli, eng, err := s.redisClientEng(ctx)
	if err != nil {
		return errResult(err), true
	}

	versionID := stringArg(args, "version_id")
	if versionID == "" {
		vers, e := cli.ListVersions(ctx, url.Values{})
		if e != nil {
			return errResult(fmt.Errorf("list versions: %w", e)), true
		}
		if len(vers) == 0 {
			return errResult(fmt.Errorf("no redis versions available")), true
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
			"message": "redis_create needs a specification: pass specification_id, or cpu+ram matching one of these.", "available_specifications": menu}
		if trunc > 0 {
			out["specifications_truncated"] = trunc
		}
		return toJSON(out), false
	}
	specID, _ := spec["id"].(string)

	storage := pgInt(args, "storage_gb", 0)
	if storage == 0 {
		storage = int(toF(spec["min_storage_gb"]))
	}
	if storage == 0 {
		storage = 40
	}
	shards := pgInt(args, "shards", 1)
	if shards < 1 {
		shards = 1
	}
	// The API rejects STANDALONE ("shards must be range [1,8]" regardless of value)
	// and requires exactly 3 replicas per shard for MASTER_REPLICA. Default to a
	// working MASTER_REPLICA/3.
	rtype := stringArg(args, "type")
	if rtype == "" {
		rtype = "MASTER_REPLICA"
	}
	replicas := pgInt(args, "replicas", 3)
	if rtype == "MASTER_REPLICA" && replicas < 3 {
		replicas = 3
	}

	subnetID := stringArg(args, "subnet_id")
	if subnetID == "" {
		if subs, e := eng.Cloud.ListSubnets(ctx, eng.Config.ProjectID); e == nil && len(subs) > 0 {
			subnetID = subs[0].ID
		}
	}

	willCreate := map[string]any{"name": name, "version_id": versionID, "specification_id": specID,
		"cpu": spec["cpu"], "memory_gb": spec["memory"], "storage_gb": storage, "shards": shards, "replicas": replicas, "type": rtype}
	warn := ""
	if rtype == "MASTER_REPLICA" {
		warn = "MASTER_REPLICA needs 3 replicas spread across 3 availability zones — it will FAIL to provision in a single-zone project. A FAILED cluster currently CANNOT be deleted via the API (Cloud.ru orphan-lock) and needs console cleanup. Confirm the project has ≥3 AZs before creating."
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		out := map[string]any{"status": "confirmation_required",
			"message":     "redis_create provisions a BILLABLE Managed Redis cluster (async, ~minutes). Get the user's approval, then call again with confirmed=true.",
			"will_create": willCreate}
		if warn != "" {
			out["warning"] = warn
		}
		return toJSON(out), true
	}

	body := map[string]any{
		"name": name, "project_id": cli.ProjectID(), "version_id": versionID,
		"specification_id": specID, "storage_gb": storage, "shards": shards, "replicas": replicas, "type": rtype,
	}
	// Use the DEPRECATED singular subnet_id, NOT subnet_ids[]: the Redis backend
	// panics (divide-by-zero) when given the subnet_ids array (it mishandles
	// zone-distribution across the list), but works with the single subnet_id.
	if subnetID != "" {
		body["subnet_id"] = subnetID
	}
	if d := stringArg(args, "description"); d != "" {
		body["description"] = d
	}
	op, err := cli.CreateCluster(ctx, body)
	if err != nil {
		return errResult(fmt.Errorf("create cluster: %w", err)), true
	}
	return toJSON(map[string]any{"status": "creating", "operation_id": op.ID, "cluster_id": op.ResourceID,
		"created": willCreate, "note": "Async (~minutes). Poll redis_operation(operation_id); then redis_get for status READY + redis_hosts."}), false
}

func (s *Registry) handleRedisDelete(ctx context.Context, args map[string]any) (string, bool) {
	id := firstArg(args, "cluster_id", "id")
	if id == "" {
		return errResult(fmt.Errorf("'cluster_id' is required (from redis_list)")), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{"status": "confirmation_required",
			"message": fmt.Sprintf("redis_delete PERMANENTLY deletes Redis cluster %s and its data. Confirm, then call again with confirmed=true.", id)}), true
	}
	cli, _, err := s.redisClientEng(ctx)
	if err != nil {
		return errResult(err), true
	}
	op, err := cli.DeleteCluster(ctx, id)
	if err != nil {
		return errResult(fmt.Errorf("delete cluster: %w", err)), true
	}
	return toJSON(map[string]any{"status": "deleting", "id": id, "operation_id": op.ID}), false
}

func (s *Registry) handleRedisOperation(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "operation_id")
	if id == "" {
		return errResult(fmt.Errorf("'operation_id' is required")), true
	}
	cli, _, err := s.redisClientEng(ctx)
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
