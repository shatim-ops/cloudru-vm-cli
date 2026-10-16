// Package images holds the rules for marketplace and OS images that every
// VM-creating path shares. It prints nothing and knows nothing of cobra or
// MCP: callers turn its results into previews, errors and requests.
package images

import (
	"fmt"
	"sort"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// Field types of image_metadata as the catalogue declares them
// (compute openapi-v3, checked live 16.10.2026).
const (
	TypeLinuxLogin    = "linux_login"
	TypePublicKey     = "public_key"
	TypeLinuxPassword = "linux_password"
	TypePlainPassword = "plain_password"
	TypeHostname      = "hostname"
	TypeText          = "text"
	TypeFlag          = "flag"
	TypeInfo          = "info"
)

// statusFields are read-only VM status flags some images declare: sending
// them makes the create API answer 500, whatever their type or default.
var statusFields = map[string]bool{"vmm_enabled": true, "guest_agent_ready": true}

// IsStatusField reports whether a metadata field must never be sent.
func IsStatusField(name string) bool { return statusFields[name] }

// Inputs are what the caller knows before the VM exists.
type Inputs struct {
	// PublicKey goes into every public-key field; empty leaves them unset
	// (an empty public_key makes the create API answer 500).
	PublicKey string
	// Hostname goes into every hostname field (fqdn of RED OS 7.3 included).
	Hostname string
	// Login is the fallback for login fields that have no default (when no
	// other login field of the image declares one).
	Login string
	// GenPassword makes the random access password. Nil defers it: the
	// fields are listed in Result.PasswordFields and left out of meta, for a
	// caller that must not keep the value (a saved deploy plan).
	GenPassword func() string
	// Values are explicit values by field name (a future --meta). They win
	// over everything evo fills itself and may set any field except the
	// status flags.
	Values map[string]string
}

// FieldRef names a field for a message or a preview.
type FieldRef struct {
	Name        string `json:"name"`
	FieldType   string `json:"field_type,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
}

// MissingGroup is a mandatory group no field of which got a value.
type MissingGroup struct {
	Group       string     `json:"group"`
	DisplayName string     `json:"display_name,omitempty"`
	Fields      []FieldRef `json:"fields"`
}

// Result says what FillMetadata did, without any secret value.
type Result struct {
	// Login is the value of the login field that was filled ("" when the
	// image declares none).
	Login string
	// PasswordFields are the access-password fields evo fills with a random
	// value: in meta when Inputs.GenPassword is set, otherwise left to the
	// caller.
	PasswordFields []string
	// KeyFields are the public-key fields evo fills with Inputs.PublicKey;
	// KeyUsed says whether the key was actually put in meta.
	KeyFields []string
	KeyUsed   bool
	// Missing are the mandatory groups still unsatisfied, in field order.
	Missing []MissingGroup
	// Sensitive are the fields sent (or to be sent) whose value is a secret.
	Sensitive []string
}

type kind int

const (
	kindOther kind = iota
	kindStatus
	kindKey
	kindHostname
	kindLogin
	kindPassword // the access password of the VM
)

// typedKinds are the field types that decide a field's role by themselves.
// Any other type (empty, text, an unknown one) falls back to the field name,
// as older images carry no field_type.
var typedKinds = map[string]kind{
	TypeLinuxLogin:    kindLogin,
	TypePublicKey:     kindKey,
	TypeLinuxPassword: kindPassword,
	TypeHostname:      kindHostname,
}

var nameKinds = map[string]kind{
	"ssh_key": kindKey, "public_key": kindKey, "ssh_public_key": kindKey,
	"hostname": kindHostname, "instance_name": kindHostname,
	"linux_password": kindPassword,
	"name":           kindLogin, "user": kindLogin, "username": kindLogin, "default_user": kindLogin,
}

// loginPreference is the order the SSH user is read in when an image has
// several login fields (Jaeger, Zabbix_Proxy: "name" and "linux_login").
var loginPreference = []string{"name", "user", "username", "default_user"}

// NormGroup is the mandatory group as it is compared: the catalogue has
// " authentication" with a leading space and mixed case.
func NormGroup(g string) string { return strings.ToLower(strings.TrimSpace(g)) }

func classify(fields []provision.ImageMetadataField) []kind {
	kinds := make([]kind, len(fields))
	hasAccess := false
	for i, f := range fields {
		switch {
		case statusFields[f.Name]:
			kinds[i] = kindStatus
			continue
		case f.FieldType == TypePlainPassword, f.FieldType == TypeFlag, f.FieldType == TypeInfo:
			// plain_password is decided below; flags and info are only
			// ever sent with their default or an explicit value.
		default:
			if k, ok := typedKinds[f.FieldType]; ok {
				kinds[i] = k
			} else {
				kinds[i] = nameKinds[f.Name]
			}
		}
		if kinds[i] == kindKey || kinds[i] == kindPassword {
			hasAccess = true
		}
	}
	// plain_password is the VM's access password only when it is named so,
	// or when the image has no other way in (Windows: login + one password).
	// Otherwise it is a product secret (OpensearchDashboards) that evo must
	// not invent: the user would never learn it.
	for i, f := range fields {
		if kinds[i] != kindOther || f.FieldType != TypePlainPassword {
			continue
		}
		if f.Name == "linux_password" || !hasAccess {
			kinds[i] = kindPassword
			hasAccess = true
		}
	}
	return kinds
}

func preferredLoginDefault(fields []provision.ImageMetadataField, kinds []kind) string {
	for _, n := range loginPreference {
		for i, f := range fields {
			if kinds[i] == kindLogin && f.Name == n && f.DefaultValue != "" {
				return f.DefaultValue
			}
		}
	}
	for i, f := range fields {
		if kinds[i] == kindLogin && f.DefaultValue != "" {
			return f.DefaultValue
		}
	}
	return ""
}

func hasDefault(f provision.ImageMetadataField) bool {
	return strings.TrimSpace(f.DefaultValue) != ""
}

// FillMetadata builds the image_metadata of a create-VM request from the
// image's declared fields. Roles come from field_type; the field name is the
// fallback for images without one. An explicit value wins; then evo's own
// (key, hostname, login, random password); then a real default. Status
// flags, and flags or info without a default, are never sent. Password
// values appear only in meta.
func FillMetadata(fields []provision.ImageMetadataField, in Inputs) (map[string]string, Result, error) {
	var res Result
	var password string // generated once, shared by every password field
	declared := make(map[string]bool, len(fields))
	for _, f := range fields {
		declared[f.Name] = true
	}
	userKeys := make([]string, 0, len(in.Values))
	for k := range in.Values {
		userKeys = append(userKeys, k)
	}
	sort.Strings(userKeys)
	for _, k := range userKeys {
		switch {
		case statusFields[k]:
			return nil, res, fmt.Errorf("image metadata field %q is a read-only status flag and cannot be set", k)
		case !declared[k]:
			return nil, res, fmt.Errorf("the image declares no metadata field %q", k)
		case strings.TrimSpace(in.Values[k]) == "":
			return nil, res, fmt.Errorf("image metadata field %q: empty value", k)
		}
	}

	kinds := classify(fields)
	// A login field without a default takes the image's own login when
	// another login field declares one (two login fields must not name two
	// users), else the caller's fallback.
	login := in.Login
	if d := preferredLoginDefault(fields, kinds); d != "" {
		login = d
	}
	meta := map[string]string{}
	pending := map[string]bool{} // password fields left to the caller
	loginFrom := map[string]bool{}
	for i, f := range fields {
		if kinds[i] == kindStatus {
			continue
		}
		if v, ok := in.Values[f.Name]; ok {
			meta[f.Name] = v
			if kinds[i] == kindLogin {
				loginFrom[f.Name] = true
			}
			continue
		}
		switch kinds[i] {
		case kindKey:
			res.KeyFields = append(res.KeyFields, f.Name)
			if in.PublicKey != "" {
				meta[f.Name] = in.PublicKey
				res.KeyUsed = true
			}
		case kindHostname:
			if in.Hostname != "" {
				meta[f.Name] = in.Hostname
			}
		case kindLogin:
			v := f.DefaultValue
			if v == "" {
				v = login
			}
			if v != "" {
				meta[f.Name] = v
				loginFrom[f.Name] = true
			}
		case kindPassword:
			// Set even when the key already satisfies the group: without it
			// the VM never leaves "creating" (seen live 2026-10-14).
			// One password for every password field: the person gets one
			// password back, and it must open the VM whichever field the
			// image reads.
			res.PasswordFields = append(res.PasswordFields, f.Name)
			if in.GenPassword != nil {
				if password == "" {
					password = in.GenPassword()
				}
				meta[f.Name] = password
			} else {
				pending[f.Name] = true
			}
		}
	}

	filled := func(name string) bool { _, ok := meta[name]; return ok || pending[name] }
	satisfied := map[string]bool{}
	for _, f := range fields {
		if g := NormGroup(f.MandatoryGroup); g != "" && filled(f.Name) {
			satisfied[g] = true
		}
	}
	// A default only where nothing else filled the field and its group is
	// not already covered (no stray value next to the key and password).
	for i, f := range fields {
		if kinds[i] == kindStatus || filled(f.Name) || !hasDefault(f) {
			continue
		}
		if g := NormGroup(f.MandatoryGroup); g != "" && satisfied[g] {
			continue
		}
		meta[f.Name] = f.DefaultValue
	}

	for _, n := range loginPreference {
		if loginFrom[n] {
			res.Login = meta[n]
			break
		}
	}
	if res.Login == "" {
		for _, f := range fields {
			if loginFrom[f.Name] {
				res.Login = meta[f.Name]
				break
			}
		}
	}

	groupIdx := map[string]int{}
	for i, f := range fields {
		if filled(f.Name) && (f.Sensitive || f.FieldType == TypeLinuxPassword || f.FieldType == TypePlainPassword || kinds[i] == kindPassword) {
			res.Sensitive = append(res.Sensitive, f.Name)
		}
		g := NormGroup(f.MandatoryGroup)
		if g == "" || kinds[i] == kindStatus {
			continue
		}
		if filled(f.Name) {
			satisfied[g] = true
		}
		idx, ok := groupIdx[g]
		if !ok {
			idx = len(res.Missing)
			groupIdx[g] = idx
			res.Missing = append(res.Missing, MissingGroup{
				Group:       strings.TrimSpace(f.MandatoryGroup),
				DisplayName: strings.TrimSpace(f.MandatoryGroupDisplayName),
			})
		}
		m := &res.Missing[idx]
		if m.DisplayName == "" {
			m.DisplayName = strings.TrimSpace(f.MandatoryGroupDisplayName)
		}
		m.Fields = append(m.Fields, FieldRef{Name: f.Name, FieldType: f.FieldType, DisplayName: f.DisplayName})
	}
	missing := res.Missing[:0]
	for _, m := range res.Missing {
		if !satisfied[NormGroup(m.Group)] {
			missing = append(missing, m)
		}
	}
	res.Missing = missing
	if len(res.Missing) == 0 {
		res.Missing = nil
	}
	return meta, res, nil
}
