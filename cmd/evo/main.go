package main

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/shatim-ops/cloudru-vm-cli/internal/auth"
	"github.com/shatim-ops/cloudru-vm-cli/internal/config"
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
	if isLegacyAlias(os.Args[0]) {
		printAliasNotice(os.Stderr, os.Args[1:])
	}
	os.Exit(run(os.Args[1:]))
}

// aliasNotice is printed when the binary runs under its legacy name: the
// release archives ship a copy named after the legacy alias for one minor
// release, so scripts and MCP configs written for the old name keep working.
const aliasNotice = config.LegacyAppName + " переименован в " + config.AppName + ", alias будет удалён в v1.1"

// isLegacyAlias reports whether argv0 is the legacy alias binary:
// cloudru-vm (legacy name), or cloudru-vm.exe on Windows.
func isLegacyAlias(argv0 string) bool {
	name := filepath.Base(argv0)
	if ext := filepath.Ext(name); strings.EqualFold(ext, ".exe") {
		name = strings.TrimSuffix(name, ext)
	}
	return name == config.LegacyAppName
}

// printAliasNotice writes the one-line rename notice of the legacy alias to
// stderr; with --json as {"warning": ...}, so stderr stays parseable.
func printAliasNotice(w io.Writer, args []string) {
	if jsonRequested(args) {
		_ = json.NewEncoder(w).Encode(map[string]string{"warning": aliasNotice})
		return
	}
	fmt.Fprintln(w, aliasNotice)
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
	// Until flags are parsed: plain text to stderr.
	config.SetWarnFunc(func(msg string) { output.New(false, true).Warning(msg) })
	if msg, err := config.MigrateLegacyDir(); err != nil {
		output.New(jsonRequested(args), true).Warning(fmt.Sprintf(
			"could not copy the profiles of %s to %s: %v; run '%s configure' again or copy them by hand",
			config.LegacyAppName, config.Dir(), err, config.AppName))
	} else if msg != "" {
		output.New(jsonRequested(args), true).Warning(msg)
	}
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
		// cobra stops at the first bad flag or command, so a --json after
		// it is never parsed; the error must still be JSON.
		flagJSON = flagJSON || jsonRequested(args)
	}
	handleError(output.New(flagJSON, flagNoColor), err)
	return clerrors.ExitCode(err)
}

// jsonRequested reports whether the command line asks for --json, looking
// at the raw arguments up to "--".
func jsonRequested(args []string) bool {
	on := false
	for _, a := range args {
		switch {
		case a == "--":
			return on
		case a == "--json":
			on = true
		case strings.HasPrefix(a, "--json="):
			if b, err := strconv.ParseBool(strings.TrimPrefix(a, "--json=")); err == nil {
				on = b
			}
		}
	}
	return on
}

// usageError turns a cobra parsing error (unknown command or flag, bad
// argument) into a CLIError pointing at --help.
func usageError(cmd *cobra.Command, err error) error {
	return clerrors.Wrap(clerrors.CodeUsage, err.Error(),
		fmt.Sprintf("Run '%s --help' for usage.", cmd.CommandPath()), err)
}

// asGroup makes cmd a command group that shows its help when called alone
// and rejects anything else: by default cobra answers "vm lst" with the
// group's help and exit 0, and a script takes the typo for success.
func asGroup(cmd *cobra.Command) *cobra.Command {
	cmd.SuggestionsMinimumDistance = 2 // cobra's default, set only for the root
	cmd.Args = func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return nil
		}
		msg := fmt.Sprintf("unknown command %q for %q", args[0], cmd.CommandPath())
		if s := cmd.SuggestionsFor(args[0]); len(s) > 0 {
			msg += "; did you mean " + strings.Join(s, ", ") + "?"
		}
		return stderrors.New(msg)
	}
	cmd.RunE = func(cmd *cobra.Command, _ []string) error { return cmd.Help() }
	return cmd
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "evo",
		Short: "Deploy docker-compose projects to Cloud.ru VMs",
		Long: "Deploy docker-compose projects to Cloud.ru VMs.\n\n" +
			clerrors.ExitCodesHelp,
		// Errors are printed once, by run(): as text or, with --json, as
		// {"error": {...}} on stderr.
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			commandStarted = true
			// Warnings about legacy paths (.cloudru-vm/, .cloudru-vm.yaml)
			// follow --json and --no-color from here on.
			config.SetWarnFunc(func(msg string) {
				printWarnings(output.New(flagJSON, flagNoColor), []string{msg})
			})
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
		SSHKeyLoader: func() ([]byte, string, error) {
			priv, pub, err := loadOrGenerateSSHKey(cwd)
			if err == nil {
				printWarnings(printer, sshKeyWarnings(cwd))
			}
			return priv, pub, err
		},
	}, nil
}

