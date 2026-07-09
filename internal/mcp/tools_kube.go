package mcp

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/dzhechko/cloudru-vm-cli/internal/kubetools"
	"github.com/dzhechko/cloudru-vm-cli/internal/mk8s"
)

// kubeconfigForCluster fetches a cluster's kubeconfig with an embedded bearer
// token (no cloudlogin needed), ready to hand to kubectl/helm.
func (s *Server) kubeconfigForCluster(ctx context.Context, clusterID string) (string, error) {
	cli, _, _, err := s.k8sClient(ctx)
	if err != nil {
		return "", err
	}
	cfg, err := cli.GetKubeConfig(ctx, clusterID, "")
	if err != nil {
		return "", err
	}
	plain := cfg
	if b, e := base64.StdEncoding.DecodeString(cfg); e == nil && len(b) > 0 {
		plain = string(b)
	}
	keyID, secret := os.Getenv("CLOUDRU_KEY_ID"), os.Getenv("CLOUDRU_SECRET")
	if keyID == "" || secret == "" {
		return "", fmt.Errorf("CLOUDRU_KEY_ID/CLOUDRU_SECRET not set — cannot mint a kube token")
	}
	tok, err := mk8s.FetchOIDCToken(ctx, mk8s.OIDCURLFromKubeconfig(plain), keyID, secret)
	if err != nil {
		return "", fmt.Errorf("kube token exchange: %w", err)
	}
	return mk8s.EmbedToken(plain, tok)
}

// readOnlyKubectl is the allowlist of verbs that run freely; anything else
// (create/delete/patch/scale/rollout restart/exec/…) is a mutation and needs
// confirmed=true. Safe-by-default: an unknown verb is treated as mutating.
var readOnlyKubectl = map[string]bool{
	"get": true, "describe": true, "logs": true, "log": true, "top": true,
	"version": true, "api-resources": true, "api-versions": true, "explain": true,
	"events": true, "cluster-info": true, "config": true, "auth": true,
	"rollout": true, "diff": true, "wait": true, // rollout status/history are reads
}

// handleK8sKubectl runs ANY kubectl command against a cluster (uses an
// auto-embedded token — no cloudlogin needed). Read verbs run freely; verbs
// that mutate the cluster require confirmed=true. This is the general escape
// hatch for whatever a product's deploy/operate needs beyond helm/apply
// (create secret/configmap, scale, rollout restart, label, exec, cordon, …).
func (s *Server) handleK8sKubectl(ctx context.Context, args map[string]any) (string, bool) {
	clusterID := stringArg(args, "cluster_id")
	if clusterID == "" {
		return errResult(fmt.Errorf("cluster_id is required")), true
	}
	raw := stringArg(args, "args")
	if raw == "" {
		return errResult(fmt.Errorf("'args' is required, e.g. \"get pods -A\"")), true
	}
	fields := strings.Fields(raw)
	verb := ""
	if len(fields) > 0 {
		verb = fields[0]
	}
	if !readOnlyKubectl[verb] {
		if confirmed, _ := args["confirmed"].(bool); !confirmed {
			return toJSON(map[string]any{
				"status":  "confirmation_required",
				"message": fmt.Sprintf("kubectl %q changes the cluster. Get the captain's approval, then call again with confirmed=true. (Read-only verbs run without confirmation.)", verb),
			}), true
		}
	}
	kc, err := s.kubeconfigForCluster(ctx, clusterID)
	if err != nil {
		return errResult(err), true
	}
	out, err := kubetools.Kubectl(ctx, kc, "", 90*time.Second, fields...)
	if err != nil {
		return toJSON(map[string]any{"status": "error", "output": out, "error": err.Error()}), true
	}
	return toJSON(map[string]any{"status": "ok", "output": out}), false
}

// handleK8sApply applies a manifest (YAML/JSON) to the cluster. Mutation → confirmed.
func (s *Server) handleK8sApply(ctx context.Context, args map[string]any) (string, bool) {
	clusterID := stringArg(args, "cluster_id")
	manifest := stringArg(args, "manifest")
	if clusterID == "" || manifest == "" {
		return errResult(fmt.Errorf("cluster_id and manifest are required")), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status":  "confirmation_required",
			"message": "k8s_apply changes the cluster. Get the captain's approval, then call again with confirmed=true.",
		}), true
	}
	kc, err := s.kubeconfigForCluster(ctx, clusterID)
	if err != nil {
		return errResult(err), true
	}
	kargs := []string{"apply", "-f", "-"}
	if ns := stringArg(args, "namespace"); ns != "" {
		kargs = append(kargs, "-n", ns)
	}
	out, err := kubetools.Kubectl(ctx, kc, manifest, 120*time.Second, kargs...)
	if err != nil {
		return toJSON(map[string]any{"status": "error", "output": out, "error": err.Error()}), true
	}
	return toJSON(map[string]any{"status": "applied", "output": out}), false
}

