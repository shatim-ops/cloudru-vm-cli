package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/shatim-ops/cloudru-vm-cli/internal/auth"
	"github.com/shatim-ops/cloudru-vm-cli/internal/config"
	clerrors "github.com/shatim-ops/cloudru-vm-cli/internal/errors"
	"github.com/shatim-ops/cloudru-vm-cli/internal/output"
)

// orgProjectsURL lists the projects visible to the service account.
var orgProjectsURL = "https://organization.api.cloud.ru/v3/projects"

// Terminal access, replaceable in tests.
var (
	stdinIsTerminal = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }
	readPassword    = func() ([]byte, error) { return term.ReadPassword(int(os.Stdin.Fd())) }
	// saveTerminal returns a function that puts the terminal back as it is
	// now (echo on), for when Ctrl-C abandons a ReadPassword.
	saveTerminal = func() func() {
		fd := int(os.Stdin.Fd())
		st, err := term.GetState(fd)
		if err != nil {
			return func() {}
		}
		return func() { _ = term.Restore(fd, st) }
	}
)

const configureHelp = `Store a service account access key and a default project in a profile.

Interactively asks for the key ID and the secret (not echoed), checks them
against Cloud.ru IAM, then lists the projects the key can see and lets you
pick one (a single project is picked automatically).

Non-interactive (CI, scripts) — the secret is read from stdin, never from a flag:
  printf '%s' "$SECRET" | cloudru-vm configure --key-id KEY_ID --secret-stdin --project-id PROJECT_ID

Files (mode 0600, directory 0700; CLOUDRU_CONFIG_DIR overrides the directory):
  <user config dir>/cloudru-vm/config.yaml        project_id, region, defaults
  <user config dir>/cloudru-vm/credentials.yaml   key_id, secret, optional s3 section

Required access: the service account needs a project role that allows managing
Compute and VPC resources; listing projects also needs read access to the
organization (otherwise pass --project-id). Details:
https://github.com/shatim-ops/cloudru-vm-cli/blob/main/docs/auth.md`

func newConfigureCmd() *cobra.Command {
	var opts configureOptions
	cmd := &cobra.Command{
		Use:   "configure",
		Short: "Set up credentials and the default project for a profile",
		Long:  configureHelp,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			printer := output.New(flagJSON, flagNoColor)
			con := newConsole(os.Stdin, printer.ErrWriter(), !opts.secretStdin && stdinIsTerminal())
			return runConfigure(cmd.Context(), opts, con, printer)
		},
	}
	cmd.Flags().StringVar(&opts.keyID, "key-id", "", "Access key ID of the service account")
	cmd.Flags().BoolVar(&opts.secretStdin, "secret-stdin", false, "Read the secret from the first line of stdin (non-interactive)")
	cmd.Flags().StringVar(&opts.projectID, "project-id", "", "Cloud.ru project UUID (skips the project list)")
	cmd.Flags().StringVar(&opts.region, "region", "", "Default availability zone UUID for the profile")

	cmd.AddCommand(newConfigureListCmd(), newConfigureDeleteCmd())
	return cmd
}

type configureOptions struct {
	keyID       string
	secretStdin bool
	projectID   string
	region      string
}

// console reads answers from the user; prompts go to stderr.
type console struct {
	in          *bufio.Reader
	out         io.Writer
	interactive bool
}

func newConsole(in io.Reader, out io.Writer, interactive bool) *console {
	return &console{in: bufio.NewReader(in), out: out, interactive: interactive}
}

// readLine reads one trimmed line, giving up when ctx is cancelled: the
// CLI catches SIGINT, so a read that ignores ctx makes Ctrl-C do nothing.
func (c *console) readLine(ctx context.Context) (string, error) {
	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := c.in.ReadString('\n')
		if err != nil && (err != io.EOF || line == "") {
			ch <- result{"", err}
			return
		}
		ch <- result{strings.TrimSpace(line), nil}
	}()
	select {
	case r := <-ch:
		return r.line, r.err
	case <-ctx.Done():
		fmt.Fprintln(c.out)
		return "", ctx.Err()
	}
}

func (c *console) ask(ctx context.Context, prompt string) (string, error) {
	fmt.Fprint(c.out, prompt)
	return c.readLine(ctx)
}

func (c *console) askSecret(ctx context.Context, prompt string) (string, error) {
	fmt.Fprint(c.out, prompt)
	restore := saveTerminal()
	type result struct {
		b   []byte
		err error
	}
	ch := make(chan result, 1)
	read := readPassword // the goroutine may outlive this call
	go func() {
		b, err := read()
		ch <- result{b, err}
	}()
	select {
	case r := <-ch:
		fmt.Fprintln(c.out)
		if r.err != nil {
			return "", r.err
		}
		return strings.TrimSpace(string(r.b)), nil
	case <-ctx.Done():
		// ReadPassword turned echo off and will never restore it: the
		// process exits first.
		restore()
		fmt.Fprintln(c.out)
		return "", ctx.Err()
	}
}

