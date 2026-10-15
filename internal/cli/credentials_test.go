package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/auth"
	"github.com/shatim-ops/evo-cli/internal/config"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// All credentials below are fake; the Cloud.ru endpoints are httptest servers.
const (
	fakeKeyID  = "fake0000-key-id-0000-000000000001"
	fakeSecret = "fake-secret-value"
)

type fakeCloud struct {
	iamCalls   atomic.Int32
	orgCalls   atomic.Int32
	iamStatus  int    // 0 = 200
	orgStatus  int    // 0 = 200
	orgProject string // JSON array of projects
}

// setupCLI isolates a CLI run: temp config dir, cwd and HOME, no CLOUDRU_*
// variables from the developer's shell, and fake IAM / Organization /
// Compute endpoints.
func setupCLI(t *testing.T) *fakeCloud {
	t.Helper()
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); strings.HasPrefix(k, "CLOUDRU_") {
			t.Setenv(k, "")
		}
	}
	root := t.TempDir()
	t.Setenv(config.EnvConfigDir, filepath.Join(root, "config", "evo"))
	t.Setenv("HOME", root)
	work := filepath.Join(root, "work")
	if err := os.Mkdir(work, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(work)

	fc := &fakeCloud{orgProject: `[{"id":"11111111-1111-1111-1111-111111111111","name":"only","status":"active"}]`}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/iam/token":
			fc.iamCalls.Add(1)
			var body struct{ KeyID, Secret string }
			_ = json.NewDecoder(r.Body).Decode(&body)
			if fc.iamStatus != 0 {
				http.Error(w, "denied", fc.iamStatus)
				return
			}
			fmt.Fprintf(w, `{"access_token":"tok-%s","expires_in":3600}`, body.KeyID)
		case "/org/v3/projects":
			fc.orgCalls.Add(1)
			if r.Header.Get("Authorization") == "" {
				http.Error(w, "no token", http.StatusUnauthorized)
				return
			}
			if fc.orgStatus != 0 {
				http.Error(w, "forbidden", fc.orgStatus)
				return
			}
			fmt.Fprintf(w, `{"projects":%s}`, fc.orgProject)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	oldIAM, oldOrg, oldCompute := auth.IAMEndpoint, orgProjectsURL, computeBaseURL
	auth.IAMEndpoint, orgProjectsURL, computeBaseURL = srv.URL+"/iam/token", srv.URL+"/org/v3/projects", srv.URL
	oldTTY, oldPass := stdinIsTerminal, readPassword
	stdinIsTerminal = func() bool { return false }
	t.Cleanup(func() {
		auth.IAMEndpoint, orgProjectsURL, computeBaseURL = oldIAM, oldOrg, oldCompute
		stdinIsTerminal, readPassword = oldTTY, oldPass
	})
	newProcess()
	return fc
}

// newProcess forgets the in-memory state a fresh CLI process would not have.
func newProcess() {
	providersMu.Lock()
	providers = map[string]*auth.IAMProvider{}
	providersMu.Unlock()
	warnedMu.Lock()
	warned = map[string]bool{}
	warnedMu.Unlock()
}

// runCLI runs the CLI with stdin and returns the exit code, stdout, stderr.
func runCLI(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	dir := t.TempDir()
	open := func(name, content string) *os.File {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		f, err := os.OpenFile(path, os.O_RDWR, 0)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	in, out, errOut := open("stdin", stdin), open("stdout", ""), open("stderr", "")
	oldIn, oldOut, oldErr := os.Stdin, os.Stdout, os.Stderr
	os.Stdin, os.Stdout, os.Stderr = in, out, errOut
	code := run(args)
	os.Stdin, os.Stdout, os.Stderr = oldIn, oldOut, oldErr
	for _, f := range []*os.File{in, out, errOut} {
		f.Close()
	}
	o, _ := os.ReadFile(filepath.Join(dir, "stdout"))
	e, _ := os.ReadFile(filepath.Join(dir, "stderr"))
	return code, string(o), string(e)
}

func saveProfile(t *testing.T, name, projectID string) {
	t.Helper()
	if err := config.SaveProfile(name, config.Profile{ProjectID: projectID},
		config.Credentials{KeyID: fakeKeyID, Secret: fakeSecret}); err != nil {
		t.Fatal(err)
	}
}

func TestConfigure_NonInteractivePicksTheOnlyProject(t *testing.T) {
	fc := setupCLI(t)
	code, stdout, stderr := runCLI(t, fakeSecret+"\n", "configure", "--key-id", fakeKeyID, "--secret-stdin")
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, stdout, stderr)
	}
	_, creds, ok, err := config.LoadProfile("default")
	if err != nil || !ok || creds.KeyID != fakeKeyID || creds.Secret != fakeSecret {
		t.Fatalf("credentials not stored: %+v %v %v", creds, ok, err)
	}
	prof, _, _, _ := config.LoadProfile("default")
	if prof.ProjectID != "11111111-1111-1111-1111-111111111111" {
		t.Errorf("project not picked: %q", prof.ProjectID)
	}
	if fc.iamCalls.Load() != 1 || fc.orgCalls.Load() != 1 {
		t.Errorf("iam %d, org %d calls", fc.iamCalls.Load(), fc.orgCalls.Load())
	}
	if strings.Contains(stdout+stderr, fakeSecret) || strings.Contains(stdout+stderr, fakeKeyID) {
		t.Errorf("credentials printed:\n%s%s", stdout, stderr)
	}
	if runtime.GOOS != "windows" {
		for _, p := range []string{config.SettingsPath(), config.CredentialsPath()} {
			st, err := os.Stat(p)
			if err != nil || st.Mode().Perm() != 0o600 {
				t.Errorf("%s: mode %v, err %v", p, st.Mode().Perm(), err)
			}
		}
		if st, _ := os.Stat(config.Dir()); st.Mode().Perm() != 0o700 {
			t.Errorf("config dir mode %04o", st.Mode().Perm())
		}
	}
}

