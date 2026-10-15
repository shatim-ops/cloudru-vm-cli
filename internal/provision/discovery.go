package provision

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// listProjectScoped fetches a project-scoped list endpoint and decodes either a
// paginated {items:[...]} envelope or a bare JSON array. Cloud.ru is
// inconsistent about which shape it returns, so we accept both (matching the
// Python reference implementation).
func listProjectScoped[T any](a *CloudRuAdapter, ctx context.Context, path, projectID string) ([]*T, error) {
	full := path
	if projectID != "" {
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		full = fmt.Sprintf("%s%sproject_id=%s&limit=200", path, sep, url.QueryEscape(projectID))
	}

	resp, err := a.doWithRetry(ctx, http.MethodGet, full, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, clierrors.Wrap(clierrors.CodeAPIError, "failed to read response for "+path, "API returned unexpected format", err)
	}

	var page paginatedResponse[T]
	if err := json.Unmarshal(body, &page); err == nil && len(page.Items) > 0 {
		out := make([]*T, len(page.Items))
		for i := range page.Items {
			out[i] = &page.Items[i]
		}
		return out, nil
	}

	var arr []T
	if err := json.Unmarshal(body, &arr); err == nil {
		out := make([]*T, len(arr))
		for i := range arr {
			out[i] = &arr[i]
		}
		return out, nil
	}

	return nil, clierrors.New(clierrors.CodeAPIError, "failed to decode list response for "+path, "API returned unexpected format — run with --verbose to debug")
}

// ListVMsInProject lists VMs scoped to a project (the /vms endpoint requires
// project_id in this tenant).
func (a *CloudRuAdapter) ListVMsInProject(ctx context.Context, projectID string) ([]*VM, error) {
	return listProjectScoped[VM](a, ctx, "/api/v1/vms", projectID)
}

