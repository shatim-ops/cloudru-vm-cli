package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/mk8s"
)

// k8sClient builds an authenticated mk8s client and returns it together with
// the service-account id (from the IAM token's `sub` claim) used as clusterSaId.
func (s *Registry) k8sClient(ctx context.Context) (*mk8s.Client, string, string, error) {
	eng, err := s.newEngine(nil)
	if err != nil {
		return nil, "", "", err
	}
	tok, err := eng.Auth.Token(ctx)
	if err != nil {
		return nil, "", "", err
	}
	pid := eng.Config.ProjectID
	cli := mk8s.New(tok.Value, pid, "")
	return cli, pid, jwtSub(tok.Value), nil
}

// jwtSub extracts the `sub` claim (service-account id) from a JWT.
func jwtSub(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	seg := parts[1]
	if m := len(seg) % 4; m != 0 {
		seg += strings.Repeat("=", 4-m)
	}
	b, err := base64.URLEncoding.DecodeString(seg)
	if err != nil {
		return ""
	}
	var c struct {
		Sub string `json:"sub"`
	}
	_ = json.Unmarshal(b, &c)
	return c.Sub
}

// handleK8sList lists Managed Kubernetes clusters.
func (s *Registry) handleK8sList(ctx context.Context, args map[string]any) (string, bool) {
	cli, _, _, err := s.k8sClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	cs, err := cli.ListClusters(ctx)
	if err != nil {
		return errResult(err), true
	}
	out := make([]map[string]any, 0, len(cs))
	for _, c := range cs {
		out = append(out, map[string]any{"id": c.ID, "name": c.Name, "status": c.Status})
	}
	return toJSON(map[string]any{"clusters": out}), false
}

// handleK8sGet returns a cluster by id.
func (s *Registry) handleK8sGet(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "cluster_id")
	if id == "" {
		return usageResult(fmt.Errorf("cluster_id is required")), true
	}
	cli, _, _, err := s.k8sClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	m, err := cli.GetCluster(ctx, id)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(m), false
}

// handleK8sOptions returns available k8s versions, flavors and zones.
func (s *Registry) handleK8sOptions(ctx context.Context, args map[string]any) (string, bool) {
	cli, _, _, err := s.k8sClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	pc, err := cli.ProductConfiguration(ctx)
	if err != nil {
		return errResult(err), true
	}
	versions := map[string][]string{}
	for _, ch := range pc.Channels {
		versions[ch.Channel] = ch.AvailableVersions
	}
	flavors := make([]map[string]any, 0, len(pc.Flavors))
	for _, f := range pc.Flavors {
		if f.GPU == 0 {
			flavors = append(flavors, map[string]any{"flavor_id": f.FlavorID, "name": f.Name, "cpu": f.CPU, "ram": f.RAM})
		}
	}
	zones := make([]string, 0, len(pc.Zones))
	for _, z := range pc.Zones {
		zones = append(zones, z.Name)
	}
	return toJSON(map[string]any{
		"versions_by_channel": versions,
		"flavors":             flavors,
		"zones":               zones,
		"defaults":            map[string]any{"zone": pc.DefaultZone, "channel": pc.DefaultChannel, "disk_type": pc.DefaultDiskType},
	}), false
}

