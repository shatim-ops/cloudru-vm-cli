package provision

import (
	"encoding/json"
	"time"
)

// VM represents a Cloud.ru virtual machine.
type VM struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Status     string    `json:"state"` // running, stopped, error, creating
	FlavorID   string    `json:"flavor_id"`
	ImageID    string    `json:"image_id"`
	Region     string    `json:"availability_zone_id"`
	PublicIP   string    `json:"public_ip,omitempty"`
	FloatingIP string    `json:"floating_ip,omitempty"`
	CreatedAt  time.Time `json:"created_time"`
	// The /api/v1/vms list returns the zone nested, not as a flat id.
	AvailabilityZone *ZoneRef `json:"availability_zone,omitempty"`
}

// ZoneRef is a nested {id,name} availability-zone reference.
type ZoneRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ZoneID returns the VM's availability-zone id from either the flat or nested field.
func (v *VM) ZoneID() string {
	if v.Region != "" {
		return v.Region
	}
	if v.AvailabilityZone != nil {
		return v.AvailabilityZone.ID
	}
	return ""
}

// CreateVMRequest contains parameters for VM creation (v1.1 schema).
//
// The field set mirrors the request body proven to work against the Cloud.ru
// tenant by the Python reference implementation: it prefers *_name fields
// (flavor_name, image_name, disk_type_name) over UUIDs, carries an explicit
// interfaces section (subnet + security groups + optional attach_external_ip_id
// for "variant A" floating-IP attach at creation), and omits empty public_key
// (an empty string triggers a 500 on this platform).
type CreateVMRequest struct {
	ProjectID          string            `json:"project_id"`
	Name               string            `json:"name"`
	FlavorID           string            `json:"flavor_id,omitempty"`
	FlavorName         string            `json:"flavor_name,omitempty"`
	ImageID            string            `json:"image_id,omitempty"`
	ImageName          string            `json:"image_name,omitempty"`
	AvailabilityZoneID string            `json:"availability_zone_id,omitempty"`
	Disks              []DiskReq         `json:"disks"`
	ImageMetadata      map[string]string `json:"image_metadata,omitempty"`
	CloudInit          string            `json:"cloud_init,omitempty"`
	Interfaces         []InterfaceReq    `json:"interfaces,omitempty"`
}

// DiskReq describes a disk to attach to a new VM (VmCreateNewDiskItem).
// Either DiskTypeID or DiskTypeName may be set; name is preferred to avoid
// pinning to a UUID that can change between projects/images.
type DiskReq struct {
	Name         string `json:"name"`
	Size         int    `json:"size"`
	DiskTypeID   string `json:"disk_type_id,omitempty"`
	DiskTypeName string `json:"disk_type_name,omitempty"`
}

// InterfaceReq describes a network interface for VM creation (v1.1).
// AttachExternalIPID, when set, attaches an existing available floating IP at
// creation time ("variant A"). new_external_ip is intentionally never sent: it
// triggers 500 ExternalNetNotFound on this platform.
type InterfaceReq struct {
	Type                     string   `json:"type"` // "regular" | "direct_ip"
	SubnetID                 string   `json:"subnet_id,omitempty"`
	InterfaceSecurityEnabled bool     `json:"interface_security_enabled"`
	SecurityGroups           []string `json:"security_groups,omitempty"`
	AttachExternalIPID       string   `json:"attach_external_ip_id,omitempty"`
}

// Subnet represents a Cloud.ru subnet (GET /api/v1/subnets).
type Subnet struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	AvailabilityZoneID string `json:"availability_zone_id"`
	SubnetAddress      string `json:"subnet_address,omitempty"`
	VPCID              string `json:"vpc_id,omitempty"`
}

// SecurityGroup represents a Cloud.ru security group (GET /api/v1/security-groups).
// The documented list response has no rules, only their counts: read them
// with ListSecurityGroupRules.
type SecurityGroup struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	AvailabilityZoneID string   `json:"availability_zone_id,omitempty"`
	Rules              []SGRule `json:"rules,omitempty"`
}

// SGRule is a security-group rule. Used both for reading existing rules and for
// creating new ones (POST /api/v1/security-groups/{id}/rules). The create
// endpoint requires direction/ether_type/ip_protocol/port_range and rejects
// unknown fields (additionalProperties:false), so id/description/remote/state
// are omitempty.
type SGRule struct {
	ID             string `json:"id,omitempty"`
	Direction      string `json:"direction"`   // ingress | egress
	EtherType      string `json:"ether_type"`  // IPv4 | IPv6
	IPProtocol     string `json:"ip_protocol"` // tcp | udp | icmp
	PortRange      string `json:"port_range"`  // e.g. "80:80"
	RemoteIPPrefix string `json:"remote_ip_prefix,omitempty"`
	Description    string `json:"description,omitempty"`
	State          string `json:"state,omitempty"` // read only: created, creating, deleting, error…
}

