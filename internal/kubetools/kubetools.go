// Package kubetools runs kubectl/helm against a Managed Kubernetes cluster
// using a kubeconfig passed in-memory (written to a private temp file for the
// duration of the call). It is the workload-deploy layer of the engine: cluster
// lifecycle is done via the Cloud.ru API, but deploying products INTO a cluster
// means driving kubectl/helm — the standard, battle-tested tools.
package kubetools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// resolve finds a kubectl/helm binary even when PATH is minimal (the MCP server
// may be spawned with a sparse PATH; the binaries live in ~/.local/bin on the VM).
func resolve(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	for _, c := range []string{home + "/.local/bin/" + name, "/usr/local/bin/" + name, "/usr/bin/" + name} {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c
		}
	}
	return name // let exec fail with a clear "not found" error
}

// Available reports whether a binary is resolvable.
func Available(name string) bool {
	if _, err := exec.LookPath(name); err == nil {
		return true
	}
	home, _ := os.UserHomeDir()
	for _, c := range []string{home + "/.local/bin/" + name, "/usr/local/bin/" + name, "/usr/bin/" + name} {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return true
		}
	}
	return false
}

// run executes `name args...` with KUBECONFIG pointed at a temp file holding the
// given kubeconfig, optionally feeding stdin. Returns combined stdout+stderr.
func run(ctx context.Context, kubeconfig, stdin string, timeout time.Duration, name string, args ...string) (string, error) {
	if !Available(name) {
		return "", fmt.Errorf("%s not found on the host (install it in ~/.local/bin)", name)
	}
	f, err := os.CreateTemp("", "kubecfg-*.yaml")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	if err := os.Chmod(f.Name(), 0o600); err == nil {
		_, _ = f.WriteString(kubeconfig)
	}
	f.Close()

	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, resolve(name), args...)
	cmd.Env = append(childEnv(os.Environ()), "KUBECONFIG="+f.Name())
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	out, err := cmd.CombinedOutput()
	if cctx.Err() == context.DeadlineExceeded {
		return string(out), fmt.Errorf("%s timed out after %s", name, timeout)
	}
	return string(out), err
}

// childEnv drops the server's own secrets from env: kubectl and helm (and
// their plugins) get the cluster token through KUBECONFIG and need neither
// the Cloud.ru access key nor the MCP bearer token.
func childEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "CLOUDRU_") || name == "MCP_BEARER_TOKEN" {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// Kubectl runs a kubectl command; stdin optional (for `apply -f -`).
func Kubectl(ctx context.Context, kubeconfig, stdin string, timeout time.Duration, args ...string) (string, error) {
	return run(ctx, kubeconfig, stdin, timeout, "kubectl", args...)
}

// Helm runs a helm command.
func Helm(ctx context.Context, kubeconfig string, timeout time.Duration, args ...string) (string, error) {
	return run(ctx, kubeconfig, "", timeout, "helm", args...)
}
