package cli

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"github.com/shatim-ops/evo-cli/internal/auth"
	"github.com/shatim-ops/evo-cli/internal/config"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/output"
)

// computeBaseURL is probed by doctor; replaceable in tests.
var computeBaseURL = "https://compute.api.cloud.ru"

// doctorProbeTimeout bounds each reachability probe.
const doctorProbeTimeout = 10 * time.Second

// Check statuses.
const (
	checkOK   = "ok"
	checkWarn = "warn"
	checkFail = "fail"
	checkSkip = "skip"
)

type doctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
	// auth marks a failure of configuration or credentials (exit code 2).
	auth bool
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check configuration, credentials and connectivity",
		Long: `Check the setup: config files and their permissions, where each setting
comes from, reachability of the IAM and Compute APIs, the credentials, and
kubectl/helm in PATH (needed only by the k8s tools; a warning, not an error).

Exits with 2 if the configuration or credentials are broken, 1 if only
connectivity checks failed.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			printer := output.New(flagJSON, flagNoColor)
			checks := runDoctor(cmd.Context())
			return reportDoctor(printer, checks)
		},
	}
}

func runDoctor(ctx context.Context) []doctorCheck {
	checks := []doctorCheck{{
		Name: "version", Status: checkOK,
		Detail: fmt.Sprintf("evo %s %s/%s", version, runtime.GOOS, runtime.GOARCH),
	}}
	checks = append(checks, fileChecks()...)

	cfg, sources, err := config.ResolveWithSources(config.Options{
		Profile:     flagProfile,
		ProjectFile: projectConfigPath(),
	})
	if err != nil {
		checks = append(checks, doctorCheck{Name: "config", Status: checkFail, Detail: errDetail(err), auth: true})
	} else {
		checks = append(checks, configChecks(cfg, sources)...)
	}

	checks = append(checks,
		probe(ctx, "IAM API", auth.IAMEndpoint),
		probe(ctx, "Compute API", computeBaseURL),
	)

	switch {
	case err != nil:
		checks = append(checks, doctorCheck{Name: "credentials", Status: checkSkip, Detail: "configuration is broken"})
	case cfg.KeyID == "" || cfg.Secret == "":
		checks = append(checks, doctorCheck{Name: "credentials", Status: checkFail, auth: true,
			Detail: "no access key: run 'evo configure' or set CLOUDRU_KEY_ID and CLOUDRU_SECRET"})
	default:
		provider := tokenProvider(cfg)
		before := provider.Exchanges()
		tok, terr := provider.Token(ctx)
		if terr != nil {
			checks = append(checks, doctorCheck{Name: "credentials", Status: checkFail, Detail: errDetail(terr),
				auth: clerrors.ExitCode(terr) == clerrors.ExitAuth})
		} else {
			src := "cached token"
			if provider.Exchanges() != before {
				src = "token issued"
			}
			checks = append(checks, doctorCheck{Name: "credentials", Status: checkOK,
				Detail: fmt.Sprintf("key %s: %s, valid for %s", config.MaskKeyID(cfg.KeyID), src, tok.TTL().Truncate(time.Minute))})
			checks = append(checks, keyScopeCheck(ctx, tok.Value))
		}
	}

	for _, bin := range []string{"kubectl", "helm"} {
		if path, err := exec.LookPath(bin); err == nil {
			checks = append(checks, doctorCheck{Name: bin, Status: checkOK, Detail: path})
		} else {
			checks = append(checks, doctorCheck{Name: bin, Status: checkWarn,
				Detail: "not in PATH (needed only by the k8s_kubectl, k8s_apply and k8s_helm tools)"})
		}
	}
	return checks
}

// fileChecks reports the config directory, both files and their modes.
func fileChecks() []doctorCheck {
	var checks []doctorCheck
	dir := config.Dir()
	st, err := os.Stat(dir)
	switch {
	case os.IsNotExist(err):
		return append(checks, doctorCheck{Name: "config dir", Status: checkWarn,
			Detail: dir + " does not exist; run 'evo configure' (or use environment variables)"})
	case err != nil:
		return append(checks, doctorCheck{Name: "config dir", Status: checkFail, Detail: err.Error(), auth: true})
	case config.PermissionsEnforced() && config.LooseMode(st.Mode()):
		checks = append(checks, doctorCheck{Name: "config dir", Status: checkWarn,
			Detail: fmt.Sprintf("%s has mode %04o; run: chmod 700 %s", dir, st.Mode().Perm(), dir)})
	case !config.PermissionsEnforced() && config.PrivateDirWarning(dir) != "":
		checks = append(checks, doctorCheck{Name: "config dir", Status: checkWarn, Detail: config.PrivateDirWarning(dir)})
	default:
		checks = append(checks, doctorCheck{Name: "config dir", Status: checkOK, Detail: dir})
	}

	for _, f := range []struct {
		name, path string
		secret     bool
	}{
		{"config.yaml", config.SettingsPath(), false},
		{"credentials.yaml", config.CredentialsPath(), true},
	} {
		st, err := os.Stat(f.path)
		switch {
		case os.IsNotExist(err):
			checks = append(checks, doctorCheck{Name: f.name, Status: checkWarn, Detail: "not found"})
		case err != nil:
			checks = append(checks, doctorCheck{Name: f.name, Status: checkFail, Detail: err.Error(), auth: true})
		case !config.PermissionsEnforced():
			// Windows: the ACL instead of the mode.
			if w := config.PrivateFileWarning(f.path); w != "" {
				status := checkWarn
				if f.secret {
					status = checkFail
				}
				checks = append(checks, doctorCheck{Name: f.name, Status: status, auth: f.secret, Detail: w})
			} else {
				checks = append(checks, doctorCheck{Name: f.name, Status: checkOK, Detail: f.path + " (ACL: no access for other users)"})
			}
		case config.LooseMode(st.Mode()):
			status := checkWarn
			if f.secret {
				status = checkFail
			}
			checks = append(checks, doctorCheck{Name: f.name, Status: status, auth: f.secret,
				Detail: fmt.Sprintf("mode %04o, readable by others; run: chmod 600 %s", st.Mode().Perm(), f.path)})
		default:
			checks = append(checks, doctorCheck{Name: f.name, Status: checkOK, Detail: fmt.Sprintf("%s (%04o)", f.path, st.Mode().Perm())})
		}
	}

	if home, err := os.UserHomeDir(); err == nil {
		legacy := filepath.Join(home, config.LegacyProjectFileName)
		if _, err := os.Stat(legacy); err == nil {
			checks = append(checks, doctorCheck{Name: "legacy config", Status: checkWarn,
				Detail: legacy + " is no longer read: move its settings to 'evo configure' or ./" + config.ProjectFileName})
		}
	}
	return checks
}

// configChecks shows where the key settings come from.
func configChecks(cfg config.Config, sources []config.Source) []doctorCheck {
	checks := []doctorCheck{{
		Name: "config", Status: checkOK,
		Detail: fmt.Sprintf("profile %s; key_id from %s; project_id from %s",
			cfg.Profile, sourceOrigin(sources, "KeyID"), sourceOrigin(sources, "ProjectID")),
	}}
	if cfg.ProjectID == "" {
		checks = append(checks, doctorCheck{Name: "project_id", Status: checkWarn,
			Detail: "not set: run 'evo configure' or set CLOUDRU_PROJECT_ID"})
	}
	for _, w := range cfg.Warnings {
		checks = append(checks, doctorCheck{Name: "config", Status: checkWarn, Detail: w})
	}
	return checks
}

// probe checks that an API host answers HTTP at all; the status code does
// not matter, only that TLS and the connection work.
func probe(ctx context.Context, name, rawURL string) doctorCheck {
	u, err := url.Parse(rawURL)
	if err != nil {
		return doctorCheck{Name: name, Status: checkFail, Detail: err.Error()}
	}
	base := u.Scheme + "://" + u.Host + "/"
	ctx, cancel := context.WithTimeout(ctx, doctorProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base, nil)
	if err != nil {
		return doctorCheck{Name: name, Status: checkFail, Detail: err.Error()}
	}
	start := time.Now()
	resp, err := httpClient.Do(req)
	if err != nil {
		return doctorCheck{Name: name, Status: checkFail, Detail: fmt.Sprintf("%s unreachable: %v", u.Host, err)}
	}
	resp.Body.Close()
	return doctorCheck{Name: name, Status: checkOK,
		Detail: fmt.Sprintf("%s reachable (HTTP %d, %s)", u.Host, resp.StatusCode, time.Since(start).Round(time.Millisecond))}
}

func errDetail(err error) string {
	if cliErr, ok := err.(*clerrors.CLIError); ok && cliErr.Hint != "" {
		return cliErr.Message + " — " + cliErr.Hint
	}
	return err.Error()
}

func reportDoctor(printer *output.Printer, checks []doctorCheck) error {
	failed, authFailed := 0, false
	for _, c := range checks {
		if c.Status == checkFail {
			failed++
			authFailed = authFailed || c.auth
		}
	}
	if printer.IsJSON() {
		if err := printer.JSON(map[string]any{"checks": checks, "ok": failed == 0}); err != nil {
			return err
		}
	} else {
		rows := make([][]string, len(checks))
		for i, c := range checks {
			rows[i] = []string{c.Status, c.Name, c.Detail}
		}
		printer.Table([]string{"Status", "Check", "Detail"}, rows)
	}
	switch {
	case failed == 0:
		return nil
	case authFailed:
		return clerrors.New(clerrors.CodeConfigInvalid, fmt.Sprintf("doctor: %d check(s) failed", failed),
			"Fix the configuration or credentials above, e.g. with 'evo configure'")
	default:
		return fmt.Errorf("doctor: %d check(s) failed", failed)
	}
}

// keyScopeCheck warns about a key that sees several projects: the project
// a command works in is then a setting, not a limit of the key.
func keyScopeCheck(ctx context.Context, token string) doctorCheck {
	visible, err := listProjects(ctx, token)
	switch {
	case err != nil:
		return doctorCheck{Name: "key scope", Status: checkSkip, Detail: scopeText(nil, err)}
	case len(visible) > 1:
		return doctorCheck{Name: "key scope", Status: checkWarn, Detail: manyProjectsWarning(len(visible))}
	default:
		return doctorCheck{Name: "key scope", Status: checkOK, Detail: scopeText(visible, nil)}
	}
}