// DiskType represents a Cloud.ru disk type (GET /api/v1/disk-types).
type DiskType struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// AvailabilityZone represents a Cloud.ru availability zone.
type AvailabilityZone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Flavor represents a VM sizing tier.
type Flavor struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	VCPUs    int    `json:"cpu"`
	RAMGB    int    `json:"ram"`
	DiskGB   int    `json:"disk_gb,omitempty"`
	FreeTier bool   `json:"free_tier"`
}

// Image represents a Cloud.ru VM boot image.
type Image struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	OS     string `json:"os"`
	Status string `json:"status"` // active, creating, deleted
}

// ImageDetail contains full image info including required metadata fields.
type ImageDetail struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	OS          string `json:"os"`
	Status      string `json:"status"`
	MinCPU      int    `json:"min_cpu"`
	MinRAM      int    `json:"min_ram"`
	MinDisk     int    `json:"min_disk"`
	// UserDataTemplate is the cloud-init template the platform renders when a
	// VM is created from this image; MetadataFields declares the values it
	// substitutes.
	UserDataTemplate string               `json:"user_data_template"`
	MetadataFields   []ImageMetadataField `json:"image_metadata"`
	// AvailZones carries the real build state: the top-level Status is often
	// empty, and an image is usable only once a zone reports "created".
	AvailZones []ImageAvailZone `json:"availability_zones"`
}

// ImageAvailZone is an image's per-zone build state and size.
type ImageAvailZone struct {
	AvailabilityZoneID   string `json:"availability_zone_id"`
	AvailabilityZoneName string `json:"availability_zone_name"`
	State                string `json:"state"`
	Size                 int64  `json:"size"`
	Uploaded             bool   `json:"uploaded"`
	Default              bool   `json:"default"`
	Enabled              bool   `json:"enabled"`
}

// Ready reports whether the image finished building in at least one zone.
func (i *ImageDetail) Ready() bool {
	for _, az := range i.AvailZones {
		if az.State == "created" {
			return true
		}
	}
	return false
}

// ImageMetadataField describes a metadata field required by an image.
type ImageMetadataField struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Required       bool   `json:"is_required"`
	MandatoryGroup string `json:"mandatory_group"`
	DefaultValue   string `json:"default_value"`
	FieldType      string `json:"field_type"`
	Sensitive      bool   `json:"sensitive"`
}

// FloatingIP represents a Cloud.ru floating (public) IP address.
// Interface/VM are raw because the list endpoint may return them as nested
// objects, strings, or omit them; presence is what signals "in use".
type FloatingIP struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	IPAddress string          `json:"ip_address"`
	State     string          `json:"state"`
	Interface json.RawMessage `json:"interface,omitempty"`
	VM        json.RawMessage `json:"vm,omitempty"`
}

// Available reports whether this floating IP can be attached to a new VM.
// Mirrors the Python reference: state=="available", or no interface/vm bound
// (some tenants omit the state field on the list endpoint).
func (f FloatingIP) Available() bool {
	if f.State == "available" {
		return true
	}
	if f.State == "in_use" {
		return false
	}
	return isEmptyRaw(f.Interface) && isEmptyRaw(f.VM)
}

func isEmptyRaw(r json.RawMessage) bool {
	s := string(r)
	return s == "" || s == "null" || s == "\"\"" || s == "{}" || s == "[]"
}

// CreateFloatingIPRequest contains parameters for floating IP creation.
type CreateFloatingIPRequest struct {
	Name               string `json:"name"`
	ProjectID          string `json:"project_id"`
	InterfaceID        string `json:"interface_id,omitempty"` // empty: a free FIP, attached later
	AvailabilityZoneID string `json:"availability_zone_id"`
}

// VMDetail is the full VM representation from the API including interfaces.
type VMDetail struct {
	ID         string        `json:"id"`
	Name       string        `json:"name"`
	State      string        `json:"state"`
	Interfaces []VMInterface `json:"interfaces"`
}

// VMInterface represents a network interface attached to a VM.
//
// The API's ip_address carries the PRIVATE (subnet-internal) address when
// Type=="regular", and a directly-attached PUBLIC address when
// Type=="direct_ip". A regular interface may additionally have a FloatingIP
// (the public address bound to it). Callers that need a VM's private vs public
// IP must branch on Type — see handleVMGet.
type VMInterface struct {
	ID         string           `json:"id"`
	Name       string           `json:"name,omitempty"`
	IPAddress  string           `json:"ip_address"`
	Type       string           `json:"type,omitempty"` // "regular" | "direct_ip"
	Primary    bool             `json:"primary"`
	State      string           `json:"state"`
	FloatingIP *IfaceFloatingIP `json:"floating_ip,omitempty"`
}

// IfaceFloatingIP is the public (floating) IP bound to a VM interface.
type IfaceFloatingIP struct {
	ID        string `json:"id"`
	IPAddress string `json:"ip_address"`
	State     string `json:"state"`
}

// paginatedResponse wraps paginated API responses.
type paginatedResponse[T any] struct {
	Items  []T `json:"items"`
	Total  int `json:"total"`
	Offset int `json:"offset"`
	Limit  int `json:"limit"`
}
