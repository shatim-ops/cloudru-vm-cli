package mcp

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	"github.com/dzhechko/cloudru-vm-cli/internal/engine"
	"github.com/dzhechko/cloudru-vm-cli/internal/postgresql"
)

// specMenuCap caps how many specifications the need_specification menu returns.
// The full list (17+ specs, each ~10 fields) overflows the agent trajectory
// (trajectory-array-size-limit). Cheapest-first + a truncated count is enough
// to pick from.
const specMenuCap = 12

// compactSpecs returns a compact, cheapest-first, capped view of PG specs:
// only the fields an agent needs to choose (id, display_name, cpu, memory,
// deployment_mode, min_storage_gb). Returns the menu and how many were dropped.
func compactSpecs(specs []map[string]any, cap int) ([]map[string]any, int) {
	sorted := make([]map[string]any, len(specs))
	copy(sorted, specs)
	sort.SliceStable(sorted, func(i, j int) bool {
		ci, cj := toF(sorted[i]["cpu"]), toF(sorted[j]["cpu"])
		if ci != cj {
			return ci < cj
		}
		return toF(sorted[i]["memory"]) < toF(sorted[j]["memory"])
	})
	trunc := 0
	if len(sorted) > cap {
		trunc = len(sorted) - cap
		sorted = sorted[:cap]
	}
	out := make([]map[string]any, 0, len(sorted))
	for _, s := range sorted {
		minStorage := s["min_storage_gb"]
		if minStorage == nil {
			minStorage = s["min_storage_gigs"] // Kafka uses this field name
		}
		name := s["display_name"]
		if name == nil {
			name = s["name"] // Kafka/Redis specs use "name"
		}
		out = append(out, map[string]any{
			"id": s["id"], "display_name": name,
			"cpu": s["cpu"], "memory": s["memory"],
			"deployment_mode": s["deployment_mode"], "min_storage_gb": minStorage,
		})
	}
	return out, trunc
}

// pgClient builds an authenticated Managed PostgreSQL client.
func (s *Server) pgClient(ctx context.Context) (*postgresql.Client, error) {
	cli, _, err := s.pgClientEng(ctx)
	return cli, err
}

// pgClientEng also returns the engine (for subnet resolution in pg_create).
// Uses authedEngine (not newEngine) so eng.Cloud (the compute adapter) carries a
// valid bearer — otherwise eng.Cloud.ListSubnets 403s with "RBAC: access denied".
func (s *Server) pgClientEng(ctx context.Context) (*postgresql.Client, *engine.Engine, error) {
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	tok, err := eng.Auth.Token(ctx)
	if err != nil {
		return nil, nil, err
	}
	return postgresql.New(tok.Value, eng.Config.ProjectID, ""), eng, nil
}

func pgInt(args map[string]any, key string, def int) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func toF(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case string:
		f, _ := strconv.ParseFloat(n, 64)
		return f
	}
	return 0
}

// --- Clusters: read ---

func (s *Server) handlePgList(ctx context.Context, args map[string]any) (string, bool) {
	cli, err := s.pgClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	items, err := cli.ListClusters(ctx)
	if err != nil {
		return errResult(err), true
	}
	out := make([]map[string]any, 0, len(items))
	for _, m := range items {
		out = append(out, map[string]any{
			"id":      m["id"],
			"name":    m["name"],
			"status":  firstNonNil(m["status"], m["state"]),
			"version": m["version"],
			"health":  m["health"],
		})
	}
	return toJSON(map[string]any{"clusters": out}), false
}

func (s *Server) handlePgGet(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "id")
	if id == "" {
		id = stringArg(args, "cluster_id") // accept both, for consistency with pg_* sub-resource tools
	}
	if id == "" {
		return errResult(fmt.Errorf("'id' is required (get it from pg_list)")), true
	}
	cli, err := s.pgClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	m, err := cli.GetCluster(ctx, id)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(m), false
}

// --- Reference ---

func (s *Server) handlePgVersions(ctx context.Context, args map[string]any) (string, bool) {
	cli, err := s.pgClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	items, err := cli.ListVersions(ctx)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{"versions": items}), false
}

