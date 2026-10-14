package mcp

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"
)

// kubectl and helm run on the MCP server's host. Over HTTP their arguments
// come from a remote client, so the policy below keeps them on the cluster:
// no host files, no listeners on the host, no plugins from the host's PATH,
// no other API server for the cluster token, no internal URLs.

// kubectlHTTPVerbs are the built-in kubectl commands allowed over HTTP.
// Left out: plugin lookups (any other word), cp (host files), port-forward
// and proxy (listeners on the host), edit (runs an editor on the host),
// kustomize (host directory), attach (needs a terminal), completion, plugin.
var kubectlHTTPVerbs = map[string]bool{
	"get": true, "describe": true, "logs": true, "log": true, "top": true,
	"version": true, "api-resources": true, "api-versions": true, "explain": true,
	"events": true, "cluster-info": true, "config": true, "auth": true,
	"rollout": true, "diff": true, "wait": true,
	"create": true, "expose": true, "run": true, "set": true, "delete": true,
	"scale": true, "autoscale": true, "certificate": true, "cordon": true,
	"uncordon": true, "drain": true, "taint": true, "exec": true, "debug": true,
	"apply": true, "patch": true, "replace": true, "label": true, "annotate": true,
}

// kubectlHTTPDeniedFlags read or write host files, or send the cluster token
// to another server.
var kubectlHTTPDeniedFlags = map[string]bool{
	"kubeconfig": true, "server": true, "s": true, "token": true, "username": true,
	"password": true, "client-certificate": true, "client-key": true,
	"certificate-authority": true, "insecure-skip-tls-verify": true,
	"tls-server-name": true, "cache-dir": true, "filename": true, "f": true,
	"kustomize": true, "k": true, "profile": true, "profile-output": true,
	"log-file": true, "log-dir": true, "output-directory": true,
}

// kubectlHTTPCheck validates k8s_kubectl arguments for the HTTP transport.
// allowSecrets also permits commands that print credentials.
func kubectlHTTPCheck(fields []string, allowSecrets bool) error {
	verb := ""
	var resources []string
	output := ""
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if f == "--" {
			break // the rest is the command exec/debug runs in the pod
		}
		if !strings.HasPrefix(f, "-") || f == "-" {
			if verb == "" {
				verb = f
			} else {
				resources = append(resources, f)
			}
			continue
		}
		name, val, hasVal := strings.Cut(strings.TrimLeft(f, "-"), "=")
		if !strings.HasPrefix(f, "--") && len(name) > 1 {
			// -fFILE, -ojson: the shorthand is one letter.
			name, val, hasVal = name[:1], name[1:], true
		}
		if kubectlHTTPDeniedFlags[name] || strings.Contains(name, "file") || strings.HasSuffix(name, "-dir") {
			return fmt.Errorf("k8s_kubectl over HTTP: flag %s is not allowed: it reaches the MCP server's own files or credentials (use k8s_apply for manifests)", "-"+strings.TrimLeft(f, "-"))
		}
		if name == "o" || name == "output" {
			if !hasVal && i+1 < len(fields) {
				i++
				val = fields[i]
			}
			if strings.Contains(val, "file") {
				return fmt.Errorf("k8s_kubectl over HTTP: output %q reads a file on the MCP server", val)
			}
			output = val
		}
		if name == "raw" && !allowSecrets {
			return fmt.Errorf("k8s_kubectl over HTTP: --raw is disabled (it can read Secret objects); start the server with --allow-secret-export to enable it")
		}
	}
	if verb == "" {
		return nil // the handler itself asks for args
	}
	if !kubectlHTTPVerbs[verb] {
		return fmt.Errorf("k8s_kubectl over HTTP: %q is not a built-in kubectl command allowed in this mode (plugins, cp, port-forward, proxy, edit and kustomize run on the MCP server's host); put the command first, flags after it", verb)
	}
	if allowSecrets {
		return nil
	}
	if verb == "create" && len(resources) > 0 && resources[0] == "token" {
		return fmt.Errorf("k8s_kubectl create token is disabled over HTTP: it mints a credential; start the server with --allow-secret-export to enable it")
	}
	if verb == "get" && output != "" && output != "name" && output != "wide" && namesSecrets(resources) {
		return fmt.Errorf("k8s_kubectl get secret with -o %s is disabled over HTTP: it prints the secret data; start the server with --allow-secret-export to enable it", output)
	}
	return nil
}

// namesSecrets reports whether kubectl resource arguments include Secret
// objects ("secret", "secrets/x", "secret.v1", "cm,secret").
func namesSecrets(resources []string) bool {
	for _, r := range resources {
		for _, part := range strings.Split(r, ",") {
			kind, _, _ := strings.Cut(part, "/")
			kind, _, _ = strings.Cut(strings.ToLower(kind), ".")
			if kind == "secret" || kind == "secrets" {
				return true
			}
		}
	}
	return false
}

// lookupIP resolves a host name; tests replace it.
var lookupIP = func(ctx context.Context, host string) ([]net.IP, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return net.DefaultResolver.LookupIP(ctx, "ip", host)
}

// helmHTTPCheck validates k8s_helm arguments for the HTTP transport: the
// chart is remote (not a path on the MCP server), and every URL helm will
// fetch is https/oci on a public address — not the container's loopback,
// the cloud metadata service or a private network.
func helmHTTPCheck(ctx context.Context, args map[string]any) error {
	chart, _ := args["chart"].(string)
	repoURL, _ := args["repo_url"].(string)
	if repoURL != "" {
		if err := publicURL(ctx, repoURL); err != nil {
			return fmt.Errorf("k8s_helm over HTTP: repo_url: %w", err)
		}
	}
	if chart == "" {
		return nil
	}
	if strings.Contains(chart, "://") {
		if err := publicURL(ctx, chart); err != nil {
			return fmt.Errorf("k8s_helm over HTTP: chart: %w", err)
		}
		return nil
	}
	// "repo/name" or "name": helm tries a local path of that name first.
	local := strings.HasPrefix(chart, "/") || strings.HasPrefix(chart, ".") ||
		strings.HasPrefix(chart, "~") || strings.Contains(chart, `\`) || strings.Contains(chart, ":")
	if _, err := os.Stat(chart); err == nil {
		local = true
	}
	if local {
		return fmt.Errorf("k8s_helm over HTTP: chart %q is a path on the MCP server; use repo/name with repo_url, or an oci:// / https:// chart", chart)
	}
	return nil
}

// publicURL accepts an https or oci URL whose host resolves only to public
// unicast addresses.
func publicURL(ctx context.Context, raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL %q", raw)
	}
	if u.Scheme != "https" && u.Scheme != "oci" {
		return fmt.Errorf("%q: only https:// and oci:// URLs are allowed in this mode", raw)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("%q has no host", raw)
	}
	ips := []net.IP{net.ParseIP(host)}
	if ips[0] == nil {
		if ips, err = lookupIP(ctx, host); err != nil {
			return fmt.Errorf("resolve %s: %w", host, err)
		}
	}
	for _, ip := range ips {
		if !publicIP(ip) {
			return fmt.Errorf("%s resolves to %s, a loopback, private or link-local address", host, ip)
		}
	}
	return nil
}

// publicIP reports whether ip is a global unicast address outside private,
// shared (100.64/10, CGNAT) and link-local ranges.
func publicIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || !ip.IsGlobalUnicast() {
		return false
	}
	if v4 := ip.To4(); v4 != nil && v4[0] == 100 && v4[1]&0xc0 == 64 {
		return false
	}
	return true
}