// resolveK8sPlan resolves the create parameters (version, flavor, subnet, SA).
func (s *Registry) resolveK8sPlan(ctx context.Context, args map[string]any) (mk8s.CreateClusterReq, string, []string, error) {
	var warnings []string
	cli, pid, saID, err := s.k8sClient(ctx)
	if err != nil {
		return mk8s.CreateClusterReq{}, "", nil, err
	}
	pc, err := cli.ProductConfiguration(ctx)
	if err != nil {
		return mk8s.CreateClusterReq{}, "", nil, err
	}

	// Version: explicit, else newest STABLE, else first available.
	version := stringArg(args, "version")
	if version == "" {
		for _, ch := range pc.Channels {
			if strings.Contains(ch.Channel, "STABLE") && len(ch.AvailableVersions) > 0 {
				version = ch.AvailableVersions[0]
				break
			}
		}
		if version == "" {
			for _, ch := range pc.Channels {
				if len(ch.AvailableVersions) > 0 {
					version = ch.AvailableVersions[0]
					break
				}
			}
		}
	}

	// Flavor: explicit name, else smallest non-GPU that fits cpu/ram, else gen-2-4.
	cpu := intArg(args, "cpu", 2)
	ram := intArg(args, "ram_gb", 4)
	flavorName := stringArg(args, "flavor")
	flavorID := ""
	var bestName string
	var bestCPU, bestRAM int
	for _, f := range pc.Flavors {
		if f.GPU != 0 {
			continue
		}
		if flavorName != "" {
			if f.Name == flavorName {
				flavorID = f.FlavorID
				break
			}
			continue
		}
		if f.CPU >= cpu && f.RAM >= ram {
			if flavorID == "" || f.CPU < bestCPU || (f.CPU == bestCPU && f.RAM < bestRAM) {
				flavorID, bestName, bestCPU, bestRAM = f.FlavorID, f.Name, f.CPU, f.RAM
			}
		}
	}
	if flavorID == "" {
		return mk8s.CreateClusterReq{}, "", nil, fmt.Errorf("no matching k8s flavor (cpu>=%d ram>=%d); use k8s_options to see flavors", cpu, ram)
	}
	_ = bestName

	// Master count: 1, 3 or 5.
	masterCount := intArg(args, "master_count", 1)
	if masterCount != 1 && masterCount != 3 && masterCount != 5 {
		masterCount = 1
		warnings = append(warnings, "master_count must be 1, 3 or 5 — defaulted to 1")
	}

	// Subnet for the control-plane private VIP: explicit, else resolve a
	// zone-matched subnet from the compute API.
	subnetID := stringArg(args, "subnet_id")
	zoneName := firstNonEmptyStr(stringArg(args, "zone"), pc.DefaultZone)
	if subnetID == "" {
		eng, e := s.authedEngine(ctx, nil)
		if e != nil {
			return mk8s.CreateClusterReq{}, "", nil, e
		}
		// find zone uuid
		zoneID := ""
		if zs, ze := eng.Cloud.ListAvailabilityZones(ctx); ze == nil {
			for _, z := range zs {
				if z.Name == zoneName {
					zoneID = z.ID
					break
				}
			}
		}
		if subs, se := eng.Cloud.ListSubnets(ctx, pid); se == nil {
			for _, sn := range subs {
				if sn.AvailabilityZoneID == zoneID {
					subnetID = sn.ID
					break
				}
			}
			if subnetID == "" && len(subs) > 0 {
				subnetID = subs[0].ID
			}
		}
	}
	if subnetID == "" {
		return mk8s.CreateClusterReq{}, "", nil, fmt.Errorf("no subnet found for the control-plane VIP; pass subnet_id")
	}

	// Cluster service account: explicit, else the agent's own (token sub).
	clusterSaID := firstNonEmptyStr(stringArg(args, "cluster_sa_id"), saID)
	if clusterSaID == "" {
		return mk8s.CreateClusterReq{}, "", nil, fmt.Errorf("could not resolve cluster service account id; pass cluster_sa_id")
	}

	name := stringArg(args, "name")
	if name == "" {
		name = "k8s-" + firstNonEmptyStr(stringArg(args, "product"), "cluster")
	}

	kubeApiInternet := true
	if v, ok := args["kube_api_internet"].(bool); ok {
		kubeApiInternet = v
	}

	req := mk8s.CreateClusterReq{
		Name:      name,
		ProjectID: pid,
		ControlPlaneConfiguration: mk8s.ControlPlaneConfiguration{
			Version:             version,
			SizingConfiguration: mk8s.SizingConfiguration{MasterCount: masterCount, FlavorID: flavorID},
		},
		NetworkConfiguration:  mk8s.NetworkConfiguration{PrivateVipSubnetID: subnetID, KubeApiInternet: kubeApiInternet},
		IdentityConfiguration: mk8s.IdentityConfiguration{ClusterSaID: clusterSaID},
	}
	return req, flavorName, warnings, nil
}

// handleK8sPlan resolves and shows the cluster config without creating it.
func (s *Registry) handleK8sPlan(ctx context.Context, args map[string]any) (string, bool) {
	req, _, warnings, err := s.resolveK8sPlan(ctx, args)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{
		"name":              req.Name,
		"version":           req.ControlPlaneConfiguration.Version,
		"master_count":      req.ControlPlaneConfiguration.SizingConfiguration.MasterCount,
		"flavor_id":         req.ControlPlaneConfiguration.SizingConfiguration.FlavorID,
		"subnet_id":         req.NetworkConfiguration.PrivateVipSubnetID,
		"cluster_sa_id":     req.IdentityConfiguration.ClusterSaID,
		"kube_api_internet": req.NetworkConfiguration.KubeApiInternet,
		"warnings":          warnings,
		"note":              "Read-only plan. On the user's approval call k8s_create with the same args + confirmed=true. Cluster creation is async (~10-20 min); poll k8s_get. Add workers with k8s_add_nodepool (TODO) or via kubeconfig.",
	}), false
}