func (s *Server) handlePgSpecs(ctx context.Context, args map[string]any) (string, bool) {
	cli, err := s.pgClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	// /v1/specifications requires a version_name (404 without it) → default to latest.
	version := stringArg(args, "version")
	if version == "" {
		if vers, e := cli.ListVersions(ctx); e == nil {
			version = pickLatestVersion(vers)
		}
	}
	items, err := cli.ListSpecifications(ctx, version)
	if err != nil {
		return errResult(err), true
	}
	// Cheapest-first + cap so the 17-element array can't overflow the agent
	// trajectory (trajectory-array-size-limit → non-deliverable turn).
	sort.SliceStable(items, func(i, j int) bool {
		ci, cj := toF(items[i]["cpu"]), toF(items[j]["cpu"])
		if ci != cj {
			return ci < cj
		}
		return toF(items[i]["memory"]) < toF(items[j]["memory"])
	})
	trunc := 0
	if len(items) > specMenuCap {
		trunc = len(items) - specMenuCap
		items = items[:specMenuCap]
	}
	out := make([]map[string]any, 0, len(items))
	for _, m := range items {
		out = append(out, map[string]any{
			"id":              m["id"],
			"display_name":    m["display_name"],
			"cpu":             m["cpu"],
			"memory_gb":       m["memory"],
			"deployment_mode": m["deployment_mode"],
			"flavor_type":     m["flavor_type"],
			"min_storage_gb":  m["min_storage_gb"],
			"max_hosts":       m["max_hosts"],
		})
	}
	res := map[string]any{"specifications": out}
	if trunc > 0 {
		res["specifications_truncated"] = trunc
	}
	return toJSON(res), false
}

func (s *Server) handlePgOperation(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "operation_id")
	if id == "" {
		return errResult(fmt.Errorf("'operation_id' is required")), true
	}
	cli, err := s.pgClient(ctx)
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

// --- Cluster lifecycle (mutations) ---

func pickLatestVersion(versions []map[string]any) string {
	best, bestN := "", -1
	for _, v := range versions {
		name, _ := v["name"].(string)
		if name == "" {
			continue
		}
		if n, err := strconv.Atoi(name); err == nil {
			if n > bestN {
				bestN, best = n, name
			}
		} else if best == "" {
			best = name
		}
	}
	return best
}

func findSpec(specs []map[string]any, specID string, cpu, ram int) map[string]any {
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
	}
	return nil
}

