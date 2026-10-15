package config

import (
	stderrors "errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// isolate points the config directory at a temp dir and blanks every
// CLOUDRU_* variable of the developer's shell (empty counts as unset).
// All credentials in these tests are fake.
func isolate(t *testing.T) string {
	t.Helper()
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); strings.HasPrefix(k, "CLOUDRU_") {
			t.Setenv(k, "")
		}
	}
	dir := filepath.Join(t.TempDir(), "evo")
	t.Setenv(EnvConfigDir, dir)
	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func saveFakeProfile(t *testing.T, name string, p Profile) {
	t.Helper()
	if err := SaveProfile(name, p, Credentials{KeyID: "fake-key-id-" + name, Secret: "fake-secret-" + name}); err != nil {
		t.Fatal(err)
	}
}

func exitCode(err error) int { return clerrors.ExitCode(err) }

func TestDefaults(t *testing.T) {
	cfg := Defaults()
	if cfg.Region != "" || cfg.Flavor != "auto" || cfg.Timeout != 600 ||
		cfg.ComposeFile != "docker-compose.yml" || cfg.DiskSize != 10 {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
}

func TestResolve_NothingConfigured(t *testing.T) {
	isolate(t)
	cfg, err := Resolve(Options{})
	if err != nil {
		t.Fatalf("no files and no env must not be an error: %v", err)
	}
	if cfg.Profile != DefaultProfile || cfg.KeyID != "" || cfg.Flavor != "auto" {
		t.Errorf("unexpected config: %+v", cfg)
	}
}

func TestResolve_Precedence(t *testing.T) {
	isolate(t)
	saveFakeProfile(t, "default", Profile{
		ProjectID: "proj-profile", Region: "az-profile", Flavor: "flavor-profile",
		ImageID: "img-profile", DiskSize: 20,
	})
	project := filepath.Join(t.TempDir(), ".evo.yaml")
	writeFile(t, project, "region: az-project\nflavor: flavor-project\nimage_id: img-project\n")
	t.Setenv("CLOUDRU_FLAVOR", "flavor-env")
	t.Setenv("CLOUDRU_IMAGE_ID", "img-env")

	cfg, sources, err := ResolveWithSources(Options{
		ProjectFile: project,
		Flags:       map[string]any{"ImageID": "img-flag"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][2]string{
		// field: value, origin
		"ProjectID": {"proj-profile", OriginProfile},
		"Region":    {"az-project", OriginProject},
		"Flavor":    {"flavor-env", OriginEnv},
		"ImageID":   {"img-flag", OriginFlag},
		"Timeout":   {"600", OriginDefault},
		"DiskSize":  {"20", OriginProfile},
		"KeyID":     {"fake****ault", OriginProfile},
	}
	for _, s := range sources {
		w, ok := want[s.Key]
		if !ok {
			continue
		}
		if got := toString(s.Value); got != w[0] || s.Origin != w[1] {
			t.Errorf("%s = %q from %s, want %q from %s", s.Key, got, s.Origin, w[0], w[1])
		}
	}
	if cfg.KeyID != "fake-key-id-default" || cfg.Secret != "fake-secret-default" {
		t.Errorf("credentials not taken from the profile: %q", cfg.KeyID)
	}
	for _, s := range sources {
		if s.Key == "Secret" && s.Value != "****" {
			t.Errorf("secret not masked in sources: %v", s.Value)
		}
	}
}

func TestResolve_EnvCredentialsOverrideProfileAsAPair(t *testing.T) {
	isolate(t)
	saveFakeProfile(t, "default", Profile{})

	t.Setenv("CLOUDRU_KEY_ID", "env-key")
	cfg, err := Resolve(Options{})
	if err != nil {
		t.Fatal(err)
	}
	// Only the key ID in env: the profile secret must not be mixed in.
	if cfg.KeyID != "env-key" || cfg.Secret != "" {
		t.Errorf("got key %q secret set=%v, want env-key without secret", cfg.KeyID, cfg.Secret != "")
	}

	t.Setenv("CLOUDRU_SECRET", "env-secret")
	cfg, err = Resolve(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.KeyID != "env-key" || cfg.Secret != "env-secret" {
		t.Errorf("env pair not used: %q", cfg.KeyID)
	}
}

func TestResolve_DeprecatedAPIKey(t *testing.T) {
	isolate(t)
	t.Setenv("CLOUDRU_API_KEY", "legacy-id:legacy-secret")
	cfg, err := Resolve(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.KeyID != "legacy-id" || cfg.Secret != "legacy-secret" {
		t.Errorf("legacy key not split: %q", cfg.KeyID)
	}
	if len(cfg.Warnings) != 1 || !strings.Contains(cfg.Warnings[0], "deprecated") ||
		!strings.Contains(cfg.Warnings[0], "CLOUDRU_KEY_ID") {
		t.Errorf("expected a deprecation warning, got %q", cfg.Warnings)
	}

	// The proper variables win and silence the warning.
	t.Setenv("CLOUDRU_KEY_ID", "new-id")
	t.Setenv("CLOUDRU_SECRET", "new-secret")
	cfg, err = Resolve(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.KeyID != "new-id" || len(cfg.Warnings) != 0 {
		t.Errorf("got %q, warnings %q", cfg.KeyID, cfg.Warnings)
	}
}

func TestResolve_MalformedAPIKeyIsAuthError(t *testing.T) {
	isolate(t)
	t.Setenv("CLOUDRU_API_KEY", "no-colon-here")
	_, err := Resolve(Options{})
	if exitCode(err) != clerrors.ExitAuth {
		t.Errorf("exit code %d for %v, want %d", exitCode(err), err, clerrors.ExitAuth)
	}
}

func TestResolve_ProfileSelection(t *testing.T) {
	isolate(t)
	saveFakeProfile(t, "default", Profile{ProjectID: "proj-default"})
	saveFakeProfile(t, "staging", Profile{ProjectID: "proj-staging"})

	cfg, err := Resolve(Options{Profile: "staging"})
	if err != nil || cfg.ProjectID != "proj-staging" || cfg.KeyID != "fake-key-id-staging" {
		t.Fatalf("flag: %+v %v", cfg, err)
	}

	t.Setenv("CLOUDRU_PROFILE", "staging")
	cfg, err = Resolve(Options{})
	if err != nil || cfg.Profile != "staging" || cfg.ProjectID != "proj-staging" {
		t.Fatalf("env: %+v %v", cfg, err)
	}
	// The flag beats the env.
	cfg, err = Resolve(Options{Profile: "default"})
	if err != nil || cfg.ProjectID != "proj-default" {
		t.Fatalf("flag over env: %+v %v", cfg, err)
	}
}

func TestResolve_UnknownExplicitProfile(t *testing.T) {
	isolate(t)
	saveFakeProfile(t, "default", Profile{})
	_, err := Resolve(Options{Profile: "nosuch"})
	if exitCode(err) != clerrors.ExitAuth {
		t.Fatalf("exit code %d for %v, want %d", exitCode(err), err, clerrors.ExitAuth)
	}
	var cliErr *clerrors.CLIError
	if !asCLI(err, &cliErr) || !strings.Contains(cliErr.Hint, "configure --profile nosuch") {
		t.Errorf("hint does not say how to create the profile: %v", err)
	}

	if _, err := Resolve(Options{Profile: "../etc"}); exitCode(err) != clerrors.ExitAuth {
		t.Errorf("path-like profile name accepted: %v", err)
	}
}

func TestResolve_ProjectFileSecretsIgnored(t *testing.T) {
	isolate(t)
	project := filepath.Join(t.TempDir(), ".evo.yaml")
	writeFile(t, project, "project_id: p1\nkey_id: leaked-id\nsecret: leaked-secret\n")
	cfg, err := Resolve(Options{ProjectFile: project})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.KeyID != "" || cfg.Secret != "" || cfg.ProjectID != "p1" {
		t.Errorf("secrets taken from the project file: %+v", cfg)
	}
	if len(cfg.Warnings) != 2 {
		t.Errorf("want warnings for key_id and secret, got %q", cfg.Warnings)
	}
}

func TestResolve_S3SectionAndEnvOverride(t *testing.T) {
	isolate(t)
	err := SaveProfile("default", Profile{}, Credentials{
		KeyID: "k", Secret: "s",
		S3: &S3Config{AccessKey: "s3-file-ak", SecretKey: "s3-file-sk", Region: "ru-central-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Resolve(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.S3.AccessKey != "s3-file-ak" || cfg.S3.SecretKey != "s3-file-sk" {
		t.Errorf("s3 section not read: %+v", cfg.S3)
	}
	t.Setenv("CLOUDRU_S3_ACCESS_KEY", "s3-env-ak")
	cfg, err = Resolve(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.S3.AccessKey != "s3-env-ak" || cfg.S3.Region != "ru-central-1" {
		t.Errorf("env must override the profile: %+v", cfg.S3)
	}
}

func TestResolve_InvalidInputs(t *testing.T) {
	isolate(t)
	project := filepath.Join(t.TempDir(), ".evo.yaml")
	writeFile(t, project, "region: [invalid yaml\n")
	if _, err := Resolve(Options{ProjectFile: project}); exitCode(err) != clerrors.ExitAuth {
		t.Errorf("invalid YAML: %v", err)
	}
	t.Setenv("CLOUDRU_TIMEOUT", "not-a-number")
	if _, err := Resolve(Options{}); err == nil {
		t.Error("non-integer CLOUDRU_TIMEOUT accepted")
	}
	t.Setenv("CLOUDRU_TIMEOUT", "")
	writeFile(t, CredentialsPath(), "profiles: [broken\n")
	if _, err := Resolve(Options{}); exitCode(err) != clerrors.ExitAuth {
		t.Errorf("broken credentials.yaml: %v", err)
	}
}

func TestResolve_MissingProjectFile(t *testing.T) {
	isolate(t)
	cfg, err := Resolve(Options{ProjectFile: filepath.Join(t.TempDir(), "nonexistent.yaml"), Flags: map[string]any{"Verbose": true}})
	if err != nil || !cfg.Verbose || cfg.Flavor != "auto" {
		t.Errorf("got %+v, %v", cfg, err)
	}
}

func TestSaveProfile_FileModes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file modes are not enforced on Windows")
	}
	dir := isolate(t)
	saveFakeProfile(t, "default", Profile{ProjectID: "p"})

	for path, want := range map[string]os.FileMode{
		dir:               0o700,
		SettingsPath():    0o600,
		CredentialsPath(): 0o600,
	} {
		st, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != want {
			t.Errorf("%s: mode %04o, want %04o", path, st.Mode().Perm(), want)
		}
	}
	// Secrets go to credentials.yaml only.
	data, _ := os.ReadFile(SettingsPath())
	if strings.Contains(string(data), "fake-secret") || strings.Contains(string(data), "fake-key-id") {
		t.Errorf("config.yaml contains credentials:\n%s", data)
	}
}

func TestReadCredentials_WarnsOnLooseMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file modes are not enforced on Windows")
	}
	isolate(t)
	saveFakeProfile(t, "default", Profile{})
	if err := os.Chmod(CredentialsPath(), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Resolve(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Warnings) != 1 || !strings.Contains(cfg.Warnings[0], "chmod 600") {
		t.Errorf("expected a permissions warning, got %q", cfg.Warnings)
	}
}

func TestProfiles_ListAndDelete(t *testing.T) {
	isolate(t)
	saveFakeProfile(t, "prod", Profile{ProjectID: "p-prod"})
	saveFakeProfile(t, "default", Profile{ProjectID: "p-default"})
	writeFile(t, TokenCachePath("prod"), `{"key_id":"x"}`)

	list, err := Profiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Name != "default" || list[1].Name != "prod" ||
		list[1].ProjectID != "p-prod" || !list[1].HasSecret || strings.Contains(list[1].KeyID, "key-id") {
		t.Fatalf("unexpected list: %+v", list)
	}

	if err := DeleteProfile("prod"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(TokenCachePath("prod")); !os.IsNotExist(err) {
		t.Errorf("token cache of a deleted profile left behind: %v", err)
	}
	list, _ = Profiles()
	if len(list) != 1 || list[0].Name != "default" {
		t.Errorf("after delete: %+v", list)
	}
	if err := DeleteProfile("prod"); exitCode(err) != clerrors.ExitAuth {
		t.Errorf("deleting a missing profile: %v", err)
	}
}

func TestMaskKeyID(t *testing.T) {
	for in, want := range map[string]string{
		"":                 "",
		"short":            "****",
		"abcdef0123456789": "abcd****6789",
	} {
		if got := MaskKeyID(in); got != want {
			t.Errorf("MaskKeyID(%q) = %q, want %q", in, got, want)
		}
	}
}

func asCLI(err error, target **clerrors.CLIError) bool { return stderrors.As(err, target) }

// .evo.yaml or CLOUDRU_PROJECT_ID replacing the profile's project is
// reported; --project-id is explicit and is not (best practice: no silent
// "active project", cf. gcloud/az which take it silently).
func TestResolve_WarnsWhenProjectOverridesProfile(t *testing.T) {
	isolate(t)
	saveFakeProfile(t, "default", Profile{ProjectID: "proj-profile"})
	project := filepath.Join(t.TempDir(), ".evo.yaml")
	writeFile(t, project, "project_id: proj-dir\n")

	warned := func(cfg Config, want string) bool {
		for _, w := range cfg.Warnings {
			if strings.Contains(w, want) {
				return true
			}
		}
		return false
	}
	cfg, _, err := ResolveWithSources(Options{ProjectFile: project})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ProjectID != "proj-dir" || cfg.ProjectOrigin != OriginProject || !warned(cfg, "proj-profile") {
		t.Errorf(".evo.yaml: project %q from %q, warnings %q", cfg.ProjectID, cfg.ProjectOrigin, cfg.Warnings)
	}

	t.Setenv("CLOUDRU_PROJECT_ID", "proj-env")
	cfg, _, _ = ResolveWithSources(Options{})
	if cfg.ProjectOrigin != OriginEnv || !warned(cfg, "CLOUDRU_PROJECT_ID") {
		t.Errorf("env: origin %q, warnings %q", cfg.ProjectOrigin, cfg.Warnings)
	}

	cfg, _, _ = ResolveWithSources(Options{Flags: map[string]any{"ProjectID": "proj-flag"}})
	if cfg.ProjectOrigin != OriginFlag || warned(cfg, "proj-profile") {
		t.Errorf("flag: origin %q, warnings %q", cfg.ProjectOrigin, cfg.Warnings)
	}

	t.Setenv("CLOUDRU_PROJECT_ID", "proj-profile") // same project: nothing to say
	cfg, _, _ = ResolveWithSources(Options{})
	if warned(cfg, "proj-profile") {
		t.Errorf("same project warned: %q", cfg.Warnings)
	}
}
