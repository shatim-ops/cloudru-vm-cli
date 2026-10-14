package main

import (
	"context"
	stderrors "errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/shatim-ops/cloudru-vm-cli/internal/auth"
	"github.com/shatim-ops/cloudru-vm-cli/internal/deploy"
	"github.com/shatim-ops/cloudru-vm-cli/internal/engine"
	clerrors "github.com/shatim-ops/cloudru-vm-cli/internal/errors"
	"github.com/shatim-ops/cloudru-vm-cli/internal/output"
	"github.com/shatim-ops/cloudru-vm-cli/internal/provision"
)

// Build identity. A release build sets all three through goreleaser:
//
//	-ldflags "-X main.version=1.2.3 -X main.commit=<sha> -X main.date=<RFC 3339>"
//
// A plain "go build" / "go install" leaves the defaults, and resolveVersion
// fills them from the module and VCS data the Go toolchain embeds.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// Global flags.
var (
	flagJSON    bool
	flagVerbose bool
	flagNoColor bool
	flagProfile string
	flagYes     bool
	flagDryRun  bool
)

// httpClient is shared by the IAM and compute clients: one connection pool
// and a timeout, so a stalled Cloud.ru endpoint cannot hang the CLI forever.
var httpClient = &http.Client{Timeout: 60 * time.Second}

// commandStarted is set once cobra has parsed flags and arguments and is
// about to run a command. An error before that is a usage error.
var commandStarted bool

func main() {
	os.Exit(run(os.Args[1:]))
}

// run executes the CLI and returns the process exit code.
func run(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		// After the first signal restore the default behaviour, so a second
		// Ctrl-C kills a command that does not watch the context.
		<-ctx.Done()
		stop()
	}()

	commandStarted = false
	rootCmd := newRootCmd()
	rootCmd.SetArgs(args)
	cmd, err := rootCmd.ExecuteContextC(ctx)
	if err == nil {
		return clerrors.ExitOK
	}
	if ctx.Err() != nil && !stderrors.Is(err, context.Canceled) {
		// The command was interrupted but reported it as something else.
		err = fmt.Errorf("%w: %w", context.Canceled, err)
	}
	if cmd == nil {
		cmd = rootCmd
	}
	if !commandStarted {
		err = usageError(cmd, err)
	}
	handleError(output.New(flagJSON, flagNoColor), err)
	return clerrors.ExitCode(err)
}

// usageError turns a cobra parsing error (unknown command or flag, bad
// argument) into a CLIError pointing at --help.
func usageError(cmd *cobra.Command, err error) error {
	return clerrors.Wrap(clerrors.CodeUsage, err.Error(),
		fmt.Sprintf("Run '%s --help' for usage.", cmd.CommandPath()), err)
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cloudru-vm",
		Short: "Deploy docker-compose projects to Cloud.ru VMs",
		Long: "Deploy docker-compose projects to Cloud.ru VMs.\n\n" +
			clerrors.ExitCodesHelp,
		// Errors are printed once, by run(): as text or, with --json, as
		// {"error": {...}} on stderr.
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			commandStarted = true
		},
	}

	cmd.PersistentFlags().BoolVar(&flagJSON, "json", false, "Output in JSON format")
	cmd.PersistentFlags().BoolVar(&flagVerbose, "verbose", false, "Enable verbose logging")
	cmd.PersistentFlags().BoolVar(&flagNoColor, "no-color", false, "Disable colored output")
	cmd.PersistentFlags().StringVar(&flagProfile, "profile", "", "Credentials profile (default: $CLOUDRU_PROFILE or \"default\")")
	cmd.PersistentFlags().BoolVarP(&flagYes, "yes", "y", false, "Confirm operations that create paid resources or delete data, without a prompt")
	cmd.PersistentFlags().BoolVar(&flagDryRun, "dry-run", false, "Show what would be created or deleted (with a cost estimate) and exit")
	addScopeFlags(cmd)

	cmd.AddCommand(
		newDeployCmd(),
		newStatusCmd(),
		newLogsCmd(),
		newDestroyCmd(),
		newVerifyCmd(),
		newInitCmd(),
		newVersionCmd(),
		newListZonesCmd(),
		newListImagesCmd(),
		newMCPCmd(),
		newConfigureCmd(),
		newWhoamiCmd(),
		newDoctorCmd(),
		newToolsCmd(),
	)
	cmd.AddCommand(newTreeCmds()...)

	return cmd
}

