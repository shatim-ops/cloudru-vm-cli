package provision

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// CreateDiskReq mirrors POST /api/v1/disks. Required (confirmed via the live 422
// validator): name, size, project_id (in body), availability_zone_id (UUID v4),
// and exactly one of disk_type_id / disk_type_name.
type CreateDiskReq struct {
	Name               string `json:"name"`
	Size               int    `json:"size"` // GB
	DiskTypeName       string `json:"disk_type_name,omitempty"`
	DiskTypeID         string `json:"disk_type_id,omitempty"`
	AvailabilityZoneID string `json:"availability_zone_id"`
	ProjectID          string `json:"project_id"`
}

// ListDisks lists disks (volumes) in a project.
func (a *CloudRuAdapter) ListDisks(ctx context.Context, projectID string) ([]map[string]any, error) {
	q := url.Values{}
	if projectID != "" {
		q.Set("project_id", projectID)
	}
	path := "/api/v1/disks"
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}
	resp, err := a.doWithRetry(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var r struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	return r.Items, nil
}

// CreateDisk creates a disk and returns the raw API object.
func (a *CloudRuAdapter) CreateDisk(ctx context.Context, req CreateDiskReq) (map[string]any, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	path := "/api/v1/disks"
	if req.ProjectID != "" {
		path += "?project_id=" + url.QueryEscape(req.ProjectID)
	}
	resp, err := a.doWithRetry(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}

// AttachDisk attaches a disk to a VM via POST /api/v1/disks/{id}/attach {vm_id}.
func (a *CloudRuAdapter) AttachDisk(ctx context.Context, diskID, vmID, projectID string) (map[string]any, error) {
	return a.diskAction(ctx, "attach", diskID, vmID, projectID)
}

// DetachDisk detaches a disk from a VM via POST /api/v1/disks/{id}/detach {vm_id}.
func (a *CloudRuAdapter) DetachDisk(ctx context.Context, diskID, vmID, projectID string) (map[string]any, error) {
	return a.diskAction(ctx, "detach", diskID, vmID, projectID)
}

func (a *CloudRuAdapter) diskAction(ctx context.Context, action, diskID, vmID, projectID string) (map[string]any, error) {
	body, err := json.Marshal(map[string]string{"vm_id": vmID})
	if err != nil {
		return nil, err
	}
	path := fmt.Sprintf("/api/v1/disks/%s/%s", seg(diskID), action)
	if projectID != "" {
		path += "?project_id=" + url.QueryEscape(projectID)
	}
	resp, err := a.doWithRetry(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var m map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&m)
	return m, nil
}

// MaxDiskGB is the largest disk: 16 TB (docs, «Диски», ограничения;
// maximum 16384 of size in PublicDiskCreateRequest/UpdateRequest).
const MaxDiskGB = 16384

// ResizeDisk grows a disk to sizeGB via PUT /api/v1/disks/{id} {size}. The API
// caps size at MaxDiskGB and forbids SHRINK and free-tier resize (returns 422).
func (a *CloudRuAdapter) ResizeDisk(ctx context.Context, diskID string, sizeGB int, projectID string) (map[string]any, error) {
	body, err := json.Marshal(map[string]int{"size": sizeGB})
	if err != nil {
		return nil, err
	}
	path := fmt.Sprintf("/api/v1/disks/%s", seg(diskID))
	if projectID != "" {
		path += "?project_id=" + url.QueryEscape(projectID)
	}
	resp, err := a.doWithRetry(ctx, http.MethodPut, path, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}

// DeleteDisk deletes a disk by id.
func (a *CloudRuAdapter) DeleteDisk(ctx context.Context, diskID, projectID string) error {
	path := fmt.Sprintf("/api/v1/disks/%s", seg(diskID))
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
