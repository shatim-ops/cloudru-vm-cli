package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/httpretry"
)

// adminGET performs an authenticated GET against an absolute Cloud.ru API URL
// and returns the decoded JSON. Used for the read-only IAM / Organization
// admin endpoints (they share the same IAM bearer token).
func (s *Registry) adminGET(ctx context.Context, rawurl string) (map[string]any, error) {
	eng, err := s.newEngine(nil)
	if err != nil {
		return nil, err
	}
	tok, err := eng.Auth.Token(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawurl, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok.Value)
	cl := httpretry.NewClient(30 * time.Second)
	resp, err := cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var m map[string]any
	if e := json.Unmarshal(data, &m); e != nil {
		return nil, &adminHTTPError{Status: resp.StatusCode, Msg: string(data[:min(len(data), 300)])}
	}
	if resp.StatusCode != 200 {
		msg, _ := m["message"].(string)
		return nil, &adminHTTPError{Status: resp.StatusCode, Msg: msg}
	}
	return m, nil
}

// adminHTTPError is an answer of the API other than 200 (or not JSON): the
// API was reached and refused, unlike a network error.
type adminHTTPError struct {
	Status int
	Msg    string
}

func (e *adminHTTPError) Error() string { return fmt.Sprintf("HTTP %d: %s", e.Status, e.Msg) }

// projectID returns the configured project id.
func (s *Registry) projectID(ctx context.Context) string {
	eng, err := s.newEngine(nil)
	if err != nil {
		return ""
	}
	return eng.Config.ProjectID
}

// handleIamServiceAccounts lists service accounts for a project (or the current
// one). This is where a k8s cluster_sa_id can be discovered.
func (s *Registry) handleIamServiceAccounts(ctx context.Context, args map[string]any) (string, bool) {
	pid := firstNonEmptyStr(stringArg(args, "project_id"), s.projectID(ctx))
	m, err := s.adminGET(ctx, "https://iam.api.cloud.ru/api/v2/serviceAccounts?target.projectId="+neturl.QueryEscape(pid))
	if err != nil {
		return errResult(err), true
	}
	// serviceAccounts in the spec, service_accounts as read so far.
	raw, _ := m["service_accounts"].([]any)
	if raw == nil {
		raw, _ = m["serviceAccounts"].([]any)
	}
	out := make([]map[string]any, 0, len(raw))
	for _, x := range raw {
		sa, _ := x.(map[string]any)
		if sa == nil {
			continue
		}
		out = append(out, map[string]any{
			"id": sa["id"], "name": sa["name"], "email": sa["email"],
			"type": sa["type"], "enabled": sa["enabled"],
		})
	}
	return toJSON(map[string]any{"service_accounts": out}), false
}

// orgProjectsNote is the note of org_projects when the Organization API
// refuses the list: the same case and wording as 'evo project list'.
const orgProjectsNote = "unknown (the Organization API does not list projects for this key)"

// handleOrgProjects lists projects the caller can see. A key the
// Organization API does not list projects for (a project-level service
// account: 400 "user not found", 14.10.2026) is not a failure — the key
// works in its project. As 'evo project list' does (QA r3 C3), the answer
// is then projects: null, the active project and a note, not an error
// (it was NOT_FOUND, exit 4, from "user not found" in the message).
func (s *Registry) handleOrgProjects(ctx context.Context, args map[string]any) (string, bool) {
	m, err := s.adminGET(ctx, "https://organization.api.cloud.ru/v3/projects")
	var he *adminHTTPError
	if errors.As(err, &he) {
		return toJSON(map[string]any{
			"projects":    nil,
			"active":      s.projectID(ctx),
			"note":        orgProjectsNote,
			"http_status": he.Status,
			"hint":        "Not an error: the key works in the active project (whoami). Pass project_id where a tool takes one.",
		}), false
	}
	if err != nil {
		return errResult(err), true
	}
	raw, _ := m["projects"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, x := range raw {
		p, _ := x.(map[string]any)
		if p == nil {
			continue
		}
		out = append(out, map[string]any{
			"id": p["id"], "name": p["name"], "status": p["status"],
			"organization_unit_id": p["organization_unit_id"],
		})
	}
	return toJSON(map[string]any{"projects": out}), false
}

// handleOrgQuotas lists quota usages (limits vs usage) — useful for capacity
// planning. Requires the caller to have customer/project quota permissions.
func (s *Registry) handleOrgQuotas(ctx context.Context, args map[string]any) (string, bool) {
	url := "https://organization.api.cloud.ru/v3/quotas/usages?page_size=200"
	if pid := stringArg(args, "project_id"); pid != "" {
		url += "&subject_type_codes=QUOTA_SUBJECT_TYPE_PROJECT&subject_ids=" + neturl.QueryEscape(pid)
	}
	m, err := s.adminGET(ctx, url)
	if err != nil {
		// The org quota endpoint needs a customer/organization scope a service
		// account typically lacks (403 "dont have permissions to customers").
		// That's an expected limitation, not a failure — degrade to a soft
		// "unavailable" so it doesn't read as a hard error on an early turn.
		low := strings.ToLower(err.Error())
		if strings.Contains(low, "403") || strings.Contains(low, "permission") {
			return toJSON(map[string]any{
				"status":       "unavailable",
				"quota_usages": []any{},
				"reason":       "this service account lacks customer/organization quota scope; quota usages are not readable with the current credentials",
				"detail":       err.Error(),
			}), false
		}
		return errResult(err), true
	}
	raw, _ := m["usages"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, x := range raw {
		u, _ := x.(map[string]any)
		if u == nil {
			continue
		}
		out = append(out, map[string]any{
			"resource": u["resource_type_code"], "limit": u["individual_limit"], "usage": u["individual_usage"],
		})
	}
	return toJSON(map[string]any{"quota_usages": out}), false
}
