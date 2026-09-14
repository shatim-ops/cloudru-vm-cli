package mcp

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/shatim-ops/cloudru-vm-cli/internal/kubetools"
	"github.com/shatim-ops/cloudru-vm-cli/internal/mk8s"
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

// helmActions is the full set, split by what each one does to the world.
//
//   - mutating: changes the cluster → confirmed=true. `test` belongs here and
//     not in "reads": it runs a pod built from the CHART's own spec, i.e. code
//     the chart author wrote, inside our cluster.
//   - cluster reads: need a kubeconfig, change nothing.
//   - local: `lint` and `template` never touch a cluster, so they must not
//     require a cluster_id. The factory lints and renders a chart it has just
//     built, long before any cluster exists.
var (
	helmMutating = map[string]bool{"install": true, "upgrade": true, "uninstall": true, "delete": true, "test": true}
	helmLocal    = map[string]bool{"lint": true, "template": true}
	helmKnown    = map[string]bool{
		"install": true, "upgrade": true, "uninstall": true, "delete": true,
		"list": true, "status": true, "test": true, "lint": true, "template": true,
	}
)

// emptyKubeconfig is handed to the local actions. Without it helm would fall
// back to ~/.kube/config — a stray config from some other cluster, silently
// rendering against the wrong Kubernetes version.
const emptyKubeconfig = "apiVersion: v1\nkind: Config\nclusters: []\ncontexts: []\nusers: []\npreferences: {}\n"

