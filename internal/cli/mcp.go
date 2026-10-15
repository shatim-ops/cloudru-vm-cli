package cli

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	clog "github.com/shatim-ops/evo-cli/internal/log"
	"github.com/shatim-ops/evo-cli/internal/mcp"
)

// envBearerToken holds the token clients must present in HTTP mode. It is
// read from the environment only: a flag would show up in ps and in the
// container spec.
const envBearerToken = "MCP_BEARER_TOKEN"

// Authentication modes of the HTTP transport.
const (
	authBearer = "bearer"
	authNone   = "none"
)

func newMCPCmd() *cobra.Command {
	var (
		httpAddr          string
		authMode          string
		allowSecretExport bool
		allowProjects     []string
	)
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run as an MCP server (stdio, or Streamable HTTP with --http) for AI agents",
		Long: `Run evo as a Model Context Protocol server.

The server exposes the evo tools (VMs, disks, networks, Kubernetes,
databases, ...) so an AI agent (e.g. Claude Code) can manage Cloud.ru
resources directly.

By default it speaks newline-delimited JSON-RPC 2.0 on stdin/stdout; all
human-readable output is suppressed (stdout is reserved for the protocol).
SIGINT/SIGTERM and notifications/cancelled cancel running tool calls;
closing stdin does not (calls already sent finish and are answered). Add it to Claude Code with:
  claude mcp add --transport stdio --scope project evo -- evo mcp

With --http ADDR it serves the MCP Streamable HTTP transport on
http://ADDR/mcp (and an unauthenticated liveness probe on /healthz):
  MCP_BEARER_TOKEN=... evo mcp --http :8080
  claude mcp add --transport http evo http://localhost:8080/mcp \
    --header "Authorization: Bearer $MCP_BEARER_TOKEN"

HTTP authentication (--auth):
  bearer  clients send "Authorization: Bearer <token>"; the token is taken
          from $MCP_BEARER_TOKEN, and the server refuses to start without it
  none    no authentication; only allowed on a loopback address
          (--http 127.0.0.1:8080)

Over HTTP, k8s_kubeconfig with an embedded token, cert_private_key and
secret_value are disabled, and credential fields (passwords, keys, tokens) in
other results are masked, unless --allow-secret-export is set. Arguments that
reach the server's host are confined: deploy's compose_file to the working
directory, k8s_kubectl to built-in commands without host flags (--kubeconfig,
-f, ...), k8s_helm to remote charts on public https/oci hosts.
k8s_kubectl/k8s_apply/k8s_helm answer with an error when kubectl/helm is not
installed (the Docker image has neither). At most 32 tool calls run at once
and a batch holds at most 100 messages. A tool call is cancelled when its
client closes the connection, deletes the session (DELETE /mcp) or sends
notifications/cancelled, and when the server stops; there is no timeout. One log line per tool call (name,
status, duration; never the arguments) goes to stderr.

Cloud.ru credentials and project parameters are the server's own, resolved
exactly as for the regular CLI commands (environment, profile from
'evo configure', .evo.yaml); clients never send them.

Projects: deploy and provision accept a project_id argument, so with a key
that sees several projects (an organization-level key) any client of the
server can work in any of them. --allow-project limits the tools to the
listed projects (repeat the flag or separate with commas): a call in any other
project, the server's own included, fails. The read-only org_quotas and
iam_service_accounts still read other projects. Without the flag the server
warns at start in HTTP mode. Prefer a project-level key for a shared server.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			srv := mcp.New(func(overrides map[string]any) (*engine.Engine, error) {
				if overrides == nil {
					overrides = map[string]any{}
				}
				// The SSH key stays lazy here too: only the tools that
				// reach a VM call EnsureSSHKey.
				eng, err := buildEngine(overrides)
				if err != nil {
					return nil, err
				}
				if err := checkAllowedProject(allowProjects, eng.Config.ProjectID); err != nil {
					return nil, err
				}
				return eng, nil
			}, version)

			if httpAddr == "" {
				if cmd.Flags().Changed("auth") || allowSecretExport {
					return clerrors.New(clerrors.CodeUsage,
						"--auth and --allow-secret-export apply to the HTTP transport only",
						"Add --http ADDR, e.g. --http 127.0.0.1:8080")
				}
				return runMCPStdio(cmd.Context(), srv)
			}

			token, err := httpAuthToken(httpAddr, authMode, os.Getenv(envBearerToken))
			if err != nil {
				return err
			}
			logger := clog.New(cmd.ErrOrStderr(), flagVerbose)
			if token != "" && len(token) < 16 {
				logger.Warn(envBearerToken + " is shorter than 16 characters; use a long random token")
			}
			if len(allowProjects) == 0 {
				logger.Warn("no --allow-project: a client can pass project_id to deploy/provision and work in any project the key sees; " +
					"limit it with --allow-project <UUID> or use a project-level key")
			}
			return srv.ListenAndServe(cmd.Context(), httpAddr, mcp.HTTPOptions{
				BearerToken:       token,
				AllowSecretExport: allowSecretExport,
				Logger:            logger.Logger,
			}, nil)
		},
	}
	cmd.Flags().StringVar(&httpAddr, "http", "", "Serve MCP over Streamable HTTP on this address (e.g. :8080) instead of stdio")
	cmd.Flags().StringVar(&authMode, "auth", authBearer, "HTTP authentication: bearer (token from $MCP_BEARER_TOKEN) or none (loopback address only)")
	cmd.Flags().StringSliceVar(&allowProjects, "allow-project", nil, "Allow tool calls only in these Cloud.ru projects (UUIDs; repeat or comma-separate). Default: any project the key sees")
	cmd.Flags().BoolVar(&allowSecretExport, "allow-secret-export", false, "HTTP mode: allow k8s_kubeconfig with an embedded token, cert_private_key, secret_value and unmasked credentials in results")
	cmd.AddCommand(newMCPHealthcheckCmd())
	return cmd
}

// checkAllowedProject refuses an engine for a project outside --allow-project
// (QA P4). Every tool that reaches the cloud builds its engine through the
// factory, so this covers project_id arguments and the server's own project.
func checkAllowedProject(allowed []string, project string) error {
	if len(allowed) == 0 {
		return nil
	}
	for _, p := range allowed {
		if p == project {
			return nil
		}
	}
	shown := project
	if shown == "" {
		shown = "(not set)"
	}
	return clerrors.New(clerrors.CodeConfigInvalid,
		fmt.Sprintf("project %s is not allowed on this server", shown),
		fmt.Sprintf("The server runs with --allow-project %s; ask its operator to add the project.", strings.Join(allowed, ",")))
}

// runMCPStdio serves the stdio transport.
func runMCPStdio(ctx context.Context, srv *mcp.Server) error {
	// SIGINT/SIGTERM (ctx) cancel the running tool call; RunContext
	// returns once it has wound down (the call records what it created),
	// at most a few seconds later. The end of stdin cancels nothing.
	return srv.RunContext(ctx, os.Stdin, os.Stdout)
}

// httpAuthToken validates the --http/--auth combination and returns the
// bearer token to require ("" for --auth none). The server never listens
// without authentication on a non-loopback address.
func httpAuthToken(addr, mode, envToken string) (string, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return "", clerrors.Wrap(clerrors.CodeUsage, fmt.Sprintf("invalid --http address %q", addr),
			"Use HOST:PORT or :PORT, e.g. --http :8080 or --http 127.0.0.1:8080", err)
	}
	switch mode {
	case authBearer:
		if envToken == "" {
			return "", clerrors.New(clerrors.CodeConfigInvalid,
				envBearerToken+" is not set: the HTTP server does not start without authentication",
				"Set "+envBearerToken+" to a long random string (e.g. openssl rand -hex 32), "+
					"or use --auth none with a loopback address (--http 127.0.0.1:8080)")
		}
		return envToken, nil
	case authNone:
		if !mcp.LoopbackHost(host) { // an empty host (":8080") is every interface
			return "", clerrors.New(clerrors.CodeUsage,
				fmt.Sprintf("--auth none is only allowed on a loopback address, not %q", addr),
				"Use --http 127.0.0.1:PORT for a local server, or --auth bearer with "+envBearerToken)
		}
		return "", nil
	default:
		return "", clerrors.New(clerrors.CodeUsage,
			fmt.Sprintf("unknown --auth %q", mode), "Use --auth bearer or --auth none")
	}
}

// newMCPHealthcheckCmd is the Docker HEALTHCHECK of the image: the
// distroless base has no shell or curl, so the binary probes itself.
func newMCPHealthcheckCmd() *cobra.Command {
	var url string
	cmd := &cobra.Command{
		Use:    "healthcheck",
		Short:  "Probe /healthz of a running HTTP MCP server (exit 0 when healthy)",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if url == "" {
				url = os.Getenv("MCP_HEALTHCHECK_URL")
			}
			if url == "" {
				url = "http://127.0.0.1:8080/healthz"
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				return err
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("healthcheck: %w", err)
			}
			defer resp.Body.Close()
			_, _ = io.Copy(io.Discard, resp.Body)
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("healthcheck: %s returned %s", url, resp.Status)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&url, "url", "", "Health endpoint (default $MCP_HEALTHCHECK_URL or http://127.0.0.1:8080/healthz)")
	return cmd
}