func TestConfigure_SeveralProjectsNeedProjectIDWithoutTTY(t *testing.T) {
	fc := setupCLI(t)
	fc.orgProject = `[{"id":"p-1","name":"one"},{"id":"p-2","name":"two"}]`
	code, _, stderr := runCLI(t, fakeSecret+"\n", "configure", "--key-id", fakeKeyID, "--secret-stdin")
	if code != clerrors.ExitAuth || !strings.Contains(stderr, "--project-id") || !strings.Contains(stderr, "p-2") {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if _, err := os.Stat(config.CredentialsPath()); !os.IsNotExist(err) {
		t.Error("credentials written although configure failed")
	}

	before := fc.orgCalls.Load()
	code, _, stderr = runCLI(t, fakeSecret+"\n", "configure", "--key-id", fakeKeyID, "--secret-stdin", "--project-id", "p-2")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if fc.orgCalls.Load() != before {
		t.Error("--project-id must skip the project list")
	}
}

func TestConfigure_Interactive(t *testing.T) {
	fc := setupCLI(t)
	fc.orgProject = `[{"id":"p-1","name":"one"},{"id":"p-2","name":"two"}]`
	stdinIsTerminal = func() bool { return true }
	readPassword = func() ([]byte, error) { return []byte(fakeSecret), nil }

	// Key ID, then a wrong choice, then project number 2.
	code, stdout, stderr := runCLI(t, fakeKeyID+"\n9\n2\n", "--profile", "work", "configure")
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, stdout, stderr)
	}
	prof, creds, _, _ := config.LoadProfile("work")
	if prof.ProjectID != "p-2" || creds.Secret != fakeSecret {
		t.Errorf("got project %q", prof.ProjectID)
	}
	if !strings.Contains(stderr, "Not in the list") || !strings.Contains(stderr, "Secret:") {
		t.Errorf("prompts missing: %q", stderr)
	}

	// Re-running keeps the stored key and secret on empty answers and
	// offers the current project as the default.
	readPassword = func() ([]byte, error) { return nil, nil }
	code, _, stderr = runCLI(t, "\n\n", "--profile", "work", "configure")
	if code != 0 {
		t.Fatalf("rerun exit %d: %s", code, stderr)
	}
	prof, creds, _, _ = config.LoadProfile("work")
	if prof.ProjectID != "p-2" || creds.KeyID != fakeKeyID || creds.Secret != fakeSecret {
		t.Errorf("rerun changed the profile: %q %q", prof.ProjectID, creds.KeyID)
	}
}

func TestConfigure_OrgForbiddenFallsBackToManualEntry(t *testing.T) {
	fc := setupCLI(t)
	fc.orgStatus = http.StatusForbidden
	stdinIsTerminal = func() bool { return true }
	readPassword = func() ([]byte, error) { return []byte(fakeSecret), nil }

	code, _, stderr := runCLI(t, fakeKeyID+"\nmanual-project\n", "configure")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if prof, _, _, _ := config.LoadProfile("default"); prof.ProjectID != "manual-project" {
		t.Errorf("got %q", prof.ProjectID)
	}
}

