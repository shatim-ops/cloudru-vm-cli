package mk8s

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Addons (plugins) of Managed Kubernetes: the shared catalogue and the
// releases of one addon, read-only.
//
// The catalogue shapes (GET /v3/addons, GET /v3/addons/{name}) were read on
// the live API (api-probe #5, 16.10.2026, docs/internal/standalone/BACKLOG.md
// [k8s-addons]). Installing (POST /v3/clusterAddons) is not in v1.0: see
// [k8s-addons] for why and where the code is.

// addonPageSize is the page size of the catalogue calls; maxAddonPages stops
// a server that keeps returning a next page token.
const (
	addonPageSize = 100
	maxAddonPages = 50
)

// Addon is one addon of the shared catalogue.
type Addon struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName,omitempty"`
	Description string `json:"description,omitempty"`
	// Categories are Russian words of the console ("Сети", "Мониторинг").
	Categories []string `json:"categories,omitempty"`
	// IsBillable is the catalogue's flag; the server-side filter on it does
	// not match it (api-probe #5), so callers filter themselves.
	IsBillable bool   `json:"isBillable"`
	CreatedAt  string `json:"createdAt,omitempty"`
	UpdatedAt  string `json:"updatedAt,omitempty"`
}

// AddonRelease is one installable release of an addon.
type AddonRelease struct {
	AddonVersion   string `json:"addonVersion"`
	AppVersion     string `json:"appVersion"`
	AddonNamespace string `json:"addonNamespace,omitempty"`
	// ConfigurationSchema is a JSON Schema as a string (up to ~94 KB).
	ConfigurationSchema string            `json:"configurationSchema,omitempty"`
	Mode                string            `json:"mode,omitempty"`
	Requirements        AddonRequirements `json:"requirements"`
	CreatedAt           string            `json:"createdAt,omitempty"`
	UpdatedAt           string            `json:"updatedAt,omitempty"`
}

// AddonRequirements is what a release needs of the cluster.
type AddonRequirements struct {
	// KubeVersions is a semver range, e.g. ">=1.26.0-0".
	KubeVersions string  `json:"kubeVersions,omitempty"`
	ReadyWorkers flexInt `json:"readyWorkers,omitempty"`
}

// flexInt reads a JSON number or a quoted one: protojson writes int64 as a
// string, and the spec does not say which width readyWorkers has.
type flexInt int

func (n *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*n = 0
		return nil
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("readyWorkers: %q is not an integer", s)
	}
	*n = flexInt(v)
	return nil
}

// projectQuery is the query of the catalogue calls: they answer 400 without
// projectId (the camelCase name, as the probe sent it).
func (c *Client) projectQuery() url.Values {
	q := url.Values{}
	if c.projectID != "" {
		q.Set("projectId", c.projectID)
	}
	return q
}

// ListAddons returns the whole addon catalogue, following nextPageToken.
func (c *Client) ListAddons(ctx context.Context) ([]Addon, error) {
	var out []Addon
	err := c.pages(ctx, "/v3/addons", func(data []byte) (string, error) {
		var r struct {
			Addons        []Addon `json:"addons"`
			NextPageToken string  `json:"nextPageToken"`
		}
		if err := json.Unmarshal(data, &r); err != nil {
			return "", err
		}
		out = append(out, r.Addons...)
		return r.NextPageToken, nil
	})
	return out, err
}

// GetAddonReleases returns the releases of one addon, in the server's order
// (which is not defined: sort with SortReleases).
func (c *Client) GetAddonReleases(ctx context.Context, name string) ([]AddonRelease, error) {
	var out []AddonRelease
	err := c.pages(ctx, "/v3/addons/"+seg(name), func(data []byte) (string, error) {
		var r struct {
			Releases      []AddonRelease `json:"releases"`
			NextPageToken string         `json:"nextPageToken"`
		}
		if err := json.Unmarshal(data, &r); err != nil {
			return "", err
		}
		for _, rel := range r.Releases {
			// The live API (16.10.2026) puts the addon version into
			// addonNamespace too ("1.21.1-sbc.0" for cert-manager): a value
			// that is not a namespace name (DNS-1123 label) is left out —
			// an agent would run "get pods -n 1.21.1-sbc.0" literally.
			if !namespaceRE.MatchString(rel.AddonNamespace) {
				rel.AddonNamespace = ""
			}
			out = append(out, rel)
		}
		return r.NextPageToken, nil
	})
	return out, err
}

// pages GETs path page by page; page decodes one body and returns the next
// page token ("" ends). A token seen twice ends it too.
func (c *Client) pages(ctx context.Context, path string, page func([]byte) (string, error)) error {
	seen := map[string]bool{}
	token := ""
	for i := 0; i < maxAddonPages; i++ {
		q := c.projectQuery()
		q.Set("pageSize", strconv.Itoa(addonPageSize))
		if token != "" {
			q.Set("pageToken", token)
		}
		data, st, err := c.do(ctx, http.MethodGet, path, q, nil)
		if err != nil {
			return err
		}
		if st != 200 {
			return apiError(st, data)
		}
		next, err := page(data)
		if err != nil {
			return err
		}
		if next == "" || seen[next] {
			return nil
		}
		seen[next] = true
		token = next
	}
	return nil
}

// namespaceRE is a Kubernetes namespace name (a DNS-1123 label).
var namespaceRE = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)
