package config

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"

	"gopkg.in/yaml.v3"

	clerrors "github.com/shatim-ops/cloudru-vm-cli/internal/errors"
)

// File layout:
//
//	Dir()/config.yaml          profiles: project_id, region, defaults  (0600)
//	Dir()/credentials.yaml     profiles: key_id, secret, s3            (0600)
//	CacheDir()/token-<p>.json  IAM token cache                          (0600)
const (
	appDir          = "cloudru-vm"
	settingsName    = "config.yaml"
	credentialsName = "credentials.yaml"

	// EnvConfigDir overrides Dir(); the token cache then lives in its
	// cache/ subdirectory, so one variable isolates everything (tests, CI).
	EnvConfigDir = "CLOUDRU_CONFIG_DIR"
)

// Profile holds the non-secret settings of a profile (config.yaml).
type Profile struct {
	ProjectID string `yaml:"project_id,omitempty"`
	Region    string `yaml:"region,omitempty"`
	Flavor    string `yaml:"flavor,omitempty"`
	ImageID   string `yaml:"image_id,omitempty"`
	DiskSize  int    `yaml:"disk_size,omitempty"`
	Timeout   int    `yaml:"timeout,omitempty"`
}

// Credentials holds the secrets of a profile (credentials.yaml).
type Credentials struct {
	KeyID  string    `yaml:"key_id,omitempty"`
	Secret string    `yaml:"secret,omitempty"`
	S3     *S3Config `yaml:"s3,omitempty"`
}

type settingsFile struct {
	Profiles map[string]Profile `yaml:"profiles"`
}

type credentialsFile struct {
	Profiles map[string]Credentials `yaml:"profiles"`
}

// Dir is the directory with config.yaml and credentials.yaml.
func Dir() string {
	if d := os.Getenv(EnvConfigDir); d != "" {
		return d
	}
	base, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, appDir)
}

// CacheDir is the directory with the token cache.
func CacheDir() string {
	if d := os.Getenv(EnvConfigDir); d != "" {
		return filepath.Join(d, "cache")
	}
	base, err := os.UserCacheDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, appDir)
}

// SettingsPath is the path of config.yaml.
func SettingsPath() string { return filepath.Join(Dir(), settingsName) }

// CredentialsPath is the path of credentials.yaml.
func CredentialsPath() string { return filepath.Join(Dir(), credentialsName) }

// TokenCachePath is the IAM token cache file of a profile.
func TokenCachePath(profile string) string {
	return filepath.Join(CacheDir(), "token-"+profile+".json")
}

var profileNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

// ValidateProfileName rejects names that cannot be used as part of a file
// name (the token cache is named after the profile).
func ValidateProfileName(name string) error {
	if !profileNameRe.MatchString(name) {
		return clerrors.New(clerrors.CodeConfigInvalid,
			fmt.Sprintf("invalid profile name %q", name),
			"Use letters, digits, '-', '_' and '.', up to 64 characters")
	}
	return nil
}

// PermissionsEnforced reports whether Unix file modes are meaningful here.
// On Windows os.Chmod only toggles the read-only bit.
func PermissionsEnforced() bool { return runtime.GOOS != "windows" }

// LooseMode reports whether a file mode gives group or others any access.
func LooseMode(m fs.FileMode) bool { return m.Perm()&0o077 != 0 }

func readYAML(path string, v any) (exists bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, clerrors.Wrap(clerrors.CodeConfigInvalid, "reading "+path+": "+err.Error(),
			"Check the file permissions", err)
	}
	if err := yaml.Unmarshal(data, v); err != nil {
		return true, clerrors.Wrap(clerrors.CodeConfigInvalid, fmt.Sprintf("parsing %s: %v", path, err),
			"Fix the YAML syntax or recreate the profile with 'cloudru-vm configure'", err)
	}
	return true, nil
}

func readSettingsFile() (settingsFile, error) {
	var f settingsFile
	_, err := readYAML(SettingsPath(), &f)
	return f, err
}