func (s *Server) handlePgCreate(ctx context.Context, args map[string]any) (string, bool) {
	name := stringArg(args, "name")
	if name == "" {
		return errResult(fmt.Errorf("'name' is required")), true
	}
	cli, eng, err := s.pgClientEng(ctx)
	if err != nil {
		return errResult(err), true
	}

	// Resolve version (default: latest available).
	versions, err := cli.ListVersions(ctx)
	if err != nil {
		return errResult(fmt.Errorf("list versions: %w", err)), true
	}
	version := stringArg(args, "version")
	if version == "" {
		version = pickLatestVersion(versions)
	}

	// Resolve specification (by id, or by cpu+ram).
	specs, err := cli.ListSpecifications(ctx, version)
	if err != nil {
		return errResult(fmt.Errorf("list specifications: %w", err)), true
	}
	spec := findSpec(specs, stringArg(args, "specification_id"), pgInt(args, "cpu", 0), pgInt(args, "ram", 0))
	if spec == nil {
		// Not enough info to pick a spec — hand the agent a COMPACT, capped menu.
		// Returning all 17 full spec objects overflows the agent trajectory
		// (trajectory-array-size-limit → non-deliverable turn). Keep the key
		// fields, cheapest-first, capped.
		menu, trunc := compactSpecs(specs, specMenuCap)
		out := map[string]any{
			"status":                   "need_specification",
			"message":                  "pg_create needs a specification: pass specification_id, or cpu+ram matching one of these.",
			"resolved_version":         version,
			"available_specifications": menu,
		}
		if trunc > 0 {
			out["specifications_truncated"] = trunc
		}
		return toJSON(out), false
	}
	specID, _ := spec["id"].(string)

	storageGB := pgInt(args, "storage_gb", 0)
	if storageGB == 0 {
		storageGB = int(toF(spec["min_storage_gb"]))
	}
	if storageGB == 0 {
		storageGB = 50
	}
	initialDB := stringArg(args, "initial_database")
	if initialDB == "" {
		initialDB = "defaultdb"
	}
	instances := pgInt(args, "instances", 1)

	// Backup config. Backups CANNOT be enabled/disabled after creation (API rule),
	// and without a backup spec the cluster reports "backup disabled" (manual
	// pg_backup_create + pg_backups both 400). So enable automatic backups BY
	// DEFAULT with the documented defaults (weekly Sun 03:00, 14-day retention);
	// allow overrides, or no_backup:true to opt out.
	backupEnabled := true
	if nb, _ := args["no_backup"].(bool); nb {
		backupEnabled = false
	}
	var backupSpec map[string]any
	if backupEnabled {
		schedule := stringArg(args, "backup_schedule")
		if schedule == "" {
			schedule = "0 3 * * 0"
		}
		retention := pgInt(args, "backup_retention_days", 14)
		backupSpec = map[string]any{"schedule": schedule, "retention_policy_days": retention}
	}

	// Resolve subnet (defines the AZ). Explicit subnet_id wins; else first project subnet.
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

	willCreate := map[string]any{
		"name":             name,
		"version":          version,
		"specification_id": specID,
		"cpu":              spec["cpu"],
		"memory_gb":        spec["memory"],
		"deployment_mode":  spec["deployment_mode"],
		"storage_gb":       storageGB,
		"instances":        instances,
		"subnet_id":        subnetID,
		"initial_database": initialDB,
		"backup_enabled":   backupEnabled,
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status":      "confirmation_required",
			"message":     "pg_create provisions a BILLABLE Managed PostgreSQL cluster (~minutes). Get the captain's approval, then call again with confirmed=true.",
			"will_create": willCreate,
		}), true
	}

	body := map[string]any{
		"name":             name,
		"project_id":       cli.ProjectID(),
		"version":          version,
		"specification_id": specID,
		"storage":          map[string]any{"pg_data_gb": storageGB},
		"initial_database": initialDB,
		"instances":        instances,
		"subnet_ids":       []string{subnetID},
	}
	if backupSpec != nil {
		body["backup"] = backupSpec
	}
	if d := stringArg(args, "description"); d != "" {
		body["description"] = d
	}
	op, err := cli.CreateCluster(ctx, body)
	if err != nil {
		return errResult(fmt.Errorf("create cluster: %w", err)), true
	}
	return toJSON(map[string]any{
		"status":       "creating",
		"operation_id": op.ID,
		"created":      willCreate,
		"note":         "Cluster creation is async (~minutes). Poll pg_operation(operation_id) until done=true; then pg_list/pg_get for status READY.",
	}), false
}

func (s *Server) handlePgDelete(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "id")
	if id == "" {
		id = stringArg(args, "cluster_id") // accept both, for consistency with pg_* sub-resource tools
	}
	if id == "" {
		return errResult(fmt.Errorf("'id' is required (from pg_list)")), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status":  "confirmation_required",
			"message": fmt.Sprintf("pg_delete PERMANENTLY deletes PostgreSQL cluster %s and its data. Confirm, then call again with confirmed=true.", id),
		}), true
	}
	cli, err := s.pgClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	op, err := cli.DeleteCluster(ctx, id)
	if err != nil {
		return errResult(fmt.Errorf("delete cluster: %w", err)), true
	}
	return toJSON(map[string]any{"status": "deleting", "id": id, "operation_id": op.ID, "note": "Async — poll pg_operation(operation_id) until done=true."}), false
}

// --- Databases ---

func (s *Server) handlePgDatabases(ctx context.Context, args map[string]any) (string, bool) {
	cid := stringArg(args, "cluster_id")
	if cid == "" {
		return errResult(fmt.Errorf("'cluster_id' is required")), true
	}
	cli, err := s.pgClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	items, err := cli.ListDatabases(ctx, cid)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{"databases": items}), false
}

func (s *Server) handlePgDbCreate(ctx context.Context, args map[string]any) (string, bool) {
	cid := stringArg(args, "cluster_id")
	name := stringArg(args, "name")
	owner := stringArg(args, "owner")
	if cid == "" || name == "" || owner == "" {
		return errResult(fmt.Errorf("'cluster_id', 'name' and 'owner' are required")), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{"status": "confirmation_required", "message": fmt.Sprintf("pg_db_create creates database %q (owner %q) in cluster %s. Call again with confirmed=true.", name, owner, cid)}), true
	}
	cli, err := s.pgClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	body := map[string]any{"cluster_id": cid, "name": name, "owner": owner}
	if v := stringArg(args, "lc_collate"); v != "" {
		body["lc_collate"] = v
	}
	if v := stringArg(args, "lc_ctype"); v != "" {
		body["lc_ctype"] = v
	}
	db, err := cli.CreateDatabase(ctx, cid, body)
	if err != nil {
		return errResult(fmt.Errorf("create database: %w", err)), true
	}
	return toJSON(map[string]any{"status": "created", "database": db}), false
}