// DeleteVMInProject deletes a VM by ID, passing project_id.
func (a *CloudRuAdapter) DeleteVMInProject(ctx context.Context, id, projectID string) error {
	path := fmt.Sprintf("/api/v1/vms/%s", seg(id))
	if projectID != "" {
		path += "?project_id=" + url.QueryEscape(projectID)
	}
	resp, err := a.doWithRetry(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// AddSecurityGroupRule creates one rule via
// POST /api/v1/security-groups/{id}/rules and returns it as created. The
// documented answer is 201 with the rule (id required); a body that does not
// decode yields a rule without an ID rather than an error — the rule exists.
func (a *CloudRuAdapter) AddSecurityGroupRule(ctx context.Context, sgID, projectID string, rule SGRule) (*SGRule, error) {
	body, err := json.Marshal(rule)
	if err != nil {
		return nil, clierrors.Wrap(clierrors.CodeAPIError, "failed to marshal SG rule", "Check parameters", err)
	}
	path := fmt.Sprintf("/api/v1/security-groups/%s/rules", seg(sgID))
	if projectID != "" {
		path += "?project_id=" + url.QueryEscape(projectID)
	}
	resp, err := a.doWithRetry(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var created SGRule
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxErrorBody)).Decode(&created); err != nil {
		created = SGRule{}
	}
	return &created, nil
}

const (
	// sgRulesPageSize is the API's maximum limit for the rules list.
	sgRulesPageSize = 100
	// sgRulesMaxPages bounds the walk: a server that keeps answering full
	// pages must not keep the CLI looping.
	sgRulesMaxPages = 100
	maxListBody     = 8 << 20
)

// ListSecurityGroupRules lists a group's rules via
// GET /api/v1/security-groups/{id}/rules, following offset pages. The group
// list (GET /api/v1/security-groups) carries only rule counts.
func (a *CloudRuAdapter) ListSecurityGroupRules(ctx context.Context, sgID string) ([]*SGRule, error) {
	var out []*SGRule
	offset := 0
	for range sgRulesMaxPages {
		path := fmt.Sprintf("/api/v1/security-groups/%s/rules?offset=%d&limit=%d", seg(sgID), offset, sgRulesPageSize)
		resp, err := a.doWithRetry(ctx, http.MethodGet, path, nil)
		if err != nil {
			return nil, err
		}
		var page paginatedResponse[SGRule]
		err = json.NewDecoder(io.LimitReader(resp.Body, maxListBody)).Decode(&page)
		resp.Body.Close()
		if err != nil {
			return nil, clierrors.Wrap(clierrors.CodeAPIError, "failed to decode security group rules", "API returned unexpected format — run with --verbose to debug", err)
		}
		for i := range page.Items {
			out = append(out, &page.Items[i])
		}
		offset += len(page.Items)
		if len(page.Items) == 0 || offset >= page.Total {
			return out, nil
		}
	}
	return out, nil
}

// DeleteSecurityGroupRule deletes one rule via
// DELETE /api/v1/security-groups/{id}/rules/{rule_id} (204; 404 when the
// rule is gone, which the adapter reports as CodeVMNotFound).
func (a *CloudRuAdapter) DeleteSecurityGroupRule(ctx context.Context, sgID, ruleID string) error {
	path := fmt.Sprintf("/api/v1/security-groups/%s/rules/%s", seg(sgID), seg(ruleID))
	resp, err := a.doWithRetry(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// ListDiskTypes lists disk types via GET /api/v1/disk-types.
func (a *CloudRuAdapter) ListDiskTypes(ctx context.Context, projectID string) ([]*DiskType, error) {
	return listProjectScoped[DiskType](a, ctx, "/api/v1/disk-types", projectID)
}

// ListSubnets lists subnets via GET /api/v1/subnets.
func (a *CloudRuAdapter) ListSubnets(ctx context.Context, projectID string) ([]*Subnet, error) {
	return listProjectScoped[Subnet](a, ctx, "/api/v1/subnets", projectID)
}

// ListSecurityGroups lists security groups via GET /api/v1/security-groups.
func (a *CloudRuAdapter) ListSecurityGroups(ctx context.Context, projectID string) ([]*SecurityGroup, error) {
	return listProjectScoped[SecurityGroup](a, ctx, "/api/v1/security-groups", projectID)
}

// ListFloatingIPs lists floating IPs via GET /api/v1/floating-ips.
func (a *CloudRuAdapter) ListFloatingIPs(ctx context.Context, projectID string) ([]*FloatingIP, error) {
	return listProjectScoped[FloatingIP](a, ctx, "/api/v1/floating-ips", projectID)
}

// AttachFloatingIP binds an existing floating IP to a VM interface via
// PUT /api/v1/floating-ips/{id} (floating-IP "variant B": attach after the VM
// is running). Returns the updated floating IP.
func (a *CloudRuAdapter) AttachFloatingIP(ctx context.Context, fipID, projectID, interfaceID, name string) (*FloatingIP, error) {
	path := fmt.Sprintf("/api/v1/floating-ips/%s", seg(fipID))
	if projectID != "" {
		path += "?project_id=" + url.QueryEscape(projectID)
	}
	body, err := json.Marshal(map[string]string{"interface_id": interfaceID, "name": name})
	if err != nil {
		return nil, clierrors.Wrap(clierrors.CodeAPIError, "failed to marshal attach request", "Check parameters", err)
	}
	resp, err := a.doWithRetry(ctx, http.MethodPut, path, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var fip FloatingIP
	if err := json.NewDecoder(resp.Body).Decode(&fip); err != nil {
		// PUT may return 204/empty; treat decode failure as soft success.
		return &FloatingIP{ID: fipID}, nil
	}
	return &fip, nil
}

// DeleteFloatingIP releases a floating IP via DELETE /api/v1/floating-ips/{id}.
func (a *CloudRuAdapter) DeleteFloatingIP(ctx context.Context, fipID, projectID string) error {
	path := fmt.Sprintf("/api/v1/floating-ips/%s", seg(fipID))
	if projectID != "" {
		path += "?project_id=" + url.QueryEscape(projectID)
	}
	resp, err := a.doWithRetry(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}