func TestConfigure_ServiceAccountCannotListProjects(t *testing.T) {
	// Live behaviour: the Organization API answers 400 for a service account.
	fc := setupCLI(t)
	fc.orgStatus = http.StatusBadRequest
	code, _, stderr := runCLI(t, fakeSecret+"\n", "configure", "--key-id", fakeKeyID, "--secret-stdin")
	if code == 0 || !strings.Contains(stderr, "--project-id") {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	// Rotating the key of an existing profile keeps its project.
	saveProfile(t, "default", "p-kept")
	code, _, stderr = runCLI(t, "new-secret\n", "configure", "--key-id", "newkey00-0000", "--secret-stdin")
	if code != 0 || !strings.Contains(stderr, "keeping project p-kept") {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	prof, creds, _, _ := config.LoadProfile("default")
	if prof.ProjectID != "p-kept" || creds.KeyID != "newkey00-0000" || creds.Secret != "new-secret" {
		t.Errorf("got %q %q", prof.ProjectID, creds.KeyID)
	}
}

func TestConfigure_RejectedKeyWritesNothing(t *testing.T) {
	fc := setupCLI(t)
	fc.iamStatus = http.StatusUnauthorized
	code, _, stderr := runCLI(t, fakeSecret+"\n", "configure", "--key-id", fakeKeyID, "--secret-stdin", "--project-id", "p")
	if code != clerrors.ExitAuth {
		t.Fatalf("exit %d, want %d: %s", code, clerrors.ExitAuth, stderr)
	}
	if _, err := os.Stat(config.CredentialsPath()); !os.IsNotExist(err) {
		t.Error("rejected credentials were stored")
	}
}

func TestConfigure_MissingSecretWithoutTTY(t *testing.T) {
	setupCLI(t)
	code, _, stderr := runCLI(t, "", "configure", "--key-id", fakeKeyID)
	if code != clerrors.ExitAuth || !strings.Contains(stderr, "--secret-stdin") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
}

func TestConfigure_ListAndDelete(t *testing.T) {
	setupCLI(t)
	saveProfile(t, "default", "p-default")
	saveProfile(t, "prod", "p-prod")

	code, stdout, _ := runCLI(t, "", "--profile", "prod", "configure", "list")
	if code != 0 || !strings.Contains(stdout, "prod *") || !strings.Contains(stdout, "p-default") ||
		strings.Contains(stdout, fakeKeyID) {
		t.Errorf("exit %d, list:\n%s", code, stdout)
	}
	if code, _, _ := runCLI(t, "", "configure", "delete", "prod"); code != 0 {
		t.Errorf("delete exit %d", code)
	}
	code, stdout, _ = runCLI(t, "", "--json", "configure", "list")
	if code != 0 || strings.Contains(stdout, `"prod"`) || !strings.Contains(stdout, `"default"`) {
		t.Errorf("after delete: %s", stdout)
	}
	if code, _, _ := runCLI(t, "", "configure", "delete", "prod"); code != clerrors.ExitAuth {
		t.Errorf("deleting a missing profile: exit %d", code)
	}
}

func TestWhoami_ProfileWithoutEnvAndCachedToken(t *testing.T) {
	fc := setupCLI(t)
	saveProfile(t, "default", "p-1")

	code, stdout, stderr := runCLI(t, "", "whoami")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, want := range []string{"Profile:     default", config.MaskKeyID(fakeKeyID), "(from profile)", "p-1", "issued by IAM now"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output lacks %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, fakeKeyID) {
		t.Errorf("key ID not masked:\n%s", stdout)
	}

	// A second process reuses the token from the disk cache.
	newProcess()
	code, stdout, _ = runCLI(t, "", "--json", "whoami")
	if code != 0 {
		t.Fatalf("second whoami exit %d", code)
	}
	if fc.iamCalls.Load() != 1 {
		t.Errorf("second whoami went to IAM: %d calls", fc.iamCalls.Load())
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("not JSON: %q", stdout)
	}
	if got["token_source"] != "cache" || got["project_id"] != "p-1" {
		t.Errorf("unexpected JSON: %v", got)
	}
}

func TestWhoami_UnknownProfileExitsTwoWithHint(t *testing.T) {
	setupCLI(t)
	saveProfile(t, "default", "p-1")
	code, _, stderr := runCLI(t, "", "whoami", "--profile", "nosuch")
	if code != clerrors.ExitAuth || !strings.Contains(stderr, "configure --profile nosuch") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
}

func TestWhoami_FailuresExitTwo(t *testing.T) {
	fc := setupCLI(t)
	if code, _, stderr := runCLI(t, "", "whoami"); code != clerrors.ExitAuth || !strings.Contains(stderr, "configure") {
		t.Errorf("no credentials: exit %d, stderr %q", code, stderr)
	}
	saveProfile(t, "default", "p-1")
	fc.iamStatus = http.StatusForbidden
	if code, _, _ := runCLI(t, "", "whoami"); code != clerrors.ExitAuth {
		t.Errorf("rejected key: exit %d", code)
	}
}

func TestWhoami_EnvOverridesProfile(t *testing.T) {
	setupCLI(t)
	saveProfile(t, "default", "p-1")
	t.Setenv("CLOUDRU_KEY_ID", "envkey00-0000-0000-0000-000000000002")
	t.Setenv("CLOUDRU_SECRET", "env-secret")
	t.Setenv("CLOUDRU_PROJECT_ID", "p-env")
	code, stdout, _ := runCLI(t, "", "whoami")
	if code != 0 || !strings.Contains(stdout, "(from env)") || !strings.Contains(stdout, "p-env") ||
		!strings.Contains(stdout, config.MaskKeyID("envkey00-0000-0000-0000-000000000002")) {
		t.Errorf("exit %d:\n%s", code, stdout)
	}
}

func TestDeprecatedAPIKeyWarnsOnStderr(t *testing.T) {
	setupCLI(t)
	t.Setenv("CLOUDRU_API_KEY", fakeKeyID+":"+fakeSecret)
	code, stdout, stderr := runCLI(t, "", "--json", "whoami")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	// With --json the warning is a JSON line: stderr stays parseable.
	var w struct {
		Warning string `json:"warning"`
	}
	if err := json.Unmarshal([]byte(strings.SplitN(stderr, "\n", 2)[0]), &w); err != nil ||
		!strings.Contains(w.Warning, "CLOUDRU_API_KEY") || !strings.Contains(w.Warning, "deprecated") {
		t.Errorf("no deprecation warning on stderr: %q", stderr)
	}
	if strings.Contains(stdout, "warning") {
		t.Errorf("warning leaked into stdout: %q", stdout)
	}

	newProcess()
	if _, _, stderr := runCLI(t, "", "whoami"); !strings.Contains(stderr, "Warning: CLOUDRU_API_KEY") {
		t.Errorf("no text warning without --json: %q", stderr)
	}
}

func TestDoctor(t *testing.T) {
	setupCLI(t)
	code, stdout, _ := runCLI(t, "", "--json", "doctor")
	if code != clerrors.ExitAuth {
		t.Errorf("no credentials: exit %d, want %d\n%s", code, clerrors.ExitAuth, stdout)
	}

	saveProfile(t, "default", "p-1")
	newProcess()
	code, stdout, stderr := runCLI(t, "", "--json", "doctor")
	if code != 0 {
		t.Fatalf("exit %d: %s\n%s", code, stdout, stderr)
	}
	var got struct {
		OK     bool          `json:"ok"`
		Checks []doctorCheck `json:"checks"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("not JSON: %q", stdout)
	}
	status := map[string]string{}
	for _, c := range got.Checks {
		status[c.Name] = c.Status
	}
	for _, name := range []string{"version", "config", "credentials", "IAM API", "Compute API", "credentials.yaml"} {
		if status[name] != checkOK {
			t.Errorf("check %q = %q, want ok (all: %v)", name, status[name], got.Checks)
		}
	}
	if s := status["kubectl"]; s != checkOK && s != checkWarn {
		t.Errorf("kubectl check = %q", s)
	}

	if runtime.GOOS != "windows" {
		if err := os.Chmod(config.CredentialsPath(), 0o644); err != nil {
			t.Fatal(err)
		}
		newProcess()
		if code, _, _ := runCLI(t, "", "doctor"); code != clerrors.ExitAuth {
			t.Errorf("world-readable credentials.yaml: exit %d, want %d", code, clerrors.ExitAuth)
		}
	}
}

func TestDoctor_UnreachableAPIIsExitOne(t *testing.T) {
	setupCLI(t)
	saveProfile(t, "default", "p-1")
	computeBaseURL = "http://127.0.0.1:1" // nothing listens on port 1
	code, stdout, _ := runCLI(t, "", "doctor")
	if code != clerrors.ExitGeneral || !strings.Contains(stdout, "unreachable") {
		t.Errorf("exit %d:\n%s", code, stdout)
	}
}

func TestBuildEngine_SharesTokenProvider(t *testing.T) {
	setupCLI(t)
	saveProfile(t, "default", "p-1")
	e1, err := buildEngine(map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	e2, err := buildEngine(map[string]any{"Region": "other"})
	if err != nil {
		t.Fatal(err)
	}
	if e1.Auth != e2.Auth {
		t.Error("each Engine got its own token provider: MCP would exchange the key on every call")
	}
	if e1.Config.KeyID != fakeKeyID {
		t.Errorf("engine credentials not from the profile: %q", e1.Config.KeyID)
	}
}