// handleK8sHelm drives helm against a cluster, or locally for lint/template.
func (s *Server) handleK8sHelm(ctx context.Context, args map[string]any) (string, bool) {
	action := strings.ToLower(stringArg(args, "action"))
	if action == "" {
		return errResult(fmt.Errorf("action is required (install|upgrade|uninstall|list|status|test|lint|template)")), true
	}
	if !helmKnown[action] {
		return errResult(fmt.Errorf("unknown action %q (install|upgrade|uninstall|list|status|test|lint|template)", action)), true
	}
	clusterID := stringArg(args, "cluster_id")
	if clusterID == "" && !helmLocal[action] {
		return errResult(fmt.Errorf("cluster_id is required for %s (only lint/template run without a cluster)", action)), true
	}
	if helmMutating[action] {
		if confirmed, _ := args["confirmed"].(bool); !confirmed {
			what := "changes the cluster (billable workloads)"
			if action == "test" {
				what = "runs the chart's own test pod in the cluster — that is code the chart author wrote"
			}
			return toJSON(map[string]any{
				"status":  "confirmation_required",
				"message": fmt.Sprintf("helm %s %s. Get the captain's approval, then call again with confirmed=true.", action, what),
			}), true
		}
	}

	// Chart source: a repo ref ("repo/name") or a path on this host (a chart
	// directory or a packaged .tgz). The factory builds a chart locally and must
	// install exactly those bytes, so the local path is not a convenience.
	chart := stringArg(args, "chart")
	if p := stringArg(args, "chart_path"); p != "" {
		if chart != "" {
			return errResult(fmt.Errorf("pass either chart (repo ref) or chart_path (local path), not both")), true
		}
		fi, err := os.Stat(p)
		if err != nil {
			return errResult(fmt.Errorf("chart_path: %w", err)), true
		}
		if !fi.IsDir() && !strings.HasSuffix(p, ".tgz") && !strings.HasSuffix(p, ".tar.gz") {
			return errResult(fmt.Errorf("chart_path %s is neither a chart directory nor a packaged .tgz", p)), true
		}
		chart = p
	}

	kc := emptyKubeconfig
	if !helmLocal[action] {
		var err error
		if kc, err = s.kubeconfigForCluster(ctx, clusterID); err != nil {
			return errResult(err), true
		}
	}

	// Optionally register a chart repo first (repo_url + a repo name derived
	// from the chart's "<repo>/<name>" prefix). Pointless for a local chart.
	if repoURL := stringArg(args, "repo_url"); repoURL != "" && stringArg(args, "chart_path") == "" &&
		(action == "install" || action == "upgrade" || action == "template" || action == "lint") {
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

	// Values files: on-disk file first, inline YAML second, so inline wins on
	// conflict (helm applies -f in order). A values file that cannot be written
	// is a hard failure — the previous version swallowed that error and ran with
	// chart defaults while reporting success.
	valueArgs, cleanup, err := helmValueArgs(args)
	defer cleanup()
	if err != nil {
		return errResult(err), true
	}
	hargs, timeout, err := helmCommand(action, chart, args, valueArgs, helmSetArgs(args))
	if err != nil {
		return errResult(err), true
	}

	out, runErr := kubetools.Helm(ctx, kc, timeout, hargs...)
	if runErr != nil {
		return toJSON(map[string]any{"status": "error", "action": action, "output": out, "error": runErr.Error()}), true
	}
	return toJSON(map[string]any{"status": "ok", "action": action, "output": out}), false
}

// helmCommand builds the helm argv and the process deadline for one action.
//
// Kept pure — no cluster, no filesystem — because the argv IS the behaviour of
// this tool, and a bug in it costs a cluster-wide misinstall. Everything a test
// needs to catch that lives here.
func helmCommand(action, chart string, args map[string]any, valueArgs, setArgs []string) ([]string, time.Duration, error) {
	release := stringArg(args, "release")
	ns := stringArg(args, "namespace")
	version := stringArg(args, "version")

	switch action {
	case "install", "upgrade":
		if release == "" || chart == "" {
			return nil, 0, fmt.Errorf("install/upgrade need release and chart (or chart_path)")
		}
		h := []string{"upgrade", "--install", release, chart}
		if ns != "" {
			h = append(h, "-n", ns, "--create-namespace")
		}
		if version != "" {
			h = append(h, "--version", version)
		}
		h = append(h, valueArgs...)
		h = append(h, setArgs...)
		if boolArg(args, "wait", true) {
			h = append(h, "--wait")
		}
		h = append(h, "--timeout", helmTimeoutFlag(args, 8*time.Minute))
		return h, helmOuterTimeout(args, 9*time.Minute), nil

	case "uninstall", "delete":
		if release == "" {
			return nil, 0, fmt.Errorf("uninstall needs release")
		}
		h := []string{"uninstall", release}
		if ns != "" {
			h = append(h, "-n", ns)
		}
		if boolArg(args, "wait", true) {
			h = append(h, "--wait")
		}
		return h, helmOuterTimeout(args, 5*time.Minute), nil

	case "test":
		if release == "" {
			return nil, 0, fmt.Errorf("test needs release")
		}
		// --logs is the point of running this at all: without it a failed test
		// reports "pod failed" and the reason stays inside the deleted pod.
		h := []string{"test", release, "--logs", "--timeout", helmTimeoutFlag(args, 5*time.Minute)}
		if ns != "" {
			h = append(h, "-n", ns)
		}
		return h, helmOuterTimeout(args, 6*time.Minute), nil

	case "list":
		return []string{"list", "-A"}, helmOuterTimeout(args, 120*time.Second), nil

	case "status":
		if release == "" {
			return nil, 0, fmt.Errorf("status needs release")
		}
		h := []string{"status", release}
		if ns != "" {
			h = append(h, "-n", ns)
		}
		return h, helmOuterTimeout(args, 120*time.Second), nil

	case "lint", "template":
		if chart == "" {
			return nil, 0, fmt.Errorf("%s needs chart or chart_path", action)
		}
		var h []string
		if action == "lint" {
			h = []string{"lint", chart}
		} else {
			name := release
			if name == "" {
				name = "release"
			}
			h = []string{"template", name, chart}
			if ns != "" {
				h = append(h, "-n", ns)
			}
		}
		if version != "" {
			h = append(h, "--version", version)
		}
		if kv := stringArg(args, "kube_version"); kv != "" {
			h = append(h, "--kube-version", kv)
		}
		h = append(h, valueArgs...)
		h = append(h, setArgs...)
		return h, helmOuterTimeout(args, 120*time.Second), nil
	}
	return nil, 0, fmt.Errorf("unknown action %q", action)
}

// helmValueArgs builds the -f flags: an on-disk values file, then inline YAML
// written to a temp file. The caller must call cleanup even on error.
func helmValueArgs(args map[string]any) (flags []string, cleanup func(), err error) {
	var temps []string
	cleanup = func() {
		for _, p := range temps {
			_ = os.Remove(p)
		}
	}
	if vf := stringArg(args, "values_file"); vf != "" {
		if _, e := os.Stat(vf); e != nil {
			return nil, cleanup, fmt.Errorf("values_file: %w", e)
		}
		flags = append(flags, "-f", vf)
	}
	if values := stringArg(args, "values"); values != "" {
		f, e := os.CreateTemp("", "helm-values-*.yaml")
		if e != nil {
			return nil, cleanup, fmt.Errorf("write inline values: %w", e)
		}
		temps = append(temps, f.Name())
		if _, e := f.WriteString(values); e != nil {
			f.Close()
			return nil, cleanup, fmt.Errorf("write inline values: %w", e)
		}
		if e := f.Close(); e != nil {
			return nil, cleanup, fmt.Errorf("write inline values: %w", e)
		}
		flags = append(flags, "-f", f.Name())
	}
	return flags, cleanup, nil
}

// helmSetArgs turns the "set" array into repeated --set flags (comma-safe).
func helmSetArgs(args map[string]any) []string {
	sets, ok := args["set"].([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, kv := range sets {
		if s, _ := kv.(string); s != "" {
			out = append(out, "--set", s)
		}
	}
	return out
}

// helmTimeoutFlag renders the value for helm's own --timeout.
func helmTimeoutFlag(args map[string]any, def time.Duration) string {
	if n := pgInt(args, "timeout_seconds", 0); n > 0 {
		return fmt.Sprintf("%ds", n)
	}
	return fmt.Sprintf("%ds", int(def.Seconds()))
}

// helmOuterTimeout keeps the process deadline above helm's own, so a helm that
// is merely slow reports its own error instead of being killed by us.
func helmOuterTimeout(args map[string]any, def time.Duration) time.Duration {
	if n := pgInt(args, "timeout_seconds", 0); n > 0 {
		return time.Duration(n)*time.Second + 60*time.Second
	}
	return def
}
