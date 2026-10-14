package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/shatim-ops/cloudru-vm-cli/internal/config"
	"github.com/shatim-ops/cloudru-vm-cli/internal/output"
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

			if printer.IsJSON() {
				return printer.JSON(map[string]any{
					"profile":           cfg.Profile,
					"key_id":            config.MaskKeyID(cfg.KeyID),
					"credentials_from":  origin,
					"project_id":        cfg.ProjectID,
					"token_expires_at":  tok.ExpiresAt.UTC().Format(time.RFC3339),
					"token_ttl_seconds": int(tok.TTL().Seconds()),
					"token_source":      tokenSource,
				})
			}
			project := cfg.ProjectID
			if project == "" {
				project = "(not set)"
			}
			w := printer.Writer()
			fmt.Fprintf(w, "Profile:     %s\n", cfg.Profile)
			fmt.Fprintf(w, "Key ID:      %s (from %s)\n", config.MaskKeyID(cfg.KeyID), origin)
			fmt.Fprintf(w, "Project ID:  %s\n", project)
			fmt.Fprintf(w, "Token:       valid until %s (%s left, %s)\n",
				tok.ExpiresAt.Local().Format("2006-01-02 15:04:05 MST"),
				tok.TTL().Truncate(time.Minute), tokenSourceText(tokenSource))
			if cfg.ProjectID == "" {
				printer.Hint("Set a project with 'cloudru-vm configure' or CLOUDRU_PROJECT_ID.")
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
