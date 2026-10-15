package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/output"
)

func newWhoamiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show the active profile and check its credentials",
		Long: `Exchange the access key of the active profile for an IAM token and show
the profile, the key ID (masked), the project and the token expiry.

A valid token cached by an earlier command is reused; IAM is not contacted
again until it is about to expire. Exits with 2 when the credentials are
missing or rejected.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			printer := output.New(flagJSON, flagNoColor)
			cfg, sources, err := resolveConfigWithSources(printer, nil)
			if err != nil {
				return err
			}
			provider := tokenProvider(cfg)
			before := provider.Exchanges()
			tok, err := provider.Token(cmd.Context())
			if err != nil {
				return err
			}
			tokenSource := "cache"
			if provider.Exchanges() != before {
				tokenSource = "iam"
			}
			origin := sourceOrigin(sources, "KeyID")
			projectOrigin := sourceOrigin(sources, "ProjectID")
			visible, scopeErr := listProjects(cmd.Context(), tok.Value)

			if printer.IsJSON() {
				out := map[string]any{
					"profile":           cfg.Profile,
					"key_id":            config.MaskKeyID(cfg.KeyID),
					"credentials_from":  origin,
					"project_id":        cfg.ProjectID,
					"project_from":      projectOrigin,
					"token_expires_at":  tok.ExpiresAt.UTC().Format(time.RFC3339),
					"token_ttl_seconds": int(tok.TTL().Seconds()),
					"token_source":      tokenSource,
				}
				if scopeErr == nil {
					out["visible_projects"] = len(visible)
				}
				return printer.JSON(out)
			}
			project := cfg.ProjectID
			if project == "" {
				project = "(not set)"
			}
			w := printer.Writer()
			fmt.Fprintf(w, "Profile:     %s\n", cfg.Profile)
			fmt.Fprintf(w, "Key ID:      %s (from %s)\n", config.MaskKeyID(cfg.KeyID), origin)
			if cfg.ProjectID != "" {
				project += " (from " + projectOrigin + ")"
			}
			fmt.Fprintf(w, "Project ID:  %s\n", project)
			fmt.Fprintf(w, "Projects:    %s\n", scopeText(visible, scopeErr))
			fmt.Fprintf(w, "Token:       valid until %s (%s left, %s)\n",
				tok.ExpiresAt.Local().Format("2006-01-02 15:04:05 MST"),
				tok.TTL().Truncate(time.Minute), tokenSourceText(tokenSource))
			if cfg.ProjectID == "" {
				printer.Hint("Set a project with 'evo configure' or CLOUDRU_PROJECT_ID.")
			}
			if scopeErr == nil && len(visible) > 1 {
				printer.Warning(manyProjectsWarning(len(visible)))
			}
			return nil
		},
	}
}

func tokenSourceText(s string) string {
	if s == "cache" {
		return "cached"
	}
	return "issued by IAM now"
}

// scopeText says how many projects the key sees. The Organization API does
// not list projects for every key (a service account gets 400 "user not
// found", 14.10.2026): then the scope is unknown, which is not an error.
func scopeText(visible []orgProject, err error) string {
	if err != nil {
		return "unknown (the Organization API does not list projects for this key)"
	}
	if len(visible) == 1 {
		return "1 visible to the key"
	}
	return fmt.Sprintf("%d visible to the key", len(visible))
}

// manyProjectsWarning explains what a key that sees several projects means:
// every command works in the project above, and an MCP server with this key
// can be told to work in any of them (QA P5).
func manyProjectsWarning(n int) string {
	return fmt.Sprintf("the key sees %d projects (organization-level access): commands work in the project above "+
		"unless --project-id says otherwise; limit a shared 'evo mcp --http' server with --allow-project", n)
}
