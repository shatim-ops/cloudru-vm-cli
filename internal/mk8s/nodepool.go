package mk8s

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// --- Node pool types (v3) ---

type FixedScale struct {
	Count int `json:"count"`
}

type AutoScale struct {
	MinCount     int `json:"minCount"`
	MaxCount     int `json:"maxCount"`
	InitialCount int `json:"initialCount,omitempty"`
}

type ScalePolicy struct {
	FixedScale *FixedScale `json:"fixedScale,omitempty"`
	AutoScale  *AutoScale  `json:"autoScale,omitempty"`
}

type DiskRequest struct {
	TypeName string `json:"typeName,omitempty"`
	Size     int    `json:"size,omitempty"`
}

type MachineConfigurationRequest struct {
	FlavorID string       `json:"flavorId"`
	Disk     *DiskRequest `json:"disk,omitempty"`
}

type NodePoolNetworkConfigurationRequest struct {
	NodesSubnetID   string `json:"nodesSubnetId"`
	SecurityGroupID string `json:"securityGroupId,omitempty"`
}

type NodePoolUpdateConfiguration struct {
	Strategy string `json:"strategy"` // NODE_POOL_UPDATE_STRATEGY_ROLLING_UPDATE
}

// CreateNodePoolReq mirrors the confirmed v3 CreateNodePoolRequest.
type CreateNodePoolReq struct {
	ClusterID            string                              `json:"clusterId"`
	Name                 string                              `json:"name"`
	ScalePolicy          ScalePolicy                         `json:"scalePolicy"`
	MachineConfiguration MachineConfigurationRequest         `json:"machineConfiguration"`
	NetworkConfiguration NodePoolNetworkConfigurationRequest `json:"networkConfiguration"`
	UpdateConfiguration  NodePoolUpdateConfiguration         `json:"updateConfiguration"`
}

// CreateNodePool submits a node-pool create and returns the async operation.
func (c *Client) CreateNodePool(ctx context.Context, req CreateNodePoolReq) (*Operation, error) {
	data, st, err := c.do(ctx, http.MethodPost, "/v3/nodePools", nil, req)
	if err != nil {
		return nil, err
	}
	if st != 200 {
		return nil, apiError(st, data)
	}
	var op Operation
	if err := json.Unmarshal(data, &op); err != nil {
		return nil, err
	}
	return &op, nil
}

// ListNodePools lists node pools (optionally filtered by clusterId via query).
func (c *Client) ListNodePools(ctx context.Context, clusterID string) ([]map[string]any, error) {
	q := url.Values{}
	if clusterID != "" {
		q.Set("clusterId", clusterID)
	}
	data, st, err := c.do(ctx, http.MethodGet, "/v3/nodePools", q, nil)
	if err != nil {
		return nil, err
	}
	if st != 200 {
		return nil, apiError(st, data)
	}
	var r struct {
		NodePools []map[string]any `json:"nodePools"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	return r.NodePools, nil
}
