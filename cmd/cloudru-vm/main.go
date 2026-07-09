package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/dzhechko/cloudru-vm-cli/internal/auth"
	"github.com/dzhechko/cloudru-vm-cli/internal/config"
	"github.com/dzhechko/cloudru-vm-cli/internal/deploy"
	"github.com/dzhechko/cloudru-vm-cli/internal/engine"
	clerrors "github.com/dzhechko/cloudru-vm-cli/internal/errors"
	"github.com/dzhechko/cloudru-vm-cli/internal/mcp"
	"github.com/dzhechko/cloudru-vm-cli/internal/output"
	"github.com/dzhechko/cloudru-vm-cli/internal/provision"
	"github.com/dzhechko/cloudru-vm-cli/internal/state"
	"github.com/dzhechko/cloudru-vm-cli/internal/verify"
)

var version = "0.1.0"

// Global flags.
var (
	flagJSON    bool
	flagVerbose bool
	flagNoColor bool
)

func main() {
	rootCmd := newRootCmd()
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "cloudru-vm",
		Short:         "Deploy docker-compose projects to Cloud.ru VMs",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.PersistentFlags().BoolVar(&flagJSON, "json", false, "Output in JSON format")
	cmd.PersistentFlags().BoolVar(&flagVerbose, "verbose", false, "Enable verbose logging")
	cmd.PersistentFlags().BoolVar(&flagNoColor, "no-color", false, "Disable colored output")

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
	)

	return cmd
}

// --- mcp ---

func newMCPCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Run as an MCP server (stdio JSON-RPC) for AI agents",
		Long: `Run cloudru-vm as a Model Context Protocol server over stdio.

The server exposes deploy, status, list, logs, ssh, destroy, list_zones and
list_images as MCP tools, so an AI agent (e.g. Claude Code) can manage Cloud.ru
deployments directly. It speaks newline-delimited JSON-RPC 2.0 on stdin/stdout;
all human-readable output is suppressed (stdout is reserved for the protocol).

Add it to Claude Code with:
  claude mcp add --transport stdio --scope project cloudru-vm -- cloudru-vm mcp

Credentials and project parameters are read from the environment / .cloudru-vm.yaml
exactly as for the regular CLI commands.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			srv := mcp.New(func(overrides map[string]any) (*engine.Engine, error) {
				if overrides == nil {
					overrides = map[string]any{}
				}
				return buildEngine(overrides)
			}, version)
			return srv.Run(os.Stdin, os.Stdout)
		},
	}
}

// buildEngine constructs the Engine from config and global flags.
func buildEngine(flagOverrides map[string]any) (*engine.Engine, error) {
	flagOverrides["JSON"] = flagJSON
	flagOverrides["Verbose"] = flagVerbose
	flagOverrides["NoColor"] = flagNoColor

	cfgPath := defaultConfigPath()
	cfg, err := config.Load(cfgPath, flagOverrides)
	if err != nil {
		return nil, err
	}

	printer := output.New(cfg.JSON, cfg.NoColor)

	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("get working directory: %w", err)
	}

	privKey, pubKey, err := loadOrGenerateSSHKey(cwd)
	if err != nil {
		return nil, fmt.Errorf("SSH key: %w", err)
	}

	return &engine.Engine{
		Config:    cfg,
		Auth:      auth.NewEnvVarProvider(nil),
		Cloud:     provision.NewCloudRuAdapter(nil, ""),
		Deployer:  deploy.NewSSHDeployer(),
		Printer:   printer,
		SSHKeyPEM: privKey,
		SSHPubKey: pubKey,
		StateDir:  cwd,
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

func defaultConfigPath() string {
	// Check current directory first, then home directory.
	local := ".cloudru-vm.yaml"
	if _, err := os.Stat(local); err == nil {
		return local
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".cloudru-vm.yaml")
}

// handleError prints a CLIError with its hint or a plain error.
func handleError(cmd *cobra.Command, err error) {
	if cliErr, ok := err.(*clerrors.CLIError); ok {
		printer := output.New(flagJSON, flagNoColor)
		printer.Errorf("%s", cliErr.Message)
		if cliErr.Hint != "" {
			printer.Hint(cliErr.Hint)
		}
	} else {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
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
				handleError(cmd, err)
				return err
			}

			opts := engine.DeployOptions{
				ComposeFile:    eng.Config.ComposeFile,
				Flavor:         eng.Config.Flavor,
				Region:         eng.Config.Region,
				Force:          force,
				NonInteractive: nonInteractive,
			}

			if err := eng.Deploy(context.Background(), opts); err != nil {
				handleError(cmd, err)
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
	cmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, "Skip confirmation prompts")

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
				handleError(cmd, err)
				return err
			}
			if err := eng.Status(context.Background()); err != nil {
				handleError(cmd, err)
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
				handleError(cmd, err)
				return err
			}

			opts := engine.LogsOptions{
				Service: service,
				Tail:    tail,
				Follow:  follow,
			}

			if err := eng.Logs(context.Background(), opts); err != nil {
				handleError(cmd, err)
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
				handleError(cmd, err)
				return err
			}

			opts := engine.DestroyOptions{Force: force}
			if err := eng.Destroy(context.Background(), opts); err != nil {
				handleError(cmd, err)
				return err
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")

	return cmd
}

// --- verify ---

func newVerifyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "verify",
		Short: "Run health checks against the deployed application",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := buildEngine(map[string]any{})
			if err != nil {
				handleError(cmd, err)
				return err
			}

			cwd, _ := os.Getwd()
			statePath := state.DefaultPath(cwd)
			stateFile, err := state.Read(statePath)
			if err != nil {
				handleError(cmd, err)
				return err
			}

			active := stateFile.ActiveDeployment()
			if active == nil {
				eng.Printer.Success("No active deployment to verify.")
				eng.Printer.Hint("Run 'cloudru-vm deploy' first.")
				return nil
			}

			if len(active.ExposedPorts) == 0 {
				eng.Printer.Success("No exposed ports to verify.")
				return nil
			}

			results := verify.Check(context.Background(), active.VMIP, active.ExposedPorts, 10*time.Second)

			if eng.Printer.IsJSON() {
				return eng.Printer.JSON(results)
			}

			headers := []string{"Port", "Status", "Healthy", "Latency"}
			var rows [][]string
			for _, r := range results {
				healthy := "yes"
				status := fmt.Sprintf("%d", r.StatusCode)
				if !r.Healthy {
					healthy = "no"
					if r.Error != "" {
						status = r.Error
					}
				}
				rows = append(rows, []string{
					fmt.Sprintf("%d", r.Port),
					status,
					healthy,
					r.Latency.Round(time.Millisecond).String(),
				})
			}
			eng.Printer.Table(headers, rows)
			return nil
		},
	}
}

// --- init ---

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize a cloudru-vm project with a sample config",
		RunE: func(cmd *cobra.Command, args []string) error {
			printer := output.New(flagJSON, flagNoColor)

			cfgFile := ".cloudru-vm.yaml"
			if _, err := os.Stat(cfgFile); err == nil {
				printer.Warning(cfgFile + " already exists. Skipping.")
				return nil
			}

			content := `# cloudru-vm configuration