// buildEngine constructs the Engine from config and global flags.
func buildEngine(flagOverrides map[string]any) (*engine.Engine, error) {
	flagOverrides["JSON"] = flagJSON
	flagOverrides["Verbose"] = flagVerbose
	flagOverrides["NoColor"] = flagNoColor
	applyScopeFlags(flagOverrides)

	printer := output.New(flagJSON, flagNoColor)
	cfg, err := resolveConfig(printer, flagOverrides)
	if err != nil {
		return nil, err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("get working directory: %w", err)
	}

	return &engine.Engine{
		Config:   cfg,
		Auth:     tokenProvider(cfg),
		Cloud:    provision.NewCloudRuAdapter(httpClient, ""),
		Deployer: deploy.NewSSHDeployer(),
		Printer:  printer,
		StateDir: cwd,
		// Created on first use: list-zones and friends leave no key behind.
		SSHKeyLoader: func() ([]byte, string, error) { return loadOrGenerateSSHKey(cwd) },
	}, nil
}

// loadOrGenerateSSHKey loads an existing SSH key pair from .cloudru-vm/ or
// generates a new one and persists it for future use.
func loadOrGenerateSSHKey(projectDir string) (privPEM []byte, pubKey string, err error) {
	keyDir := filepath.Join(projectDir, ".cloudru-vm")
	privPath := filepath.Join(keyDir, "id_ed25519")
	pubPath := filepath.Join(keyDir, "id_ed25519.pub")

	// Try loading existing key pair.
	privPEM, errPriv := os.ReadFile(privPath)
	pubBytes, errPub := os.ReadFile(pubPath)
	if errPriv == nil && errPub == nil && len(privPEM) > 0 && len(pubBytes) > 0 {
		return privPEM, string(pubBytes), nil
	}

	// Generate new key pair.
	privPEM, pubKey, err = auth.GenerateSSHKeyPair()
	if err != nil {
		return nil, "", err
	}

	// Persist to disk.
	if err := os.MkdirAll(keyDir, 0700); err != nil {
		return nil, "", fmt.Errorf("create key directory: %w", err)
	}
	if err := os.WriteFile(privPath, privPEM, 0600); err != nil {
		return nil, "", fmt.Errorf("write private key: %w", err)
	}
	if err := os.WriteFile(pubPath, []byte(pubKey), 0644); err != nil {
		return nil, "", fmt.Errorf("write public key: %w", err)
	}

	return privPEM, pubKey, nil
}

// projectConfigPath is the project file with per-project overrides. User
// settings and secrets live in the profile (see 'cloudru-vm configure').
func projectConfigPath() string {
	return ".cloudru-vm.yaml"
}

// handleError prints err to the printer's error writer: as text with the
// hint of the CLIError found anywhere in the wrap chain, or, in JSON mode,
// as {"error": {"code": N, "message": "...", "hint": "..."}}.
func handleError(p *output.Printer, err error) {
	code := clerrors.ExitCode(err)
	var message, hint string
	var cliErr *clerrors.CLIError
	switch {
	case code == clerrors.ExitInterrupted:
		message = "interrupted"
		if stderrors.As(err, &cliErr) {
			// e.g. deploy: which VM was left running and how to delete it.
			message += ": " + cliErr.Message
			hint = cliErr.Hint
		}
	case stderrors.As(err, &cliErr):
		hint = cliErr.Hint
		if err == error(cliErr) {
			message = cliErr.Message
		} else {
			// Keep the context added by fmt.Errorf("...: %w").
			message = err.Error()
		}
	default:
		message = err.Error()
	}

	if p.IsJSON() {
		_ = p.JSONError(code, message, hint)
		return
	}
	p.Errorf("%s", message)
	if hint != "" {
		p.Hint(hint)
	}
}

// --- deploy ---

func newDeployCmd() *cobra.Command {
	var (
		composeFile    string
		flavor         string
		region         string
		projectID      string
		imageID        string
		diskSize       int
		force          bool
		nonInteractive bool
	)

	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy a docker-compose project to a Cloud.ru VM",
		RunE: func(cmd *cobra.Command, args []string) error {
			flags := map[string]any{}
			if cmd.Flags().Changed("compose-file") {
				flags["ComposeFile"] = composeFile
			}
			if cmd.Flags().Changed("flavor") {
				flags["Flavor"] = flavor
			}
			if cmd.Flags().Changed("region") {
				flags["Region"] = region
			}
			if cmd.Flags().Changed("project-id") {
				flags["ProjectID"] = projectID
			}
			if cmd.Flags().Changed("image-id") {
				flags["ImageID"] = imageID
			}
			if cmd.Flags().Changed("disk-size") {
				flags["DiskSize"] = diskSize
			}

			eng, err := buildEngine(flags)
			if err != nil {
				return err
			}

			opts := engine.DeployOptions{
				ComposeFile:    eng.Config.ComposeFile,
				Flavor:         eng.Config.Flavor,
				Region:         eng.Config.Region,
				Force:          force,
				NonInteractive: nonInteractive,
				Yes:            flagYes,
				DryRun:         flagDryRun,
			}

			if err := eng.Deploy(cmd.Context(), opts); err != nil {
				return err
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&composeFile, "compose-file", "f", "", "Path to docker-compose file (default: docker-compose.yml)")
	cmd.Flags().StringVar(&flavor, "flavor", "", "VM flavor (default: auto)")
	cmd.Flags().StringVar(&region, "region", "", "Availability zone UUID")
	cmd.Flags().StringVar(&projectID, "project-id", "", "Cloud.ru project UUID")
	cmd.Flags().StringVar(&imageID, "image-id", "", "Boot image UUID")
	cmd.Flags().IntVar(&diskSize, "disk-size", 0, "Boot disk size in GB (default: 10)")
	cmd.Flags().BoolVar(&force, "force", false, "Force redeploy even if compose hash unchanged")
	cmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, "Never prompt; creating a VM then requires --yes")

	return cmd
}