// readCredentialsFile also warns when the file is readable by others.
func readCredentialsFile() (credentialsFile, []string, error) {
	var f credentialsFile
	path := CredentialsPath()
	exists, err := readYAML(path, &f)
	if err != nil || !exists {
		return f, nil, err
	}
	var warnings []string
	if PermissionsEnforced() {
		if st, err := os.Stat(path); err == nil && LooseMode(st.Mode()) {
			warnings = append(warnings, fmt.Sprintf(
				"%s is accessible by other users (mode %04o); run: chmod 600 %s", path, st.Mode().Perm(), path))
		}
	}
	return f, warnings, nil
}

// LoadProfile returns the stored settings and credentials of a profile.
func LoadProfile(name string) (Profile, Credentials, bool, error) {
	s, err := readSettingsFile()
	if err != nil {
		return Profile{}, Credentials{}, false, err
	}
	c, _, err := readCredentialsFile()
	if err != nil {
		return Profile{}, Credentials{}, false, err
	}
	p, ok1 := s.Profiles[name]
	cr, ok2 := c.Profiles[name]
	return p, cr, ok1 || ok2, nil
}

// SaveProfile writes the profile into both files (0600, directory 0700),
// keeping the other profiles.
func SaveProfile(name string, p Profile, c Credentials) error {
	if err := ValidateProfileName(name); err != nil {
		return err
	}
	s, err := readSettingsFile()
	if err != nil {
		return err
	}
	cr, _, err := readCredentialsFile()
	if err != nil {
		return err
	}
	if s.Profiles == nil {
		s.Profiles = map[string]Profile{}
	}
	if cr.Profiles == nil {
		cr.Profiles = map[string]Credentials{}
	}
	s.Profiles[name] = p
	cr.Profiles[name] = c
	if err := writeYAML(SettingsPath(), s); err != nil {
		return err
	}
	return writeYAML(CredentialsPath(), cr)
}

// DeleteProfile removes a profile from both files and drops its token cache.
func DeleteProfile(name string) error {
	s, err := readSettingsFile()
	if err != nil {
		return err
	}
	cr, _, err := readCredentialsFile()
	if err != nil {
		return err
	}
	_, ok1 := s.Profiles[name]
	_, ok2 := cr.Profiles[name]
	if !ok1 && !ok2 {
		return clerrors.New(clerrors.CodeConfigInvalid,
			fmt.Sprintf("profile %q not found", name),
			"Run 'cloudru-vm configure list' to see existing profiles")
	}
	delete(s.Profiles, name)
	delete(cr.Profiles, name)
	if err := writeYAML(SettingsPath(), s); err != nil {
		return err
	}
	if err := writeYAML(CredentialsPath(), cr); err != nil {
		return err
	}
	if err := os.Remove(TokenCachePath(name)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove token cache: %w", err)
	}
	return nil
}

// ProfileSummary describes a stored profile without its secrets.
type ProfileSummary struct {
	Name      string `json:"name"`
	ProjectID string `json:"project_id"`
	KeyID     string `json:"key_id"` // masked
	HasSecret bool   `json:"has_secret"`
	HasS3     bool   `json:"has_s3"`
}

// Profiles lists the stored profiles sorted by name.
func Profiles() ([]ProfileSummary, error) {
	s, err := readSettingsFile()
	if err != nil {
		return nil, err
	}
	cr, _, err := readCredentialsFile()
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for n := range s.Profiles {
		names[n] = true
	}
	for n := range cr.Profiles {
		names[n] = true
	}
	out := make([]ProfileSummary, 0, len(names))
	for n := range names {
		c := cr.Profiles[n]
		out = append(out, ProfileSummary{
			Name:      n,
			ProjectID: s.Profiles[n].ProjectID,
			KeyID:     MaskKeyID(c.KeyID),
			HasSecret: c.Secret != "",
			HasS3:     c.S3 != nil && c.S3.AccessKey != "",
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// writeYAML replaces path atomically with mode 0600 in a 0700 directory.
func writeYAML(path string, v any) error {
	data, err := yaml.Marshal(v)
	if err != nil {
		return err
	}
	return WriteFilePrivate(path, data)
}

// WriteFilePrivate writes data to path via a temporary file and rename, so
// a crash never leaves a half-written file, and the secret is never
// readable by others even for a moment.
func WriteFilePrivate(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename
	if err := tmp.Chmod(0o600); err != nil && PermissionsEnforced() {
		tmp.Close()
		return fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
