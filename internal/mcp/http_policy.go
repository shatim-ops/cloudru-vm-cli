package mcp

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/shatim-ops/cloudru-vm-cli/internal/kubetools"
	"github.com/shatim-ops/cloudru-vm-cli/internal/tools"
)

// binaryAvailable reports whether kubectl/helm can be run; tests replace it
// (kubetools also looks in /usr/local/bin, where CI runners have both).
var binaryAvailable = kubetools.Available

// restrictForHTTP adjusts tool handlers for the HTTP transport. The tool set
// and schemas stay the same; some calls answer with an error instead:
//
//   - k8s_kubectl, k8s_apply, k8s_helm when kubectl/helm is not installed
//     (the Docker image ships without them);
//   - k8s_kubeconfig with an embedded token, k8s_kubectl config (it prints
//     the same token), cert_private_key and secret_value, unless
//     allowSecretExport: over HTTP the secret would leave the host that
//     holds the Cloud.ru credentials. Without the flag credential-shaped
//     fields of every other result are masked too (redactSecrets);
//   - arguments that reach the server's filesystem or network: deploy's
//     compose_file must lie inside the working directory, k8s_kubectl takes
//     only built-in commands without host-side flags, k8s_helm only remote
//     charts on public https/oci hosts.
//
// Wrapping the handlers (not the tools/call dispatch) also covers calls the
// stack orchestrator makes in-process.
func (s *Server) restrictForHTTP(allowSecretExport bool) {
	needsBinary := map[string]string{
		"k8s_kubectl": "kubectl",
		"k8s_apply":   "kubectl",
		"k8s_helm":    "helm",
	}
	for tool, bin := range needsBinary {
		s.wrapHandler(tool, func(next tools.Handler) tools.Handler {
			return func(ctx context.Context, args map[string]any) (string, bool) {
				if !binaryAvailable(bin) {
					return tools.ErrResult(fmt.Errorf(
						"%s is not available in this mode: %s is not installed on the MCP server (the Docker image ships without kubectl/helm). "+
							"Use k8s_kubeconfig with embed_token=false and run %s on your machine, or run 'cloudru-vm mcp' over stdio on a host with %s",
						tool, bin, bin, bin)), true
				}
				return next(ctx, args)
			}
		})
	}

	// Arguments that name files or URLs reach the server's own filesystem
	// and network: confine them (http_paths.go, http_kube.go).
	s.wrapHandler("deploy", func(next tools.Handler) tools.Handler {
		return func(ctx context.Context, args map[string]any) (string, bool) {
			if p, _ := args["compose_file"].(string); p != "" {
				if err := insideWorkDir(p); err != nil {
					return tools.ErrResult(fmt.Errorf("compose_file over HTTP: %w", err)), true
				}
			}
			return next(ctx, args)
		}
	})
	s.wrapHandler("k8s_kubectl", func(next tools.Handler) tools.Handler {
		return func(ctx context.Context, args map[string]any) (string, bool) {
			kargs, _ := args["args"].(string)
			if err := kubectlHTTPCheck(strings.Fields(kargs), allowSecretExport); err != nil {
				return tools.ErrResult(err), true
			}
			return next(ctx, args)
		}
	})
	s.wrapHandler("k8s_helm", func(next tools.Handler) tools.Handler {
		return func(ctx context.Context, args map[string]any) (string, bool) {
			if err := helmHTTPCheck(ctx, args); err != nil {
				return tools.ErrResult(err), true
			}
			return next(ctx, args)
		}
	})

	if allowSecretExport {
		return
	}
	// Any other tool may echo a cloud object that carries a credential
	// (a cluster's password, a generated VM password in deploy_apply):
	// such fields are masked in every result.
	for _, t := range s.reg.Tools() {
		s.wrapHandler(t.Name, func(next tools.Handler) tools.Handler {
			return func(ctx context.Context, args map[string]any) (string, bool) {
				text, isErr := next(ctx, args)
				return redactSecrets(text), isErr
			}
		})
	}
	s.wrapHandler("secret_value", func(tools.Handler) tools.Handler {
		return func(context.Context, map[string]any) (string, bool) {
			return tools.ErrResult(fmt.Errorf(
				"secret_value is disabled over HTTP: it returns a decrypted secret. " +
					"Start the server with --allow-secret-export to enable it")), true
		}
	})
	s.wrapHandler("k8s_kubeconfig", func(next tools.Handler) tools.Handler {
		return func(ctx context.Context, args map[string]any) (string, bool) {
			if tools.BoolArg(args, "embed_token", true) {
				return tools.ErrResult(fmt.Errorf(
					"k8s_kubeconfig with an embedded token is disabled over HTTP: the token grants access to the cluster. " +
						"Call it with embed_token=false (kubeconfig with the cloudlogin exec plugin, no credentials inside), " +
						"or start the server with --allow-secret-export")), true
			}
			return next(ctx, args)
		}
	})
	// k8s_kubectl runs on a kubeconfig with the same embedded token, and
	// "kubectl config view --raw" prints it.
	s.wrapHandler("k8s_kubectl", func(next tools.Handler) tools.Handler {
		return func(ctx context.Context, args map[string]any) (string, bool) {
			kargs, _ := args["args"].(string)
			if slices.Contains(strings.Fields(kargs), "config") {
				return tools.ErrResult(fmt.Errorf(
					"k8s_kubectl config is disabled over HTTP: the kubeconfig holds the cluster token. " +
						"Start the server with --allow-secret-export to enable it")), true
			}
			return next(ctx, args)
		}
	})
	s.wrapHandler("cert_private_key", func(tools.Handler) tools.Handler {
		return func(context.Context, map[string]any) (string, bool) {
			return tools.ErrResult(fmt.Errorf(
				"cert_private_key is disabled over HTTP: it returns a private key in plaintext. " +
					"Start the server with --allow-secret-export to enable it")), true
		}
	})
}

// wrapHandler wraps a tool's handler in the registry, so the policy also
// holds for in-process calls (stack_apply drives other tools), and mirrors
// the result into the tools/call dispatch table.
func (s *Server) wrapHandler(name string, wrap func(next tools.Handler) tools.Handler) {
	if !s.reg.Wrap(name, wrap) {
		return
	}
	if t, ok := s.reg.Lookup(name); ok {
		s.handlers[name] = toolHandler(t.Handler)
	}
}