// handleK8sHelm drives helm (install/upgrade/uninstall/list/status). Mutating
// actions (install/upgrade/uninstall) need confirmed=true.
func (s *Server) handleK8sHelm(ctx context.Context, args map[string]any) (string, bool) {
	clusterID := stringArg(args, "cluster_id")
	action := strings.ToLower(stringArg(args, "action"))
	if clusterID == "" || action == "" {
		return errResult(fmt.Errorf("cluster_id and action are required (install|upgrade|uninstall|list|status)")), true
	}
	mutating := map[string]bool{"install": true, "upgrade": true, "uninstall": true, "delete": true}
	if mutating[action] {
		if confirmed, _ := args["confirmed"].(bool); !confirmed {
			return toJSON(map[string]any{
				"status":  "confirmation_required",
				"message": fmt.Sprintf("helm %s changes the cluster (billable workloads). Get the captain's approval, then call again with confirmed=true.", action),
			}), true
		}
	}
	kc, err := s.kubeconfigForCluster(ctx, clusterID)
	if err != nil {
		return errResult(err), true
	}
	release := stringArg(args, "release")
	chart := stringArg(args, "chart")
	ns := stringArg(args, "namespace")

	// Optionally register a chart repo first (repo_url + a repo name derived
	// from the chart's "<repo>/<name>" prefix).
	if repoURL := stringArg(args, "repo_url"); repoURL != "" && (action == "install" || action == "upgrade") {
		repoName := "repo"
		if i := strings.Index(chart, "/"); i > 0 {
			repoName = chart[:i]
		}
		if out, e := kubetools.Helm(ctx, kc, 60*time.Second, "repo", "add", repoName, repoURL, "--force-update"); e != nil {
			return toJSON(map[string]any{"status": "error", "stage": "repo add", "output": out, "error": e.Error()}), true
		}
		if out, e := kubetools.Helm(ctx, kc, 90*time.Second, "repo", "update"); e != nil {
			return toJSON(map[string]any{"status": "error", "stage": "repo update", "output": out, "error": e.Error()}), true
		}
	}

	var hargs []string
	switch action {
	case "install", "upgrade":
		if release == "" || chart == "" {
			return errResult(fmt.Errorf("install/upgrade need release and chart")), true
		}
		hargs = []string{"upgrade", "--install", release, chart}
		if ns != "" {
			hargs = append(hargs, "-n", ns, "--create-namespace")
		}
		if v := stringArg(args, "version"); v != "" {
			hargs = append(hargs, "--version", v)
		}
		// Inline values YAML → temp file passed as -f.
		if values := stringArg(args, "values"); values != "" {
			vf, e := os.CreateTemp("", "helm-values-*.yaml")
			if e == nil {
				vf.WriteString(values)
				vf.Close()
				defer os.Remove(vf.Name())
				hargs = append(hargs, "-f", vf.Name())
			}
		}
		// --set overrides (comma-safe: passed as repeated --set).
		if sets, ok := args["set"].([]any); ok {
			for _, kv := range sets {
				if s, _ := kv.(string); s != "" {
					hargs = append(hargs, "--set", s)
				}
			}
		}
		hargs = append(hargs, "--wait", "--timeout", "8m")
	case "uninstall", "delete":
		if release == "" {
			return errResult(fmt.Errorf("uninstall needs release")), true
		}
		hargs = []string{"uninstall", release}
		if ns != "" {
			hargs = append(hargs, "-n", ns)
		}
	case "list":
		hargs = []string{"list", "-A"}
	case "status":
		hargs = []string{"status", release}
		if ns != "" {
			hargs = append(hargs, "-n", ns)
		}
	default:
		return errResult(fmt.Errorf("unknown action %q", action)), true
	}

	timeout := 120 * time.Second
	if action == "install" || action == "upgrade" {
		timeout = 9 * time.Minute // --wait can take a while pulling images
	}
	out, err := kubetools.Helm(ctx, kc, timeout, hargs...)
	if err != nil {
		return toJSON(map[string]any{"status": "error", "output": out, "error": err.Error()}), true
	}
	return toJSON(map[string]any{"status": "ok", "action": action, "output": out}), false
}