func runConfigure(ctx context.Context, opts configureOptions, con *console, printer *output.Printer) error {
	name, _ := config.ProfileName(flagProfile)
	if err := config.ValidateProfileName(name); err != nil {
		return err
	}
	prof, creds, _, err := config.LoadProfile(name)
	if err != nil {
		return err
	}
	if con.interactive {
		fmt.Fprintf(con.out, "Configuring profile %q in %s\n", name, config.Dir())
	}

	// Key ID.
	keyID := opts.keyID
	if keyID == "" && con.interactive {
		prompt := "Access key ID: "
		if creds.KeyID != "" {
			prompt = fmt.Sprintf("Access key ID [%s]: ", config.MaskKeyID(creds.KeyID))
		}
		if keyID, err = con.ask(ctx, prompt); err != nil {
			return fmt.Errorf("read key ID: %w", err)
		}
	}
	if keyID == "" {
		keyID = creds.KeyID
	}

	// Secret.
	var secret string
	switch {
	case opts.secretStdin:
		if secret, err = con.readLine(ctx); err != nil {
			if ctx.Err() != nil {
				return err
			}
			return clerrors.Wrap(clerrors.CodeConfigInvalid, "no secret on stdin",
				"Pipe the secret into 'cloudru-vm configure --secret-stdin'", err)
		}
	case con.interactive:
		prompt := "Secret: "
		if creds.Secret != "" && keyID == creds.KeyID {
			prompt = "Secret [keep current]: "
		}
		if secret, err = con.askSecret(ctx, prompt); err != nil {
			return fmt.Errorf("read secret: %w", err)
		}
	}
	if secret == "" && keyID == creds.KeyID {
		secret = creds.Secret
	}
	if keyID == "" || secret == "" {
		return clerrors.New(clerrors.CodeConfigInvalid, "key ID and secret are required",
			"Run 'cloudru-vm configure' in a terminal, or pass --key-id and --secret-stdin")
	}

	// Check the key with IAM itself: a cached token of the same key ID
	// proves nothing about this secret. Keep the token for the next commands.
	provider := auth.NewIAMProvider(auth.Credentials{KeyID: keyID, Secret: secret}, httpClient, nil)
	tok, err := provider.Token(ctx)
	if err != nil {
		return fmt.Errorf("checking the access key: %w", err)
	}
	_ = auth.FileCache{Path: config.TokenCachePath(name)}.Store(keyID, tok)

	projectID := opts.projectID
	if projectID == "" {
		if projectID, err = chooseProject(ctx, tok.Value, prof.ProjectID, con, printer); err != nil {
			return err
		}
	}

	prof.ProjectID = projectID
	if opts.region != "" {
		prof.Region = opts.region
	}
	creds.KeyID, creds.Secret = keyID, secret
	if err := config.SaveProfile(name, prof, creds); err != nil {
		return err
	}

	if printer.IsJSON() {
		return printer.JSON(map[string]any{
			"profile":    name,
			"key_id":     config.MaskKeyID(keyID),
			"project_id": projectID,
			"config_dir": config.Dir(),
		})
	}
	printer.Success(fmt.Sprintf("Profile %q saved to %s", name, config.Dir()))
	if name != config.DefaultProfile {
		printer.Hint(fmt.Sprintf("Use it with --profile %s or CLOUDRU_PROFILE=%s", name, name))
	}
	return nil
}

