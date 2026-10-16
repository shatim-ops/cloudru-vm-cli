package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/mk8s"
)

// Addons of Managed Kubernetes: k8s_addons (the catalogue) and
// k8s_addon_versions (the releases of one addon), both read-only. evo does
// not install addons in v1.0: the install tool was taken out until the
// mk8s team explains why an accepted install brings up no pods
// (docs/internal/standalone/BACKLOG.md [k8s-addons]).

// addonRow is one compact row of k8s_addons.
type addonRow struct {
	Name        string   `json:"name"`
	DisplayName string   `json:"display_name,omitempty"`
	Categories  []string `json:"categories"`
	Billable    bool     `json:"billable"`
}

// handleK8sAddons searches the addon catalogue. Read-only.
func (s *Registry) handleK8sAddons(ctx context.Context, args map[string]any) (string, bool) {
	cli, _, _, err := s.k8sClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	all, err := cli.ListAddons(ctx)
	if err != nil {
		return errResult(err), true
	}
	query := strings.ToLower(strings.TrimSpace(stringArg(args, "query")))
	category := strings.TrimSpace(stringArg(args, "category"))
	billable, filterBillable := args["billable"].(bool)

	cats := map[string]bool{}
	rows := []addonRow{}
	for _, a := range all {
		for _, c := range a.Categories {
			cats[c] = true
		}
		if query != "" && !strings.Contains(strings.ToLower(a.Name+"\n"+a.DisplayName+"\n"+a.Description), query) {
			continue
		}
		if category != "" && !hasCategory(a, category) {
			continue
		}
		if filterBillable && a.IsBillable != billable {
			continue
		}
		categories := a.Categories
		if categories == nil {
			categories = []string{}
		}
		rows = append(rows, addonRow{
			Name: a.Name, DisplayName: a.DisplayName, Categories: categories, Billable: a.IsBillable,
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	catList := make([]string, 0, len(cats))
	for c := range cats {
		catList = append(catList, c)
	}
	sort.Strings(catList)
	return toJSON(map[string]any{"addons": rows, "shown": len(rows), "total": len(all), "categories": catList}), false
}

func hasCategory(a mk8s.Addon, category string) bool {
	for _, c := range a.Categories {
		if strings.EqualFold(c, category) {
			return true
		}
	}
	return false
}

// findAddon is the catalogue entry with this name (case-insensitive), or
// NOT_FOUND with the command that searches the catalogue.
func findAddon(ctx context.Context, cli *mk8s.Client, name string) (mk8s.Addon, error) {
	all, err := cli.ListAddons(ctx)
	if err != nil {
		return mk8s.Addon{}, err
	}
	for _, a := range all {
		if strings.EqualFold(a.Name, name) {
			return a, nil
		}
	}
	hint := i18n.T("List the addons: evo k8s addons <substring> (MCP: k8s_addons query=…).",
		"Список плагинов: evo k8s addons <подстрока> (MCP: k8s_addons query=…).")
	if similar := similarAddons(all, name); len(similar) > 0 {
		hint += i18n.Tf(" Similar: %s.", " Похожие: %s.", strings.Join(similar, ", "))
	}
	return mk8s.Addon{}, clierrors.New(clierrors.CodeNotFound,
		i18n.Tf("addon %q not found in the Managed Kubernetes catalogue", "плагин %q не найден в каталоге Managed Kubernetes", name), hint)
}

// similarAddons are up to five catalogue names that contain the first word
// of a name that found nothing ("cert" of "cert-manger").
func similarAddons(all []mk8s.Addon, name string) []string {
	words := strings.FieldsFunc(strings.ToLower(name), func(r rune) bool { return r == '-' || r == '_' || r == ' ' || r == '.' })
	if len(words) == 0 || len([]rune(words[0])) < 3 {
		return nil
	}
	word := words[0]
	var out []string
	for _, a := range all {
		if strings.Contains(strings.ToLower(a.Name+" "+a.DisplayName), word) {
			out = append(out, a.Name)
		}
	}
	sort.Strings(out)
	if len(out) > 5 {
		out = out[:5]
	}
	return out
}

// releaseRow is one release of k8s_addon_versions.
func releaseRow(r mk8s.AddonRelease, latest, full bool) map[string]any {
	row := map[string]any{
		"version":       r.AppVersion,
		"addon_version": r.AddonVersion,
		"mode":          mk8s.NormalizeMode(r.Mode),
		"kube_versions": r.Requirements.KubeVersions,
		"ready_workers": int(r.Requirements.ReadyWorkers),
		"namespace":     r.AddonNamespace,
		"has_schema":    r.ConfigurationSchema != "",
		"schema_bytes":  len(r.ConfigurationSchema),
		"latest":        latest,
	}
	if full && r.ConfigurationSchema != "" {
		// The schema as JSON when it is JSON, else the string as is.
		if json.Valid([]byte(r.ConfigurationSchema)) {
			row["configuration_schema"] = json.RawMessage(r.ConfigurationSchema)
		} else {
			row["configuration_schema"] = r.ConfigurationSchema
		}
	}
	return row
}

// handleK8sAddonVersions lists the releases of an addon, newest first.
// Read-only. The configuration schema (up to ~94 KB) only with full=true.
func (s *Registry) handleK8sAddonVersions(ctx context.Context, args map[string]any) (string, bool) {
	name := strings.TrimSpace(stringArg(args, "name"))
	if name == "" {
		return usageResult(fmt.Errorf("name is required (an addon name from k8s_addons)")), true
	}
	full := boolArg(args, "full", false)
	cli, _, _, err := s.k8sClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	addon, err := findAddon(ctx, cli, name)
	if err != nil {
		return errResult(err), true
	}
	rels, err := cli.GetAddonReleases(ctx, addon.Name)
	if err != nil {
		return errResult(err), true
	}
	mk8s.SortReleases(rels)
	latest, hasLatest := mk8s.LatestRelease(rels)
	rows := make([]map[string]any, 0, len(rels))
	for _, r := range rels {
		isLatest := hasLatest && r.AppVersion == latest.AppVersion && r.AddonVersion == latest.AddonVersion
		rows = append(rows, releaseRow(r, isLatest, full))
	}
	out := map[string]any{
		"addon":        addon.Name,
		"display_name": addon.DisplayName,
		"billable":     addon.IsBillable,
		"releases":     rows,
	}
	if hasLatest {
		out["latest"] = latest.AppVersion
	} else {
		out["note"] = "No supported release (all END_OF_LIFE or INACTIVE)."
	}
	return toJSON(out), false
}