// handleK8sCreate creates a cluster (gated on confirmation). Async.
func (s *Registry) handleK8sCreate(ctx context.Context, args map[string]any) (string, bool) {
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status":  "confirmation_required",
			"message": "k8s_create builds a real (billable) Kubernetes cluster. Show the k8s_plan to the user; call again with confirmed=true after approval.",
		}), true
	}
	req, _, _, err := s.resolveK8sPlan(ctx, args)
	if err != nil {
		return errResult(err), true
	}
	cli, _, _, err := s.k8sClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	op, err := cli.CreateCluster(ctx, req)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{
		"status":       "creating",
		"cluster_id":   op.ResourceID,
		"operation_id": op.ID,
		"name":         req.Name,
		"note":         "Cluster is provisioning (async, ~10-20 min). Poll k8s_get with this cluster_id until status is running, then k8s_kubeconfig.",
	}), false
}

// handleK8sKubeconfig returns the kubeconfig for a cluster.
func (s *Registry) handleK8sKubeconfig(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "cluster_id")
	if id == "" {
		return usageResult(fmt.Errorf("cluster_id is required")), true
	}
	network := stringArg(args, "network") // NETWORK_TYPE_PUBLIC | NETWORK_TYPE_PRIVATE
	cli, _, _, err := s.k8sClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	cfg, err := cli.GetKubeConfig(ctx, id, network)
	if err != nil {
		return errResult(err), true
	}
	// The API returns base64; decode for convenience.
	plain := cfg
	if b, e := base64.StdEncoding.DecodeString(cfg); e == nil && len(b) > 0 {
		plain = string(b)
	}

	// By default embed a static bearer token so the kubeconfig works with a
	// plain kubectl/helm. The raw Cloud.ru kubeconfig authenticates via a
	// `cloudlogin` exec-plugin that is NOT installed by default → kubectl fails
	// "executable cloudlogin not found". embed_token=false returns the raw one.
	embed := true
	if v, ok := args["embed_token"].(bool); ok {
		embed = v
	}
	if !embed {
		return toJSON(map[string]any{"kubeconfig": plain, "auth": "cloudlogin exec-plugin (must be installed)"}), false
	}
	keyID, secret := s.accessKey()
	if keyID == "" || secret == "" {
		return toJSON(map[string]any{
			"kubeconfig": plain,
			"warning":    "embed_token requested but CLOUDRU_KEY_ID/CLOUDRU_SECRET not set — returned the raw kubeconfig, which needs the 'cloudlogin' exec plugin.",
		}), false
	}
	tok, e := mk8s.FetchOIDCToken(ctx, mk8s.OIDCURLFromKubeconfig(plain), keyID, secret)
	if e != nil {
		return errResult(fmt.Errorf("embed_token: OIDC token exchange failed: %w", e)), true
	}
	embedded, e := mk8s.EmbedToken(plain, tok)
	if e != nil {
		return errResult(fmt.Errorf("embed_token: %w", e)), true
	}
	return toJSON(map[string]any{
		"kubeconfig": embedded,
		"auth":       "embedded bearer token (no cloudlogin needed)",
		"note":       "Token TTL ~1h — re-fetch the kubeconfig if kubectl starts returning 401.",
	}), false
}

// handleK8sDelete deletes a cluster (gated on confirmation). Async.
func (s *Registry) handleK8sDelete(ctx context.Context, args map[string]any) (string, bool) {
	id := stringArg(args, "cluster_id")
	if id == "" {
		return usageResult(fmt.Errorf("cluster_id is required")), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status":  "confirmation_required",
			"message": fmt.Sprintf("k8s_delete permanently deletes cluster %s. Call again with confirmed=true after the user approves.", id),
		}), true
	}
	cli, _, _, err := s.k8sClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	op, err := cli.DeleteCluster(ctx, id)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{"status": "deleting", "cluster_id": id, "operation_id": op.ID}), false
}

// handleK8sNodePools lists node pools of a cluster.
func (s *Registry) handleK8sNodePools(ctx context.Context, args map[string]any) (string, bool) {
	cli, _, _, err := s.k8sClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	nps, err := cli.ListNodePools(ctx, stringArg(args, "cluster_id"))
	if err != nil {
		return errResult(err), true
	}
	out := make([]map[string]any, 0, len(nps))
	for _, np := range nps {
		row := map[string]any{
			"id":      np["id"],
			"name":    np["name"],
			"status":  np["status"],
			"version": np["version"],
		}
		// Desired node count comes from the scale policy (fixed or auto).
		if sp, ok := np["scalePolicy"].(map[string]any); ok {
			if fs, ok := sp["fixedScale"].(map[string]any); ok {
				row["desired"] = fs["count"]
			} else if as, ok := sp["autoScale"].(map[string]any); ok {
				row["minCount"] = as["minCount"]
				row["maxCount"] = as["maxCount"]
			}
		}
		// Flavor name for at-a-glance sizing.
		if mc, ok := np["machineConfiguration"].(map[string]any); ok {
			if fl, ok := mc["flavor"].(map[string]any); ok {
				row["flavor"] = fl["name"]
			}
		}
		out = append(out, row)
	}
	return toJSON(map[string]any{"node_pools": out}), false
}