type orgProject struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// listProjects calls the Organization API with an IAM token.
func listProjects(ctx context.Context, token string) ([]orgProject, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, orgProjectsURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, clerrors.Wrap(clerrors.CodeAPIUnreachable, "failed to reach the Organization API",
			"Check your network connection, or pass --project-id", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		code := clerrors.CodeAPIError
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			code = clerrors.CodeAuthFailed
		}
		msg := fmt.Sprintf("listing projects failed with status %d", resp.StatusCode)
		var apiErr struct {
			Message string `json:"message"`
		}
		if json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&apiErr) == nil && apiErr.Message != "" {
			msg += ": " + apiErr.Message
		}
		// Observed 2026-10-14: for a service account the API answers
		// 400 "(user_id) is invalid argument: user not found".
		return nil, clerrors.New(code, msg,
			"The Organization API may not list projects for this key; pass --project-id (find it in the Cloud.ru console)")
	}
	var body struct {
		Projects []orgProject `json:"projects"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, clerrors.Wrap(clerrors.CodeAPIError, "unexpected response from the Organization API",
			"Pass --project-id", err)
	}
	return body.Projects, nil
}

// chooseProject picks the project for the profile: the only one, the
// user's choice, or the current one when it is still in the list.
func chooseProject(ctx context.Context, token, current string, con *console, printer *output.Printer) (string, error) {
	projects, err := listProjects(ctx, token)
	if err != nil {
		if !con.interactive {
			if current != "" {
				// Re-running configure to rotate the key keeps the project.
				printer.Warning(fmt.Sprintf("could not list projects (%v); keeping project %s", err, current))
				return current, nil
			}
			return "", err
		}
		fmt.Fprintf(con.out, "Could not list projects (%v).\n", err)
		return askProjectID(ctx, con, current)
	}

	switch {
	case len(projects) == 1:
		p := projects[0]
		if !printer.IsJSON() {
			fmt.Fprintf(con.out, "Using project %s (%s)\n", p.Name, p.ID)
		}
		return p.ID, nil
	case len(projects) == 0:
		if !con.interactive {
			return "", clerrors.New(clerrors.CodeConfigInvalid, "the key sees no projects",
				"Pass --project-id, or grant the service account access to a project")
		}
		fmt.Fprintln(con.out, "The key sees no projects.")
		return askProjectID(ctx, con, current)
	}

	if !con.interactive {
		for _, p := range projects {
			if p.ID == current {
				return current, nil
			}
		}
		ids := make([]string, len(projects))
		for i, p := range projects {
			ids[i] = p.ID + " (" + p.Name + ")"
		}
		return "", clerrors.New(clerrors.CodeConfigInvalid, "several projects available, none selected",
			"Pass --project-id, one of: "+strings.Join(ids, ", "))
	}

	def := 0
	fmt.Fprintln(con.out, "Projects:")
	for i, p := range projects {
		mark := " "
		if p.ID == current {
			mark, def = "*", i+1
		}
		fmt.Fprintf(con.out, " %s%2d) %s  %s\n", mark, i+1, p.ID, p.Name)
	}
	for {
		prompt := "Choose a project (number or ID): "
		if def > 0 {
			prompt = fmt.Sprintf("Choose a project (number or ID) [%d]: ", def)
		}
		ans, err := con.ask(ctx, prompt)
		if err != nil {
			return "", fmt.Errorf("read project: %w", err)
		}
		if ans == "" && def > 0 {
			return projects[def-1].ID, nil
		}
		if n, err := strconv.Atoi(ans); err == nil && n >= 1 && n <= len(projects) {
			return projects[n-1].ID, nil
		}
		for _, p := range projects {
			if ans == p.ID {
				return p.ID, nil
			}
		}
		fmt.Fprintln(con.out, "Not in the list, try again.")
	}
}

func askProjectID(ctx context.Context, con *console, current string) (string, error) {
	prompt := "Project ID: "
	if current != "" {
		prompt = fmt.Sprintf("Project ID [%s]: ", current)
	}
	id, err := con.ask(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("read project ID: %w", err)
	}
	if id == "" {
		id = current
	}
	if id == "" {
		return "", clerrors.New(clerrors.CodeConfigInvalid, "project ID is required",
			"Find it in the Cloud.ru console, or pass --project-id")
	}
	return id, nil
}

func newConfigureListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List stored profiles",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			printer := output.New(flagJSON, flagNoColor)
			profiles, err := config.Profiles()
			if err != nil {
				return err
			}
			active, _ := config.ProfileName(flagProfile)
			if printer.IsJSON() {
				return printer.JSON(map[string]any{"profiles": profiles, "active": active, "config_dir": config.Dir()})
			}
			if len(profiles) == 0 {
				printer.Success("No profiles in " + config.Dir())
				printer.Hint("Run 'cloudru-vm configure' to create one.")
				return nil
			}
			rows := make([][]string, len(profiles))
			for i, p := range profiles {
				name := p.Name
				if name == active {
					name += " *"
				}
				rows[i] = []string{name, p.ProjectID, p.KeyID, yesNo(p.HasSecret), yesNo(p.HasS3)}
			}
			printer.Table([]string{"Profile", "Project ID", "Key ID", "Secret", "S3"}, rows)
			return nil
		},
	}
}

func newConfigureDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <profile>",
		Short: "Delete a stored profile and its cached token",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			printer := output.New(flagJSON, flagNoColor)
			if err := config.DeleteProfile(args[0]); err != nil {
				return err
			}
			if printer.IsJSON() {
				return printer.JSON(map[string]any{"deleted": args[0]})
			}
			printer.Success(fmt.Sprintf("Profile %q deleted", args[0]))
			return nil
		},
	}
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