func (s *Server) handlePgDbDelete(ctx context.Context, args map[string]any) (string, bool) {
	cid := stringArg(args, "cluster_id")
	name := stringArg(args, "name")
	if cid == "" || name == "" {
		return errResult(fmt.Errorf("'cluster_id' and 'name' are required")), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{"status": "confirmation_required", "message": fmt.Sprintf("pg_db_delete PERMANENTLY drops database %q in cluster %s. Call again with confirmed=true.", name, cid)}), true
	}
	cli, err := s.pgClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	if err := cli.DeleteDatabase(ctx, cid, name); err != nil {
		return errResult(fmt.Errorf("delete database: %w", err)), true
	}
	return toJSON(map[string]any{"status": "deleted", "database": name}), false
}

// --- Users ---

func (s *Server) handlePgUsers(ctx context.Context, args map[string]any) (string, bool) {
	cid := stringArg(args, "cluster_id")
	if cid == "" {
		return errResult(fmt.Errorf("'cluster_id' is required")), true
	}
	cli, err := s.pgClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	items, err := cli.ListUsers(ctx, cid)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{"users": items}), false
}

func (s *Server) handlePgUserCreate(ctx context.Context, args map[string]any) (string, bool) {
	cid := stringArg(args, "cluster_id")
	name := stringArg(args, "name")
	password := stringArg(args, "password")
	if cid == "" || name == "" || password == "" {
		return errResult(fmt.Errorf("'cluster_id', 'name' and 'password' are required")), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{"status": "confirmation_required", "message": fmt.Sprintf("pg_user_create creates user %q in cluster %s. Call again with confirmed=true.", name, cid)}), true
	}
	cli, err := s.pgClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	body := map[string]any{"cluster_id": cid, "name": name, "password": password}
	u, err := cli.CreateUser(ctx, cid, body)
	if err != nil {
		return errResult(fmt.Errorf("create user: %w", err)), true
	}
	// Never echo the password back.
	return toJSON(map[string]any{"status": "created", "user": u}), false
}

func (s *Server) handlePgUserDelete(ctx context.Context, args map[string]any) (string, bool) {
	cid := stringArg(args, "cluster_id")
	name := stringArg(args, "name")
	if cid == "" || name == "" {
		return errResult(fmt.Errorf("'cluster_id' and 'name' are required")), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{"status": "confirmation_required", "message": fmt.Sprintf("pg_user_delete PERMANENTLY deletes user %q in cluster %s. Call again with confirmed=true.", name, cid)}), true
	}
	cli, err := s.pgClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	if err := cli.DeleteUser(ctx, cid, name); err != nil {
		return errResult(fmt.Errorf("delete user: %w", err)), true
	}
	return toJSON(map[string]any{"status": "deleted", "user": name}), false
}

// --- Backups ---

func (s *Server) handlePgBackups(ctx context.Context, args map[string]any) (string, bool) {
	cid := stringArg(args, "cluster_id")
	if cid == "" {
		return errResult(fmt.Errorf("'cluster_id' is required")), true
	}
	cli, err := s.pgClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	items, err := cli.ListBackups(ctx, cid)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{"backups": items}), false
}

func (s *Server) handlePgBackupCreate(ctx context.Context, args map[string]any) (string, bool) {
	cid := stringArg(args, "cluster_id")
	name := stringArg(args, "name")
	if cid == "" || name == "" {
		return errResult(fmt.Errorf("'cluster_id' and 'name' are required")), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{"status": "confirmation_required", "message": fmt.Sprintf("pg_backup_create starts a manual backup %q of cluster %s. Call again with confirmed=true.", name, cid)}), true
	}
	cli, err := s.pgClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	op, err := cli.CreateBackup(ctx, cid, name)
	if err != nil {
		return errResult(fmt.Errorf("create backup: %w", err)), true
	}
	return toJSON(map[string]any{"status": "backing_up", "operation_id": op.ID, "note": "Async — poll pg_operation(operation_id)."}), false
}