# See: https://github.com/dzhechko/cloudru-vm-cli

# Availability zone (cloudru-vm list-zones)
region: 7c99a597-8516-494f-a2c7-d7377048681e  # ru.AZ-1

# Boot image (cloudru-vm list-images)
image_id: 474c9e98-760f-4e54-aaa9-70024814f2b0  # ubuntu-22.04

# VM flavor: auto picks the smallest that fits your compose services
flavor: auto

# Boot disk size in GB
disk_size: 10

# Docker Compose file to deploy
compose_file: docker-compose.yml

# Operation timeout in seconds
timeout: 300

# Cloud.ru project UUID (required — get from console.cloud.ru)
# project_id: <your-project-uuid>
`
			if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
				printer.Errorf("failed to create config: %v", err)
				return err
			}

			printer.Success("Created " + cfgFile)
			printer.Hint("Set project_id in " + cfgFile + " and export CLOUDRU_KEY_ID + CLOUDRU_SECRET, then run 'cloudru-vm deploy'")
			return nil
		},
	}
}

// --- version ---

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("cloudru-vm %s\n", version)
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
Use --json for machine-readable output.

Available zones:
  7c99a597-8516-494f-a2c7-d7377048681e  ru.AZ-1
  479a4ab3-3ff3-4972-95c5-7610bac5c0bb  ru.AZ-2
  2c63c482-2532-4bba-8c9b-70ea330507bf  ru.AZ-3`,
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := buildEngine(map[string]any{})
			if err != nil {
				handleError(cmd, err)
				return err
			}

			ctx := context.Background()

			tok, err := eng.Auth.Token(ctx)
			if err != nil {
				handleError(cmd, err)
				return err
			}
			eng.Cloud.SetToken(tok.Value)

			zones, err := eng.Cloud.ListAvailabilityZones(ctx)
			if err != nil {
				handleError(cmd, err)
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
Use --json for machine-readable output.

Common images:
  474c9e98-760f-4e54-aaa9-70024814f2b0  ubuntu-22.04
  fe51a6e6-a5ad-410e-b441-1a195208a8cf  Ubuntu-24.04
  f4f7df21-fa2f-470f-99f2-a4b778f938f1  CentOS-9
  d3405a96-e07e-40e8-abb3-bd9b151ac543  debian-11
  c3cf7d12-5e64-4d4d-b258-f3d2991daf9d  Docker
  520eea4d-d40c-46b0-be7d-a99e9b2aef33  fedora-41`,
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := buildEngine(map[string]any{})
			if err != nil {
				handleError(cmd, err)
				return err
			}

			if err := eng.ListImages(context.Background()); err != nil {
				handleError(cmd, err)
				return err
			}
			return nil
		},
	}
}
