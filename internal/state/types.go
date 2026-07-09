package state

import "time"

// DeploymentRecord is the persisted form of a deployment.
type DeploymentRecord struct {
	ID                    string    `json:"id"`
	VMID                  string    `json:"vm_id"`
	VMIP                  string    `json:"vm_ip"`
	VMFlavor              string    `json:"vm_flavor"`
	Region                string    `json:"region"`
	Status                string    `json:"status"` // creating, running, ssh_timeout, destroying, destroyed
	ComposeHash           string    `json:"compose_hash"`
	SSHUser               string    `json:"ssh_user,omitempty"`
	SSHKeyFingerprint     string    `json:"ssh_key_fingerprint,omitempty"`
	SSHHostKeyFingerprint string    `json:"ssh_host_key_fingerprint,omitempty"`
	ExposedPorts          []int     `json:"exposed_ports,omitempty"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

// File is the root state file structure.
type File struct {
	Version     int                `json:"version"`
	Project     string             `json:"project,omitempty"`
	Deployments []DeploymentRecord `json:"deployments"`
}