// --- status ---

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show current deployment status",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := buildEngine(map[string]any{})
			if err != nil {
				return err
			}
			if err := eng.Status(cmd.Context()); err != nil {
				return err
			}
			return nil
		},
	}
}

// --- logs ---

func newLogsCmd() *cobra.Command {
	var (
		service string
		tail    int
		follow  bool
	)

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "View container logs from the deployed VM",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := buildEngine(map[string]any{})
			if err != nil {
				return err
			}

			opts := engine.LogsOptions{
				Service: service,
				Tail:    tail,
				Follow:  follow,
			}

			if err := eng.Logs(cmd.Context(), opts); err != nil {
				return err
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&service, "service", "s", "", "Filter logs by service name")
	cmd.Flags().IntVarP(&tail, "tail", "n", 100, "Number of lines to show (0 = all)")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow log output")

	return cmd
}

// --- destroy ---

func newDestroyCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "destroy",
		Short: "Delete the deployed VM and clean up resources",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := buildEngine(map[string]any{})
			if err != nil {
				return err
			}

			opts := engine.DestroyOptions{Force: force || flagYes, DryRun: flagDryRun}
			if err := eng.Destroy(cmd.Context(), opts); err != nil {
				return err
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt (same as --yes)")

	return cmd
}

// --- verify ---

func newVerifyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "verify",
		Short: "Run health checks against the deployed application (exit 1 if unhealthy)",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := buildEngine(map[string]any{})
			if err != nil {
				return err
			}
			return eng.Verify(cmd.Context())
		},
	}
}

// --- init ---

func newInitCmd() *cobra.Command {
	var nonInteractive bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create .cloudru-vm.yaml with a zone and image chosen from the Cloud.ru API",
		Long: `Create .cloudru-vm.yaml for this project.

The availability zone and the boot image are listed from the Cloud.ru API
(credentials required). On a terminal you choose from a numbered list;
with --non-interactive or without a terminal the first zone and the newest
plain Ubuntu (else Debian) image are taken, and the choice is printed.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := buildEngine(map[string]any{})
			if err != nil {
				return err
			}
			return eng.Init(cmd.Context(), engine.InitOptions{
				Path:           ".cloudru-vm.yaml",
				NonInteractive: nonInteractive,
			})
		},
	}

	cmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, "Take the first zone and the preferred image without asking")

	return cmd
}

// --- version ---

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version, commit, build date, Go version and platform",
		RunE: func(cmd *cobra.Command, args []string) error {
			return printVersion(cmd.OutOrStdout(), flagJSON, currentVersion())
		},
	}
}

// --- list-zones ---

func newListZonesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list-zones",
		Short: "List available availability zones",
		Long: `List availability zones from the Cloud.ru API.

Use the zone ID as the --region flag or CLOUDRU_REGION env var for deploy.
Use --json for machine-readable output.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := buildEngine(map[string]any{})
			if err != nil {
				return err
			}

			ctx := cmd.Context()

			tok, err := eng.Auth.Token(ctx)
			if err != nil {
				return err
			}
			eng.Cloud.SetToken(tok.Value)

			zones, err := eng.Cloud.ListAvailabilityZones(ctx)
			if err != nil {
				return err
			}

			if eng.Printer.IsJSON() {
				return eng.Printer.JSON(zones)
			}

			headers := []string{"ID", "Name"}
			rows := make([][]string, len(zones))
			for i, z := range zones {
				rows[i] = []string{z.ID, z.Name}
			}
			eng.Printer.Table(headers, rows)
			return nil
		},
	}
}

// --- list-images ---

func newListImagesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list-images",
		Short: "List available VM boot images",
		Long: `List boot images from the Cloud.ru API.

Use the image ID as the --image-id flag or CLOUDRU_IMAGE_ID env var for deploy.
deploy installs Docker on Ubuntu and Debian images (e.g. Ubuntu-24.04).
Use --json for machine-readable output.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := buildEngine(map[string]any{})
			if err != nil {
				return err
			}

			if err := eng.ListImages(cmd.Context()); err != nil {
				return err
			}
			return nil
		},
	}
}