// handleK8sAddNodePool adds a worker node pool to a cluster (gated). Async.
func (s *Registry) handleK8sAddNodePool(ctx context.Context, args map[string]any) (string, bool) {
	clusterID := stringArg(args, "cluster_id")
	if clusterID == "" {
		return usageResult(fmt.Errorf("cluster_id is required")), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status":  "confirmation_required",
			"message": "k8s_add_nodepool creates billable worker nodes. Call again with confirmed=true after the user approves.",
		}), true
	}

	cli, pid, _, err := s.k8sClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	pc, err := cli.ProductConfiguration(ctx)
	if err != nil {
		return errResult(err), true
	}

	// Flavor: explicit name, else smallest non-GPU that fits cpu/ram.
	cpu := intArg(args, "cpu", 2)
	ram := intArg(args, "ram_gb", 4)
	flavorName := stringArg(args, "flavor")
	flavorID := ""
	var bCPU, bRAM int
	for _, f := range pc.Flavors {
		if f.GPU != 0 {
			continue
		}
		if flavorName != "" {
			if f.Name == flavorName {
				flavorID = f.FlavorID
				break
			}
			continue
		}
		if f.CPU >= cpu && f.RAM >= ram {
			if flavorID == "" || f.CPU < bCPU || (f.CPU == bCPU && f.RAM < bRAM) {
				flavorID, bCPU, bRAM = f.FlavorID, f.CPU, f.RAM
			}
		}
	}
	if flavorID == "" {
		return errResult(fmt.Errorf("no matching node flavor (cpu>=%d ram>=%d); see k8s_options", cpu, ram)), true
	}

	// Nodes subnet + security group: explicit, else zone-matched from compute.
	nodesSubnetID := stringArg(args, "nodes_subnet_id")
	sgID := stringArg(args, "security_group_id")
	zoneName := firstNonEmptyStr(stringArg(args, "zone"), pc.DefaultZone)
	if nodesSubnetID == "" || sgID == "" {
		eng, e := s.authedEngine(ctx, nil)
		if e != nil {
			return errResult(e), true
		}
		zoneID := ""
		if zs, ze := eng.Cloud.ListAvailabilityZones(ctx); ze == nil {
			for _, z := range zs {
				if z.Name == zoneName {
					zoneID = z.ID
					break
				}
			}
		}
		if nodesSubnetID == "" {
			if subs, se := eng.Cloud.ListSubnets(ctx, pid); se == nil {
				for _, sn := range subs {
					if sn.AvailabilityZoneID == zoneID {
						nodesSubnetID = sn.ID
						break
					}
				}
				if nodesSubnetID == "" && len(subs) > 0 {
					nodesSubnetID = subs[0].ID
				}
			}
		}
		if sgID == "" {
			if sgs, se := eng.Cloud.ListSecurityGroups(ctx, pid); se == nil {
				for _, g := range sgs {
					if zoneName != "" && strings.Contains(g.Name, zoneName) {
						sgID = g.ID
						break
					}
				}
			}
		}
	}
	if nodesSubnetID == "" {
		return errResult(fmt.Errorf("no nodes subnet found; pass nodes_subnet_id")), true
	}

	count := intArg(args, "count", 2)
	diskSize := intArg(args, "disk_gb", 20)
	name := stringArg(args, "name")
	if name == "" {
		name = "nodepool-1"
	}

	req := mk8s.CreateNodePoolReq{
		ClusterID:            clusterID,
		Name:                 name,
		ScalePolicy:          mk8s.ScalePolicy{FixedScale: &mk8s.FixedScale{Count: count}},
		MachineConfiguration: mk8s.MachineConfigurationRequest{FlavorID: flavorID, Disk: &mk8s.DiskRequest{Size: diskSize}},
		NetworkConfiguration: mk8s.NodePoolNetworkConfigurationRequest{NodesSubnetID: nodesSubnetID, SecurityGroupID: sgID},
		UpdateConfiguration:  mk8s.NodePoolUpdateConfiguration{Strategy: "NODE_POOL_UPDATE_STRATEGY_ROLLING_UPDATE"},
	}
	op, err := cli.CreateNodePool(ctx, req)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{
		"status": "creating", "node_pool": name, "cluster_id": clusterID,
		"operation_id": op.ID, "count": count, "flavor_id": flavorID,
		"note": "Node pool is provisioning (async). Poll k8s_nodepools. NB: if Cloud.ru returns 503 'no healthy upstream' it's a transient backend outage — retry.",
	}), false
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