// loadOrGenerateSSHKey loads an existing SSH key pair from .evo/ or
// generates a new one and persists it for future use.
//
// .evo/ may come with someone else's repository. A symlink there
// (the directory itself, or a key file pointing at ~/.ssh/authorized_keys or
// a file another user owns) is refused instead of followed: writing through
// it would overwrite that file with our key, or hand the private key to the
// owner of the target.
func loadOrGenerateSSHKey(projectDir string) (privPEM []byte, pubKey string, err error) {
	keyDir := config.DataDir(projectDir)
	privPath := filepath.Join(keyDir, "id_ed25519")
	pubPath := filepath.Join(keyDir, "id_ed25519.pub")

	if err := ensureKeyDir(keyDir); err != nil {
		return nil, "", err
	}
	ignoreInGit(keyDir)

	// Try loading existing key pair.
	privPEM, errPriv := readKeyFile(privPath)
	pubBytes, errPub := readKeyFile(pubPath)
	for _, e := range []error{errPriv, errPub} {
		if e != nil && !os.IsNotExist(e) {
			return nil, "", e
		}
	}
	if errPriv == nil && errPub == nil && len(privPEM) > 0 && len(pubBytes) > 0 {
		return privPEM, string(pubBytes), nil
	}

	// Generate new key pair.
	privPEM, pubKey, err = auth.GenerateSSHKeyPair()
	if err != nil {
		return nil, "", err
	}

	// Persist to disk: temporary file 0600 + rename, so the key is never
	// readable by others and an existing file or symlink lends nothing.
	if err := config.WriteFilePrivate(privPath, privPEM); err != nil {
		return nil, "", fmt.Errorf("write private key: %w", err)
	}
	if err := config.WriteFileAtomic(pubPath, []byte(pubKey), 0o644); err != nil {
		return nil, "", fmt.Errorf("write public key: %w", err)
	}

	return privPEM, pubKey, nil
}

// ensureKeyDir creates the key directory 0700, or checks that the existing
// one is a real directory and tightens it to 0700.
func ensureKeyDir(dir string) error {
	st, err := os.Lstat(dir)
	switch {
	case os.IsNotExist(err):
		if err := config.MkdirPrivate(dir); err != nil {
			return fmt.Errorf("create key directory: %w", err)
		}
		return nil
	case err != nil:
		return fmt.Errorf("key directory: %w", err)
	case !st.IsDir():
		return clerrors.New(clerrors.CodeConfigInvalid,
			fmt.Sprintf("%s is not a directory (a symlink?); refusing to keep the SSH key there", dir),
			"Remove it (it may have come with the repository) and run the command again")
	}
	if config.PermissionsEnforced() && config.LooseMode(st.Mode()) {
		if err := os.Chmod(dir, 0o700); err != nil {
			return fmt.Errorf("restrict key directory: %w", err)
		}
	}
	// Windows: a directory made by an older version inherited the ACL of
	// the project folder. Best effort (FAT has no ACL to set): what stays
	// open is reported by sshKeyWarnings.
	if !config.PermissionsEnforced() && len(config.ForeignAccess(dir, true)) > 0 {
		_ = config.RestrictToOwner(dir, true)
	}
	return nil
}

// sshKeyWarnings reports a key directory or private key other users can
// reach. Only Windows ACLs are checked: on Unix ensureKeyDir makes the
// directory 0700 and the key is written 0600.
func sshKeyWarnings(projectDir string) []string {
	keyDir := config.DataDir(projectDir)
	var out []string
	for _, w := range []string{
		config.PrivateDirWarning(keyDir),
		config.PrivateFileWarning(filepath.Join(keyDir, "id_ed25519")),
	} {
		if w != "" {
			out = append(out, w)
		}
	}
	return out
}

// ignoreInGit puts a .gitignore with "*" into dir, so "git add ." in the
// project never picks up the private key, whatever the project's own
// .gitignore says. Best effort; an existing file (or symlink) is left alone.
func ignoreInGit(dir string) {
	f, err := os.OpenFile(filepath.Join(dir, ".gitignore"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	_, _ = f.WriteString("*\n")
	_ = f.Close()
}

// readKeyFile reads a key file that must be a regular file, not a symlink.
func readKeyFile(path string) ([]byte, error) {
	st, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, clerrors.New(clerrors.CodeConfigInvalid,
			fmt.Sprintf("%s is not a regular file (a symlink?); refusing to use it as the SSH key", path),
			"Remove it (it may have come with the repository) and run the command again: a new key is created")
	}
	return os.ReadFile(path)
}

// projectConfigPath is the project file with per-project overrides. User
// settings and secrets live in the profile (see 'evo configure').
func projectConfigPath() string {
	return config.ProjectFile(".")
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
		Args:  cobra.NoArgs,
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
		Args:  cobra.NoArgs,
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
		Args:  cobra.NoArgs,
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
		Args:  cobra.NoArgs,
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
		Args:  cobra.NoArgs,
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
		Args:  cobra.NoArgs,
		Short: "Create .evo.yaml with a zone and image chosen from the Cloud.ru API",
		Long: `Create .evo.yaml for this project.

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
				Path:           ".evo.yaml",
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
		Args:  cobra.NoArgs,
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
		Args:  cobra.NoArgs,
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
		Args:  cobra.NoArgs,
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
