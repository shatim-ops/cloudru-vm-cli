package mcp

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/shatim-ops/cloudru-vm-cli/internal/engine"
	"github.com/shatim-ops/cloudru-vm-cli/internal/output"
	"github.com/shatim-ops/cloudru-vm-cli/internal/provision"
	"github.com/shatim-ops/cloudru-vm-cli/internal/state"
)

// registerTools defines the advertised tools and wires their handlers.
func (s *Server) registerTools() {
	s.tools = []Tool{
		{
			Name:        "deploy",
			Description: "Deploy the local docker-compose project to a Cloud.ru VM. Provisions a VM (auto-sized), assigns a public floating IP, uploads compose files over SSH and runs 'docker compose up -d'. Reuses the existing VM and skips unchanged deploys unless force is set. Requires Cloud.ru credentials (CLOUDRU_KEY_ID/CLOUDRU_SECRET) and project_id/region/image_id via env, config, or arguments.",
			InputSchema: objSchema(map[string]any{
				"compose_file": strProp("Path to the docker-compose file (default: docker-compose.yml)."),
				"flavor":       strProp("VM flavor, or 'auto' to pick the smallest that fits (default: auto)."),
				"region":       strProp("Availability zone UUID (see list_zones)."),
				"project_id":   strProp("Cloud.ru project UUID."),
				"image_id":     strProp("Boot image UUID (see list_images)."),
				"disk_size":    intProp("Boot disk size in GB (default: 10)."),
				"force":        boolProp("Redeploy even if the compose hash is unchanged."),
			}),
		},
		{
			Name:        "provision",
			Description: "Provision a bare Cloud.ru VM. By default assigns a public floating IP. Set no_public_ip=true for a PRIVATE VM (subnet-only, interface type 'regular') — reachable only inside the VPC and the valid target type for a load balancer. Attaches a subnet + security group automatically (required — omitting the interface 500s). For docker-compose deployments, use 'deploy' instead.",
			InputSchema: objSchema(map[string]any{
				"name":         strProp("VM name (e.g. 'hermes-agent')."),
				"flavor":       strProp("VM flavor name or UUID (e.g. 'lowcost10-2-4'). Default: auto (smallest lowcost, not free-tier)."),
				"region":       strProp("Availability zone UUID (see list_zones)."),
				"project_id":   strProp("Cloud.ru project UUID."),
				"image_id":     strProp("Boot image UUID (see list_images)."),
				"disk_size":    intProp("Boot disk size in GB (default: 10)."),
				"subnet_id":    strProp("Subnet UUID for the interface (default: a subnet in the target zone)."),
				"disk_type":    strProp("Boot disk type name, e.g. 'SSD' (default) or 'HDD' (see list_disk_types). A zone short on one type still serves the other: creating a VM on an exhausted type fails with error_creating and no readable reason."),
				"no_public_ip": boolProp("Create a PRIVATE VM (no floating IP, interface type 'regular'). Use for load-balancer targets / internal-only nodes."),
			}),
		},
		{
			Name: "vm_exec",
			Description: "Run a shell script ON a VM over SSH. This is how anything beyond a compose deploy gets done: installing packages, baking a Marketplace image, wiping state before a snapshot. " +
				"DETACHED by default (wait=false): the script is uploaded as a file, started with setsid and survives a dropped connection, so a 20-minute image build is safe. Returns a run_id; poll vm_exec_status for the exit code and log. " +
				"Set wait=true to block until it finishes (timeout_sec, default 1800). The VM must have a public address. " +
				"Runs code as the login user (the script may use sudo): requires confirmed=true.",
			InputSchema: objSchema(map[string]any{
				"vm_id":       strProp("VM UUID (see list / provision result)."),
				"name":        strProp("VM name (alternative to vm_id)."),
				"script":      strProp("The script body. Uploaded as a file and run with bash; nothing is interpolated into a command line."),
				"login":       strProp("SSH login (default: the login recorded for this VM in local state)."),
				"wait":        boolProp("Block until the script finishes (default false: return a run_id immediately)."),
				"timeout_sec": intProp("How long to wait when wait=true (default 1800, max 7200). On timeout the script keeps running on the VM."),
				"confirmed":   boolProp("Must be true — set only after the captain approved the script."),
			}),
		},
		{
			Name:        "vm_exec_status",
			Description: "Report on a script started by vm_exec: whether it is still running, its exit code once finished, and the tail of its output. Read-only; poll this instead of holding a connection open.",
			InputSchema: objSchema(map[string]any{
				"vm_id":  strProp("VM UUID."),
				"name":   strProp("VM name (alternative to vm_id)."),
				"run_id": strProp("The run_id returned by vm_exec."),
				"login":  strProp("SSH login (default: the login recorded for this VM in local state)."),
			}),
		},
		{
			Name:        "vm_download",
			Description: "Bring files back from a VM over SSH. Made for scan reports: the factory's vulnerability scan writes its results on the build VM and the cleanup step wipes that machine, so the results must be pulled first or they never existed. Files land under out_dir on the engine's machine, named by their base name. Read-only on the VM; no confirmation needed. Size-capped (max_bytes, default 32 MiB).",
			InputSchema: objSchema(map[string]any{
				"vm_id":     strProp("VM UUID."),
				"name":      strProp("VM name (alternative to vm_id)."),
				"paths":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Absolute paths of files on the VM to download."},
				"out_dir":   strProp("Existing local directory to write the files into."),
				"login":     strProp("SSH login (default: the login recorded for this VM in local state)."),
				"max_bytes": intProp("Per-file size cap in bytes (default 33554432, hard max 134217728)."),
			}),
		},
		{
			Name:        "vm_upload",
			Description: "Write files onto a VM over SFTP. Use for build inputs that are not scripts: config files, systemd units, a compose file. Paths are relative to the login user's home; move them into place with vm_exec so the privilege is explicit. Mutation: requires confirmed=true.",
			InputSchema: objSchema(map[string]any{
				"vm_id":     strProp("VM UUID."),
				"name":      strProp("VM name (alternative to vm_id)."),
				"files":     strProp("Array of {path, content, mode?} objects. path is relative to the home directory; mode is octal, e.g. \"0755\"."),
				"login":     strProp("SSH login (default: the login recorded for this VM in local state)."),
				"confirmed": boolProp("Must be true — set only after the captain approved."),
			}),
		},
		{
			Name:        "status",
			Description: "Show the current deployment status, including VM ID, public IP, flavor, region, health state and exposed ports. Reads local state; no API call.",
			InputSchema: objSchema(nil),
		},
		{
			Name:        "list",
			Description: "List all recorded deployments for this project (VM IP, status, flavor, creation time). Reads local state; no API call.",
			InputSchema: objSchema(nil),
		},
		{
			Name:        "vm_get",
			Description: "Get LIVE details for one VM by vm_id or name: its PRIVATE IP (private_ip), any public/floating IP (public_ip), state, and every network interface. Use this whenever you need a VM's IP — e.g. to wire a private service endpoint into a config or secret after creating the VM. Unlike 'list'/'status' (which read local state and omit the private IP), this calls the live API. Never shell out to curl/ssh to discover a VM's IP — use this tool.",
			InputSchema: objSchema(map[string]any{
				"vm_id": strProp("VM UUID (from a stack output, deploy_apply/provision result, or list)."),
				"name":  strProp("VM name (alternative to vm_id)."),
			}),
		},
		{
			Name:        "logs",
			Description: "Retrieve container logs from the deployed VM via 'docker compose logs' over SSH.",
			InputSchema: objSchema(map[string]any{
				"service": strProp("Filter by a single compose service name (default: all services)."),
				"tail":    intProp("Number of recent log lines to return (default: 100, 0 = all)."),
			}),
		},
		{
			Name:        "destroy",
			Description: "Permanently delete a VM and all data on it (irreversible). ALWAYS requires confirmed=true (captain approval). With no arguments, deletes the current local deployment. To delete a SPECIFIC VM (e.g. an orphan from a failed deploy), pass vm_id or name. By default the VM's floating IP is released too (release_ip).",
			InputSchema: objSchema(map[string]any{
				"vm_id":      strProp("Delete this specific VM by UUID (see the list tool / vms). Requires confirmed=true."),
				"name":       strProp("Delete a specific VM by name (alternative to vm_id). Requires confirmed=true."),
				"confirmed":  boolProp("Must be true for ANY destroy — set only after the captain approves."),
				"release_ip": boolProp("Also release the VM's floating IP (default: true)."),
			}),
		},
		{
			Name:        "list_zones",
			Description: "List available Cloud.ru availability zones (UUID + name). Use a zone UUID as the 'region' argument for deploy or provision.",
			InputSchema: objSchema(nil),
		},
		{
			Name:        "list_images",
			Description: "List available Cloud.ru VM boot images (UUID + name). Use an image UUID as the 'image_id' argument for deploy or provision.",
			InputSchema: objSchema(nil),
		},
		{
			Name: "image_create",
			Description: "Create a reusable VM image FROM A BOOTABLE DISK — the final step of producing a distributable image (e.g. a Marketplace product): provision a VM, install the product, clean it up, stop the VM, then capture its boot disk here. " +
				"The disk must be bootable AND detached — power the VM off, detach the boot disk with disk_detach, and wait until disk_list reports state 'available'. Stopping the VM is not enough: while the disk is attached the API answers invalid_disk_state_to_create_image. " +
				"For a Marketplace image also pass user_data_template (cloud-init Jinja) plus image_metadata declaring every {{ var }} the template uses — that is how a buyer's SSH key or password reaches the guest. " +
				"Building is asynchronous (~0.7 GB/min): poll list_images for state. Mutation (billable storage): requires confirmed=true.",
			InputSchema: objSchema(map[string]any{
				"name":               strProp("Image name (technical, unique in the project)."),
				"disk_id":            strProp("UUID of the bootable source disk (from disk_list)."),
				"display_name":       strProp("Human-readable name shown in the console."),
				"description":        strProp("Free-text description, MAX 255 characters (the API rejects longer with too_long). Keep it to product, pinned version and digest; legal disclaimers belong in the Marketplace listing, not here."),
				"zone":               strProp("Availability zone name or UUID to build the image in. Defaults to the configured region, and to the source disk's own zone when no region is configured. Must match the disk's zone."),
				"min_cpu":            intProp("Minimum vCPUs a VM needs to boot this image."),
				"min_ram":            intProp("Minimum RAM in GB."),
				"min_disk":           intProp("Minimum boot disk in GB — must be >= the image size, or VMs cannot be created from it."),
				"user_data_template": strProp("cloud-init template (Jinja) rendered when a VM is created from this image. Required for Marketplace images."),
				"image_metadata": map[string]any{
					"type":  "array",
					"items": map[string]any{"type": "object"},
					"description": "Fields the buyer fills in, substituted into user_data_template. Every {{ var }} in the template must be declared here. " +
						"Each entry: {name, display_name, display_position, field_type, default_value, sensitive, mandatory_group} — display_name and display_position are REQUIRED, and 'id'/'is_required' are rejected (the write schema differs from what image_get returns). " +
						"field_type uses the platform's typed values (public_key, linux_password, linux_login, hostname, flag); put public_key and linux_password in the same mandatory_group ('authentication') so either satisfies access.",
				},
				"confirmed": boolProp("Must be true — set only after the captain approved."),
			}, "name", "disk_id"),
		},
		{
			Name:        "fip_attach",
			Description: "Bind an existing FREE floating IP to a VM's primary interface, giving it a public address. Use when a VM has no public_ip — e.g. provisioning from a large custom image outran the wizard's wait and it returned before assigning one. Pick a free address from list_floating_ips. Mutation: requires confirmed=true.",
			InputSchema: objSchema(map[string]any{
				"fip":       strProp("Floating IP address (e.g. '213.171.24.210') or its UUID, from list_floating_ips."),
				"vm":        strProp("VM name or UUID to attach it to."),
				"confirmed": boolProp("Must be true — set only after the captain approved."),
			}, "fip", "vm"),
		},
		{
			Name:        "vm_power",
			Description: "Switch a VM on or off through the platform, without asking the guest OS. Use when a VM will not shut itself down — a cleanup script that deletes its own user and then calls `poweroff --no-block` can leave the machine running forever, stalling any step that waits for it to stop. Valid states: power_on, power_off, reboot. Mutation: requires confirmed=true.",
			InputSchema: objSchema(map[string]any{
				"vm_id":     strProp("VM UUID."),
				"state":     strProp("power_on, power_off or reboot."),
				"confirmed": boolProp("Must be true — set only after the captain approved."),
			}, "vm_id", "state"),
		},
		{
			Name:        "image_get",
			Description: "Get one VM image with its full spec — including the cloud-init user_data_template the platform renders when a VM is created from it, and the image_metadata fields a buyer fills in. Read-only. Use it to poll an image build until it reports state 'created', and to inspect how existing Marketplace images are structured.",
			InputSchema: objSchema(map[string]any{
				"image_id": strProp("Image UUID (from list_images)."),
			}, "image_id"),
		},
		{
			Name:        "image_delete",
			Description: "Delete a VM image. Running VMs keep working, but no new VM can be created from it. Mutation: requires confirmed=true.",
			InputSchema: objSchema(map[string]any{
				"image_id":  strProp("Image UUID (from list_images)."),
				"confirmed": boolProp("Must be true — set only after the captain approved."),
			}, "image_id"),
		},
		{
			Name:        "deploy_plan",
			Description: "STEP 1 of the deploy wizard (read-only, creates nothing). Resolves a full VM configuration — flavor, image, zone, disk, network, SSH — and returns it together with a MONTHLY COST estimate and a plan_id. In 'auto' mode pass the product's requirements (cpu/ram_gb/disk_gb from its README) and defaults fill the rest; in 'manual' mode pass explicit choices (flavor/zone/image/disk_type). Show the returned plan + cost to the captain for approval, then call deploy_apply with the plan_id.",
			InputSchema: objSchema(map[string]any{
				"base_plan_id": strProp("Optional: a prior plan_id to PATCH. Starts from that plan's parameters and overlays only the fields you pass here (e.g. just disk_gb), returning a fresh plan_id. Lets the captain tweak one value without restating everything."),
				"product":      strProp("Product/app name being deployed (used for the VM name and reporting)."),
				"mode":         strProp("'auto' (defaults + requirements) or 'manual' (explicit choices). Default: auto."),
				"cpu":          intProp("Required vCPUs (auto-size picks the smallest fitting flavor)."),
				"ram_gb":       intProp("Required RAM in GB (auto-size)."),
				"disk_gb":      intProp("Boot disk size in GB (default: 10)."),
				"ports":        map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "description": "Product ports to open in the VM's security group (e.g. [80,443] for a web server, from the README). deploy_apply opens them automatically."},
				"name":         strProp("Explicit VM name (default: <product>-<timestamp>)."),
				"compose_file": strProp("Path to a docker-compose.yml if this is a compose deploy (sets compose_required)."),
				"external_ip":  boolProp("Attach a public floating IP (default: true)."),
				"flavor":       strProp("manual: exact flavor name (see list_flavors)."),
				"zone":         strProp("manual: availability zone name or UUID (see list_zones)."),
				"image":        strProp("manual: image name or UUID (see list_images)."),
				"disk_type":    strProp("manual: disk type name, e.g. 'SSD' (see list_disk_types)."),
				"login":        strProp("Login user for the VM (default: user1)."),
				"ssh_key":      strProp("Full SSH public key to inject (e.g. 'ssh-ed25519 AAAA... user'). If omitted, a password is generated."),
			}),
		},
		{
			Name:        "deploy_apply",
			Description: "STEP 2 of the deploy wizard. Executes a plan previously returned by deploy_plan. REQUIRES the captain's approval: only call with confirmed=true AFTER the captain has explicitly approved the plan and its cost. Idempotent: if a VM with the plan's name already exists it is adopted, not duplicated. Returns vm_id, public_ip, ssh and (once) the generated password.",
			InputSchema: objSchema(map[string]any{
				"plan_id":   strProp("The plan_id returned by deploy_plan."),
				"confirmed": boolProp("Must be true — set only after the captain approved the plan."),
			}, "plan_id"),
		},
		{
			Name:        "list_flavors",
			Description: "List available Cloud.ru VM flavors (name, vCPU, RAM, free-tier flag) with a monthly cost estimate each. Use a flavor name as the 'flavor' argument for deploy_plan in manual mode.",
			InputSchema: objSchema(nil),
		},
		{
			Name:        "list_disk_types",
			Description: "List available Cloud.ru disk types (UUID + name, e.g. SSD/HDD). Use a name as the 'disk_type' argument for deploy_plan in manual mode.",
			InputSchema: objSchema(nil),
		},
		{
			Name:        "list_subnets",
			Description: "List Cloud.ru subnets (UUID, name, availability zone). Read-only.",
			InputSchema: objSchema(nil),
		},
		{
			Name:        "list_security_groups",
			Description: "List Cloud.ru security groups (UUID, name, availability zone). Read-only.",
			InputSchema: objSchema(nil),
		},
		{
			Name:        "list_floating_ips",
			Description: "List Cloud.ru floating IPs (address, state, whether available to attach). Read-only.",
			InputSchema: objSchema(nil),
		},
		{
			Name:        "open_ports",
			Description: "Open inbound ports on a Cloud.ru security group (add ingress rules, tcp, from 0.0.0.0/0 by default). Use to make a service reachable (e.g. 80,443 for a web server). Identify the group by 'security_group' (name or UUID). Skips ports already open. NOTE: security groups are shared per availability zone — opening a port affects every VM in that group. Mutation: requires confirmed=true after the captain approves.",
			InputSchema: objSchema(map[string]any{
				"security_group": strProp("Security group name (e.g. 'SSH-access_ru.AZ-2') or UUID (see list_security_groups)."),
				"ports":          map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "description": "Ports to open, e.g. [80,443]."},
				"protocol":       strProp("Protocol: tcp (default) or udp."),
				"cidr":           strProp("Source CIDR (default 0.0.0.0/0 = anywhere)."),
				"confirmed":      boolProp("Must be true — set only after the captain approved opening these ports."),
			}, "security_group"),
		},
		{
			Name:        "k8s_list",
			Description: "List Managed Kubernetes (mk8s) clusters in the project (id, name, status). Read-only.",
			InputSchema: objSchema(nil),
		},
		{
			Name:        "k8s_get",
			Description: "Get a Managed Kubernetes cluster by id (full details incl. status). Read-only.",
			InputSchema: objSchema(map[string]any{"cluster_id": strProp("Cluster UUID (from k8s_list).")}, "cluster_id"),
		},
		{
			Name:        "k8s_options",
			Description: "List available Kubernetes versions (by release channel), node flavors and zones for mk8s. Read-only. Use before k8s_plan in manual mode.",
			InputSchema: objSchema(nil),
		},
		{
			Name:        "k8s_plan",
			Description: "STEP 1 for Managed Kubernetes: resolve a cluster config (k8s version, control-plane flavor, master count, subnet, service account) WITHOUT creating anything. Show it to the captain, then call k8s_create with the same args + confirmed=true.",
			InputSchema: objSchema(map[string]any{
				"product":           strProp("What the cluster is for (used for the name)."),
				"name":              strProp("Cluster name (3-60; default k8s-<product>)."),
				"version":           strProp("Kubernetes version, e.g. v1.34.6 (default: newest STABLE; see k8s_options)."),
				"flavor":            strProp("Control-plane flavor name (see k8s_options); overrides cpu/ram_gb."),
				"cpu":               intProp("Control-plane vCPUs for auto flavor pick (default 2)."),
				"ram_gb":            intProp("Control-plane RAM GB for auto flavor pick (default 4)."),
				"master_count":      intProp("Control-plane node count: 1, 3 or 5 (default 1)."),
				"zone":              strProp("Availability zone name, e.g. ru.AZ-2 (default from productConfiguration)."),
				"subnet_id":         strProp("Subnet UUID for the control-plane private VIP (default: auto zone-matched)."),
				"cluster_sa_id":     strProp("Cluster service-account UUID (default: the agent's own service account)."),
				"kube_api_internet": boolProp("Publish kube-apiserver to the internet (default true)."),
			}),
		},
		{
			Name:        "k8s_create",
			Description: "STEP 2 for Managed Kubernetes: create the cluster resolved by k8s_plan. REQUIRES confirmed=true after the captain approves (billable). Async — returns cluster_id + operation_id; the cluster provisions in ~10-20 min (poll k8s_get). Accepts the same args as k8s_plan.",
			InputSchema: objSchema(map[string]any{
				"product": strProp("What the cluster is for."), "name": strProp("Cluster name."),
				"version": strProp("Kubernetes version."), "flavor": strProp("Control-plane flavor name."),
				"cpu": intProp("Control-plane vCPUs."), "ram_gb": intProp("Control-plane RAM GB."),
				"master_count": intProp("1, 3 or 5."), "zone": strProp("Zone name."),
				"subnet_id": strProp("Control-plane VIP subnet UUID."), "cluster_sa_id": strProp("Cluster service-account UUID."),
				"kube_api_internet": boolProp("Publish kube-apiserver to internet."),
				"confirmed":         boolProp("Must be true — set only after the captain approved."),
			}),
		},
		{
			Name:        "k8s_kubeconfig",
			Description: "Get the kubeconfig for a Managed Kubernetes cluster. By DEFAULT it embeds a static bearer token so kubectl/helm work as-is (the raw Cloud.ru kubeconfig needs a 'cloudlogin' exec-plugin that isn't installed → kubectl would fail 'executable cloudlogin not found'). Token TTL ~1h. Pass embed_token=false for the raw exec-plugin kubeconfig.",
			InputSchema: objSchema(map[string]any{
				"cluster_id":  strProp("Cluster UUID."),
				"network":     strProp("NETWORK_TYPE_PUBLIC or NETWORK_TYPE_PRIVATE (default: cluster default)."),
				"embed_token": boolProp("Embed a static bearer token so kubectl works without the cloudlogin plugin (default true)."),
			}, "cluster_id"),
		},
		{
			Name:        "k8s_delete",
			Description: "Permanently delete a Managed Kubernetes cluster. REQUIRES confirmed=true after the captain approves. Async.",
			InputSchema: objSchema(map[string]any{
				"cluster_id": strProp("Cluster UUID."),
				"confirmed":  boolProp("Must be true — set only after the captain approved."),
			}, "cluster_id"),
		},
		{
			Name:        "k8s_nodepools",
			Description: "List node pools of a Managed Kubernetes cluster (name, state, desired/ready nodes). Read-only.",
			InputSchema: objSchema(map[string]any{"cluster_id": strProp("Cluster UUID (filter).")}),
		},
		{
			Name:        "k8s_add_nodepool",
			Description: "Add a worker node pool to a Managed Kubernetes cluster (workers to run pods — a fresh cluster has none). REQUIRES confirmed=true after the captain approves (billable). Async. Auto-resolves flavor (cpu/ram or name), nodes subnet and security group. NB: Cloud.ru's node-pool backend occasionally returns 503 'no healthy upstream' (transient outage) — just retry.",
			InputSchema: objSchema(map[string]any{
				"cluster_id":        strProp("Cluster UUID (from k8s_list)."),
				"name":              strProp("Node pool name, 4-60 (default nodepool-1)."),
				"count":             intProp("Number of worker nodes (fixed scale, default 2)."),
				"flavor":            strProp("Node flavor name (see k8s_options); overrides cpu/ram_gb."),
				"cpu":               intProp("Node vCPUs for auto flavor (default 2)."),
				"ram_gb":            intProp("Node RAM GB for auto flavor (default 4)."),
				"disk_gb":           intProp("Node disk GB, min 10 (default 20)."),
				"zone":              strProp("Zone name (default from productConfiguration)."),
				"nodes_subnet_id":   strProp("Subnet UUID for worker nodes (default auto zone-matched)."),
				"security_group_id": strProp("Security group UUID for nodes (default auto zone-matched SSH-access)."),
				"confirmed":         boolProp("Must be true — set only after the captain approved."),
			}, "cluster_id"),
		},
		{
			Name:        "k8s_kubectl",
			Description: "Run ANY kubectl command against a cluster (auto-embedded token — no cloudlogin needed). Read verbs (get/describe/logs/top/events/'rollout status'/explain/…) run freely for inspection & verification. Mutating verbs (create/delete/patch/scale/label/'rollout restart'/exec/cordon/…) require confirmed=true. General escape hatch for whatever a product needs beyond helm/apply. Example args: \"get pods -A\", \"-n db logs deploy/postgres\", \"create secret generic s --from-literal=k=v\".",
			InputSchema: objSchema(map[string]any{
				"cluster_id": strProp("Cluster UUID."),
				"args":       strProp("kubectl arguments as a single string, e.g. \"get pods -A -o wide\"."),
				"confirmed":  boolProp("Required only for mutating verbs — set true after the captain approves. Read verbs need no confirmation."),
			}, "cluster_id", "args"),
		},
		{
			Name:        "k8s_apply",
			Description: "Apply a Kubernetes manifest (YAML/JSON, may be multi-document) to a cluster — like `kubectl apply -f -`. For deploying/updating raw workloads (Deployments, StatefulSets, Services, ServiceMonitors, etc.). Mutation: requires confirmed=true after the captain approves. NB: a fresh cluster needs egress (snat_create) to pull public images, and CRDs (e.g. ServiceMonitor) must exist before objects that use them.",
			InputSchema: objSchema(map[string]any{
				"cluster_id": strProp("Cluster UUID."),
				"manifest":   strProp("The manifest YAML/JSON to apply (multi-doc allowed)."),
				"namespace":  strProp("Namespace to apply into (optional; manifests may set their own)."),
				"confirmed":  boolProp("Must be true — set only after the captain approved."),
			}, "cluster_id", "manifest"),
		},
		{
			Name:        "k8s_helm",
			Description: "Drive Helm: action=install|upgrade|uninstall|list|status|test|lint|template. install/upgrade are idempotent (helm upgrade --install --wait). The chart comes either from a repo (chart='repo/name' + repo_url) or from this host (chart_path — a chart directory or a packaged .tgz), which is how a locally built chart is installed without publishing it anywhere. Values: values_file (path on disk) and/or values (inline YAML) and/or set (array of key=value); inline wins over the file. lint and template run LOCALLY and need no cluster_id. test runs the chart's own test pod (helm test --logs) — that is how you prove the product works rather than merely starts. Mutating actions (install/upgrade/uninstall/test) require confirmed=true. NB: nodes need egress (snat_create) to pull chart images; size the node pool for the chart (monitoring stacks want ≥8GB nodes).",
			InputSchema: objSchema(map[string]any{
				"cluster_id":      strProp("Cluster UUID. Required for everything except lint/template."),
				"action":          strProp("install | upgrade | uninstall | list | status | test | lint | template."),
				"release":         strProp("Helm release name (install/upgrade/uninstall/status/test; optional name for template)."),
				"chart":           strProp("Chart ref from a repo, e.g. 'prometheus-community/kube-prometheus-stack'."),
				"chart_path":      strProp("Path on this host to a chart directory or a packaged .tgz. Alternative to chart — pass one, not both."),
				"repo_url":        strProp("Chart repo URL to add+update first; repo name taken from the chart's '<repo>/' prefix. Ignored with chart_path."),
				"namespace":       strProp("Target namespace (created on install/upgrade)."),
				"version":         strProp("Chart version (optional; default latest)."),
				"values":          strProp("Inline values.yaml content (optional)."),
				"values_file":     strProp("Path on this host to a values file (optional; applied before inline values)."),
				"set":             map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Array of 'key=value' Helm --set overrides (optional)."},
				"kube_version":    strProp("Kubernetes version to render against, e.g. '1.31.0' (lint/template only)."),
				"timeout_seconds": intProp("Override helm's --timeout for install/upgrade/uninstall/test (default 480s install, 300s test)."),
				"wait":            boolProp("Wait for resources to become ready on install/upgrade/uninstall (default true)."),
				"confirmed":       boolProp("Must be true for install/upgrade/uninstall/test — set only after the captain approved."),
			}, "action"),
		},
		{
			Name:        "disk_list",
			Description: "List block-storage disks (id, name, size_gb, state, disk_type, zone, attached VMs). Read-only.",
			InputSchema: objSchema(map[string]any{}),
		},
		{
			Name:        "disk_create",
			Description: "Create a block-storage disk. Pass attach_to=<VM name/uuid> to create it in that VM's zone and (with confirmed) attach it in one step — disks must be in the SAME zone as the VM. NB: attaching adds a NEW block device; it does NOT grow the VM's existing filesystem (you then partition/format/mount it on the VM). Resizing the boot disk in place is NOT available via this API (console-only). Mutation (billable): requires confirmed=true.",
			InputSchema: objSchema(map[string]any{
				"name":      strProp("Disk name."),
				"size_gb":   intProp("Disk size in GB (required)."),
				"disk_type": strProp("Disk type name (e.g. from list_disk_types) or UUID. Default: an SSD type."),
				"attach_to": strProp("VM name or UUID to create-in-zone and auto-attach to (e.g. 'vm-712c44')."),
				"zone":      strProp("Availability zone name or UUID (alternative to attach_to; default: configured region)."),
				"confirmed": boolProp("Must be true — set only after the captain approved."),
			}, "name", "size_gb"),
		},
		{
			Name:        "disk_attach",
			Description: "Attach an existing disk to a VM (POST /disks/{id}/attach). Disk and VM must be in the same zone. Mutation: requires confirmed=true.",
			InputSchema: objSchema(map[string]any{
				"disk_id":   strProp("Disk UUID (from disk_list)."),
				"vm":        strProp("VM name or UUID."),
				"confirmed": boolProp("Must be true — set only after the captain approved."),
			}, "disk_id", "vm"),
		},
		{
			Name:        "disk_detach",
			Description: "Detach a disk from a VM (POST /disks/{id}/detach). Do NOT detach a bootable/in-use disk carelessly. Mutation: requires confirmed=true.",
			InputSchema: objSchema(map[string]any{
				"disk_id":   strProp("Disk UUID (from disk_list)."),
				"vm":        strProp("VM name or UUID the disk is attached to."),
				"confirmed": boolProp("Must be true — set only after the captain approved."),
			}, "disk_id", "vm"),
		},
		{
			Name:        "disk_resize",
			Description: "Grow a disk in place via PUT /disks/{id} (up to 65536 GB). GROW ONLY — the API rejects shrink and FREE-TIER disks (free_tier_disk_resize_is_prohibited). After resizing, grow the partition+filesystem on the VM (growpart + resize2fs). This is how you'd enlarge a VM's disk without a new volume — but a free-tier boot disk can't be resized (recreate the VM with a paid disk instead). Mutation: requires confirmed=true.",
			InputSchema: objSchema(map[string]any{
				"disk_id":   strProp("Disk UUID (from disk_list)."),
				"size_gb":   intProp("New size in GB (must be larger than current; max 65536)."),
				"confirmed": boolProp("Must be true — set only after the captain approved."),
			}, "disk_id", "size_gb"),
		},
		{
			Name:        "disk_delete",
			Description: "Permanently delete a disk and all its data (irreversible). Detach it first if attached. Mutation: requires confirmed=true.",
			InputSchema: objSchema(map[string]any{
				"disk_id":   strProp("Disk UUID (from disk_list)."),
				"confirmed": boolProp("Must be true — set only after the captain approved."),
			}, "disk_id"),
		},
		{
			Name:        "ca_list",
			Description: "List Container Apps (serverless containers) in the project (name, status). Read-only.",
			InputSchema: objSchema(nil),
		},
		{
			Name:        "ca_get",
			Description: "Get a Container App by name — full details incl. status and public URL. Read-only.",
			InputSchema: objSchema(map[string]any{"name": strProp("Container app name.")}, "name"),
		},
		{
			Name:        "ca_deploy",
			Description: "Deploy (create+run) a Container App from a container image — serverless, scales to zero when idle. REQUIRES confirmed=true after the captain approves. Async: returns operation_id; use ca_get for status + public URL. Ideal for stateless web services/APIs without managing a VM.",
			InputSchema: objSchema(map[string]any{
				"name":          strProp("Container app name (RFC1123, unique)."),
				"image":         strProp("Container image URI, e.g. docker.io/library/nginx:latest or helloworld.cr.cloud.ru/... ."),
				"cpu":           strProp("CPU request, e.g. '0.1', '1' (default 0.1)."),
				"memory":        strProp("Memory request, e.g. '256Mi', '1Gi' (default 256Mi)."),
				"port":          intProp("Container port the app listens on (default 8080)."),
				"public":        boolProp("Expose a public URL (default true)."),
				"min_instances": intProp("Minimum instances (default 0 = scale to zero)."),
				"max_instances": intProp("Maximum instances (default 1)."),
				"env":           map[string]any{"type": "object", "description": "Environment variables as a name→value object.", "additionalProperties": map[string]any{"type": "string"}},
				"description":   strProp("Optional description."),
				"confirmed":     boolProp("Must be true — set only after the captain approved."),
			}, "name", "image"),
		},
		{
			Name:        "ca_delete",
			Description: "Delete a Container App by name. REQUIRES confirmed=true after the captain approves. Async.",
			InputSchema: objSchema(map[string]any{"name": strProp("Container app name."), "confirmed": boolProp("Must be true.")}, "name"),
		},
		{
			Name:        "ca_logs",
			Description: "Get recent logs of a Container App (last 100 lines). Set system=true for platform/system events. Read-only.",
			InputSchema: objSchema(map[string]any{"name": strProp("Container app name."), "system": boolProp("System logs instead of app logs.")}, "name"),
		},
		{
			Name:        "ca_start",
			Description: "Start a stopped Container App.",
			InputSchema: objSchema(map[string]any{"name": strProp("Container app name.")}, "name"),
		},
		{
			Name:        "ca_stop",
			Description: "Stop a running Container App.",
			InputSchema: objSchema(map[string]any{"name": strProp("Container app name.")}, "name"),
		},
		{
			Name:        "iam_service_accounts",
			Description: "List IAM service accounts for a project (id, name, email, type, enabled). Read-only. Use this to find a cluster_sa_id for k8s_create, or to audit service accounts.",
			InputSchema: objSchema(map[string]any{"project_id": strProp("Project UUID (default: current project).")}),
		},
		{
			Name:        "org_projects",
			Description: "List Cloud.ru projects visible to the caller (id, name, status, org unit). Read-only.",
			InputSchema: objSchema(nil),
		},
		{
			Name:        "org_quotas",
			Description: "List quota usages (resource, limit, current usage) — for capacity planning. Read-only. Optionally scope to a project_id. (May be permission-limited depending on the service account's rights.)",
			InputSchema: objSchema(map[string]any{"project_id": strProp("Project UUID to scope quotas to (optional).")}),
		},
		{
			Name:        "release_floating_ip",
			Description: "Release (delete) a floating IP, freeing it from the project. Identify it by 'id' or 'ip_address'. REFUSES to release an IP that is currently in use by a VM (detach or destroy the VM first). Mutation: requires confirmed=true after the captain approves.",
			InputSchema: objSchema(map[string]any{
				"id":         strProp("Floating IP UUID (from list_floating_ips). Either id or ip_address is required."),
				"ip_address": strProp("Floating IP address, e.g. '213.171.25.19' (alternative to id)."),
				"confirmed":  boolProp("Must be true — set only after the captain approved releasing this IP."),
			}),
		},
		{
			Name:        "snat_list",
			Description: "List SNAT gateways in the project. A SNAT gateway gives private-subnet nodes (e.g. mk8s worker nodes) OUTBOUND internet access — needed to pull public container images (docker.io etc.), apt, or call external APIs. It is separate from a cluster's kubeApiInternet (which is inbound API-server access). Read-only.",
			InputSchema: objSchema(map[string]any{}),
		},
		{
			Name:        "snat_create",
			Description: "Create a SNAT gateway so a private subnet's nodes reach the internet (outbound egress). Zone-level: defaults to the configured region; override with 'zone' (name) or 'availability_zone_id'. Without confirmed=true this returns a preview only. Mutation (billable): requires confirmed=true after the captain approves. NOTE: this is NOT part of k8s_create — mk8s clusters come up without egress; create a SNAT gateway in the cluster's node zone if workloads need to pull public images.",
			InputSchema: objSchema(map[string]any{
				"name":                 strProp("Name for the SNAT gateway (required)."),
				"zone":                 strProp("Availability-zone NAME (e.g. 'ru.AZ-2'). Optional; resolved to a UUID. Defaults to the configured region."),
				"availability_zone_id": strProp("Availability-zone UUID (alternative to 'zone')."),
				"confirmed":            boolProp("Must be true to actually create — set only after the captain approved. Omit for a preview."),
			}),
		},
		{
			Name:        "snat_delete",
			Description: "Delete a SNAT gateway by id (from snat_list). Nodes in that zone lose outbound internet. Mutation: requires confirmed=true after the captain approves.",
			InputSchema: objSchema(map[string]any{
				"id":        strProp("SNAT gateway UUID (from snat_list). Required."),
				"confirmed": boolProp("Must be true — set only after the captain approved deleting this gateway."),
			}),
		},
		{
			Name:        "pg_list",
			Description: "List Managed PostgreSQL clusters in the project (id, name, status, version, health). Read-only. Managed PostgreSQL is Cloud.ru's managed database service (postgresql.api.cloud.ru).",
			InputSchema: objSchema(map[string]any{}),
		},
		{
			Name:        "pg_get",
			Description: "Get full details of one Managed PostgreSQL cluster by id (from pg_list): version, instances, storage, connection_string, subnets, status. Read-only.",
			InputSchema: objSchema(map[string]any{
				"id":         strProp("PostgreSQL cluster UUID (from pg_list). Required (alias: cluster_id)."),
				"cluster_id": strProp("Alias for id."),
			}),
		},
		{
			Name:        "pg_versions",
			Description: "List available Managed PostgreSQL versions (e.g. 15/16/17). Read-only. Use before pg_create.",
			InputSchema: objSchema(map[string]any{}),
		},
		{
			Name:        "pg_specs",
			Description: "List Managed PostgreSQL specifications (id, cpu, memory_gb, deployment_mode standard|business, min_storage_gb) for a version. Read-only. Use to choose specification_id for pg_create.",
			InputSchema: objSchema(map[string]any{
				"version": strProp("Product version (e.g. '17'); optional — filters specs to that version."),
			}),
		},
		{
			Name:        "pg_create",
			Description: "Create a Managed PostgreSQL cluster. Resolves version (default: latest), specification (by specification_id OR cpu+ram), storage (default: spec minimum), and subnet (default: first project subnet). Without confirmed=true returns a preview only. Mutation (BILLABLE, async ~minutes): requires confirmed=true after the captain approves. Poll the returned operation_id with pg_operation.",
			InputSchema: objSchema(map[string]any{
				"name":                  strProp("Cluster name (required)."),
				"version":               strProp("PostgreSQL version (from pg_versions). Optional; default = latest."),
				"specification_id":      strProp("Specification UUID (from pg_specs). Optional if cpu+ram given."),
				"cpu":                   intProp("vCPU count — used to auto-pick a specification when specification_id is omitted."),
				"ram":                   intProp("RAM in GB — used with cpu to auto-pick a specification."),
				"storage_gb":            intProp("Main data disk size in GB. Optional; default = specification minimum."),
				"instances":             intProp("Number of nodes (1-2 standard, 1-3 business). Default 1."),
				"subnet_id":             strProp("Subnet UUID (defines the AZ). Optional; default = first project subnet."),
				"initial_database":      strProp("Name of the default database to create. Default 'defaultdb'."),
				"description":           strProp("Optional cluster description."),
				"no_backup":             boolProp("Disable automatic backups. Backups are ENABLED by default and CANNOT be toggled after creation; a cluster without backups rejects pg_backup_create/pg_backups with 'backup disabled'."),
				"backup_schedule":       strProp("CRON schedule for automatic backups (default '0 3 * * 0' = weekly Sun 03:00; min once per 6h)."),
				"backup_retention_days": intProp("Retention for automatic backups in days (default 14, max 30)."),
				"confirmed":             boolProp("Must be true to actually create — set only after the captain approved. Omit for a preview."),
			}, "name"),
		},
		{
			Name:        "pg_delete",
			Description: "Delete a Managed PostgreSQL cluster by id (from pg_list) AND its data. Mutation (async): requires confirmed=true after the captain approves.",
			InputSchema: objSchema(map[string]any{
				"id":         strProp("Cluster UUID (from pg_list). Required (alias: cluster_id)."),
				"cluster_id": strProp("Alias for id."),
				"confirmed":  boolProp("Must be true — set only after the captain approved deleting this cluster."),
			}),
		},
		{
			Name:        "pg_operation",
			Description: "Poll a Managed PostgreSQL async operation by id (returned by pg_create/pg_delete/pg_backup_create). Returns {done, error}. Read-only.",
			InputSchema: objSchema(map[string]any{
				"operation_id": strProp("Operation UUID. Required."),
				"wait_seconds": intProp("Optional: poll up to N seconds (capped 300) with backoff and return the FINAL state in ONE call — instead of the agent re-checking every few seconds. Omit for a single instant check."),
			}, "operation_id"),
		},
		{
			Name:        "kafka_list",
			Description: "List Managed Kafka clusters (id, name, status, brokers, version). Read-only. kafka.api.cloud.ru.",
			InputSchema: objSchema(map[string]any{}),
		},
		{
			Name:        "kafka_get",
			Description: "Get a Managed Kafka cluster by id (status, kafka_hosts, brokers, version). Read-only.",
			InputSchema: objSchema(map[string]any{"cluster_id": strProp("Cluster UUID (from kafka_list). Required.")}, "cluster_id"),
		},
		{
			Name:        "kafka_versions",
			Description: "List available Managed Kafka versions (id + name). Read-only. Use before kafka_create.",
			InputSchema: objSchema(map[string]any{}),
		},
		{
			Name:        "kafka_specs",
			Description: "List Managed Kafka flavor specifications (id, cpu, memory_gb, min_storage_gb, min/max_brokers) for a version. Read-only. Use to choose specification_id for kafka_create.",
			InputSchema: objSchema(map[string]any{"version_id": strProp("Version UUID (from kafka_versions); optional — defaults to latest.")}),
		},
		{
			Name:        "kafka_create",
			Description: "Create a Managed Kafka cluster. Resolves version (latest), specification (by specification_id or cpu+ram), subnet (first project subnet), storage & brokers (spec minimum). Without confirmed=true returns a preview. Mutation (BILLABLE, async): confirmed=true after the captain approves; poll kafka_operation.",
			InputSchema: objSchema(map[string]any{
				"name":             strProp("Cluster name (2-63 chars). Required."),
				"version_id":       strProp("Kafka version UUID (from kafka_versions). Optional; default = latest."),
				"specification_id": strProp("Specification UUID (from kafka_specs). Optional if cpu+ram given."),
				"cpu":              intProp("vCPU — auto-picks a spec when specification_id omitted."),
				"ram":              intProp("RAM GB — with cpu."),
				"storage_gib":      intProp("Disk size GiB. Optional; default = spec minimum."),
				"brokers":          intProp("Broker count. Optional; default = spec minimum."),
				"ui_enabled":       boolProp("Enable the Kafka UI (default false)."),
				"description":      strProp("Optional description."),
				"confirmed":        boolProp("Must be true to create — set only after the captain approved. Omit for preview."),
			}, "name"),
		},
		{
			Name:        "kafka_delete",
			Description: "Delete a Managed Kafka cluster by id AND its data. Mutation (async): requires confirmed=true after the captain approves.",
			InputSchema: objSchema(map[string]any{
				"cluster_id": strProp("Cluster UUID (from kafka_list). Required."),
				"confirmed":  boolProp("Must be true — set only after the captain approved."),
			}, "cluster_id"),
		},
		{
			Name:        "kafka_operation",
			Description: "Poll a Managed Kafka async operation by id (from kafka_create/kafka_delete). Returns {done, error}. Read-only.",
			InputSchema: objSchema(map[string]any{"operation_id": strProp("Operation UUID. Required."), "wait_seconds": intProp("Optional: poll up to N seconds (capped 300) with backoff and return the FINAL state in ONE call — instead of re-checking every few seconds. Omit for a single instant check.")}, "operation_id"),
		},
		{
			Name:        "redis_list",
			Description: "List Managed Redis clusters (id, name, status, type, shards, replicas). Read-only. redis.api.cloud.ru.",
			InputSchema: objSchema(map[string]any{}),
		},
		{
			Name:        "redis_get",
			Description: "Get a Managed Redis cluster by id (status, redis_hosts, type, shards, replicas). Read-only.",
			InputSchema: objSchema(map[string]any{"cluster_id": strProp("Cluster UUID (from redis_list). Required.")}, "cluster_id"),
		},
		{
			Name:        "redis_versions",
			Description: "List available Managed Redis versions (id + name). Read-only. Use before redis_create.",
			InputSchema: objSchema(map[string]any{}),
		},
		{
			Name:        "redis_specs",
			Description: "List Managed Redis flavor specifications (id, cpu, memory_gb, deployment_mode, min_storage_gb) for a version. Read-only. Use to choose specification_id for redis_create.",
			InputSchema: objSchema(map[string]any{"version_id": strProp("Version UUID (from redis_versions); optional — defaults to latest.")}),
		},
		{
			Name:        "redis_create",
			Description: "Create a Managed Redis cluster. Resolves version (latest), specification (by specification_id or cpu+ram), subnet. type STANDALONE (default) or MASTER_REPLICA. Without confirmed=true returns a preview. Mutation (BILLABLE, async): confirmed=true after the captain approves; poll redis_operation.",
			InputSchema: objSchema(map[string]any{
				"name":             strProp("Cluster name. Required."),
				"version_id":       strProp("Redis version UUID (from redis_versions). Optional; default = latest."),
				"specification_id": strProp("Specification UUID (from redis_specs). Optional if cpu+ram given."),
				"cpu":              intProp("vCPU — auto-picks a spec when specification_id omitted."),
				"ram":              intProp("RAM GB — with cpu."),
				"storage_gb":       intProp("Disk size GB. Optional; default = spec minimum."),
				"shards":           intProp("Shard count 1-8 (default 1)."),
				"replicas":         intProp("Replica count (default 0)."),
				"type":             strProp("STANDALONE (default) or MASTER_REPLICA."),
				"description":      strProp("Optional description."),
				"confirmed":        boolProp("Must be true to create — set only after the captain approved. Omit for preview."),
			}, "name"),
		},
		{
			Name:        "redis_delete",
			Description: "Delete a Managed Redis cluster by id AND its data. Mutation (async): requires confirmed=true after the captain approves.",
			InputSchema: objSchema(map[string]any{
				"cluster_id": strProp("Cluster UUID (from redis_list). Required."),
				"confirmed":  boolProp("Must be true — set only after the captain approved."),
			}, "cluster_id"),
		},
		{
			Name:        "redis_operation",
			Description: "Poll a Managed Redis async operation by id (from redis_create/redis_delete). Returns {done, error}. Read-only.",
			InputSchema: objSchema(map[string]any{"operation_id": strProp("Operation UUID. Required."), "wait_seconds": intProp("Optional: poll up to N seconds (capped 300) with backoff and return the FINAL state in ONE call — instead of re-checking every few seconds. Omit for a single instant check.")}, "operation_id"),
		},
		{
			Name:        "pg_databases",
			Description: "List databases in a PostgreSQL cluster (name, owner, size). Read-only.",
			InputSchema: objSchema(map[string]any{
				"cluster_id": strProp("Cluster UUID (from pg_list). Required."),
			}, "cluster_id"),
		},
		{
			Name:        "pg_db_create",
			Description: "Create a database in a PostgreSQL cluster. Mutation: requires confirmed=true after the captain approves.",
			InputSchema: objSchema(map[string]any{
				"cluster_id": strProp("Cluster UUID. Required."),
				"name":       strProp("Database name. Required."),
				"owner":      strProp("Owning user (from pg_users). Required."),
				"lc_collate": strProp("Collation locale (optional, default C)."),
				"lc_ctype":   strProp("Character-set locale (optional, default C)."),
				"confirmed":  boolProp("Must be true — set only after the captain approved."),
			}, "cluster_id", "name", "owner"),
		},
		{
			Name:        "pg_db_delete",
			Description: "Drop a database in a PostgreSQL cluster. Mutation: requires confirmed=true after the captain approves.",
			InputSchema: objSchema(map[string]any{
				"cluster_id": strProp("Cluster UUID. Required."),
				"name":       strProp("Database name. Required."),
				"confirmed":  boolProp("Must be true — set only after the captain approved."),
			}, "cluster_id", "name"),
		},
		{
			Name:        "pg_users",
			Description: "List users of a PostgreSQL cluster (name, granted roles). Read-only.",
			InputSchema: objSchema(map[string]any{
				"cluster_id": strProp("Cluster UUID (from pg_list). Required."),
			}, "cluster_id"),
		},
		{
			Name:        "pg_user_create",
			Description: "Create a user in a PostgreSQL cluster. Mutation: requires confirmed=true after the captain approves. The password is never echoed back.",
			InputSchema: objSchema(map[string]any{
				"cluster_id": strProp("Cluster UUID. Required."),
				"name":       strProp("User name. Required."),
				"password":   strProp("User password. Required."),
				"confirmed":  boolProp("Must be true — set only after the captain approved."),
			}, "cluster_id", "name", "password"),
		},
		{
			Name:        "pg_user_delete",
			Description: "Delete a user in a PostgreSQL cluster. Mutation: requires confirmed=true after the captain approves.",
			InputSchema: objSchema(map[string]any{
				"cluster_id": strProp("Cluster UUID. Required."),
				"name":       strProp("User name. Required."),
				"confirmed":  boolProp("Must be true — set only after the captain approved."),
			}, "cluster_id", "name"),
		},
		{
			Name:        "pg_backups",
			Description: "List backups of a PostgreSQL cluster (name, status, size, timestamps). Read-only.",
			InputSchema: objSchema(map[string]any{
				"cluster_id": strProp("Cluster UUID (from pg_list). Required."),
			}, "cluster_id"),
		},
		{
			Name:        "pg_backup_create",
			Description: "Start a manual backup of a PostgreSQL cluster (async). Mutation: requires confirmed=true after the captain approves.",
			InputSchema: objSchema(map[string]any{
				"cluster_id": strProp("Cluster UUID. Required."),
				"name":       strProp("Backup name (letters/digits/hyphens/dots; not starting/ending with - or .). Required."),
				"confirmed":  boolProp("Must be true — set only after the captain approved."),
			}, "cluster_id", "name"),
		},
		{
			Name:        "lb_list",
			Description: "List Evolution Load Balancers (L4/network) in the project (id, name, status, vpc, external_ip). Read-only. nlb.api.cloud.ru.",
			InputSchema: objSchema(map[string]any{}),
		},
		{
			Name:        "lb_get",
			Description: "Get full details of one load balancer by id (from lb_list): rules, listeners, addresses, AZs, status. Read-only.",
			InputSchema: objSchema(map[string]any{"id": strProp("Load balancer UUID (from lb_list). Required.")}, "id"),
		},
		{
			Name:        "lb_operation",
			Description: "Poll a Load Balancer async operation by id (from lb_create/lb_delete/tg_create/tg_delete). Returns {done, error}. Read-only.",
			InputSchema: objSchema(map[string]any{"operation_id": strProp("Operation UUID. Required."), "wait_seconds": intProp("Optional: poll up to N seconds (capped 300) with backoff and return the FINAL state in ONE call — instead of re-checking every few seconds. Omit for a single instant check.")}, "operation_id"),
		},
		{
			Name:        "tg_list",
			Description: "List Load Balancer target groups in the project (id, name, status, vpc). Read-only.",
			InputSchema: objSchema(map[string]any{}),
		},
		{
			Name:        "tg_get",
			Description: "Get details of one target group by id (from tg_list): member VMs, vpc, status. Read-only.",
			InputSchema: objSchema(map[string]any{"id": strProp("Target group UUID (from tg_list). Required.")}, "id"),
		},
		{
			Name:        "tg_create",
			Description: "Create a target group (the set of VMs a load balancer distributes traffic to). Provide targets as just vm_id (single) or a 'targets' array of {id}. The VM's network interface is AUTO-RESOLVED — you do NOT need interface_id (only pass it to override). Targets must be private VPC-subnet VMs (provision with no_public_ip). Mutation (async): requires confirmed=true after the captain approves.",
			InputSchema: objSchema(map[string]any{
				"name":         strProp("Target group name (unique in project). Required."),
				"vm_id":        strProp("VM UUID for a single target. The interface is auto-resolved."),
				"interface_id": strProp("Optional: network interface UUID (auto-resolved from the VM if omitted)."),
				"targets":      map[string]any{"type": "array", "description": "Multiple targets: array of {id} (interface_id optional, auto-resolved).", "items": map[string]any{"type": "object"}},
				"confirmed":    boolProp("Must be true — set only after the captain approved."),
			}, "name"),
		},
		{
			Name:        "tg_delete",
			Description: "Delete a target group by id (from tg_list). Mutation (async): requires confirmed=true after the captain approves.",
			InputSchema: objSchema(map[string]any{"id": strProp("Target group UUID. Required."), "confirmed": boolProp("Must be true — set only after the captain approved.")}, "id"),
		},
		{
			Name:        "lb_create",
			Description: "Create an L4 load balancer fronting a target group. Resolves vpc_id (from the target group), subnet (first project subnet) and builds a rule with a listener (port→target_port) + TCP health check. external=true (default) allocates a public IP. Without confirmed=true returns a preview only. Mutation (BILLABLE, async): requires confirmed=true. Poll lb_operation.",
			InputSchema: objSchema(map[string]any{
				"name":            strProp("Load balancer name. Required."),
				"target_group_id": strProp("Target group UUID (from tg_list/tg_create) to balance traffic to. Required."),
				"port":            intProp("Listener port the balancer accepts traffic on. Required."),
				"target_port":     intProp("Port on the targets to forward to. Default = port."),
				"hc_port":         intProp("Health-check port. Default = target_port."),
				"external":        boolProp("Allocate a public IP (default true). Set false for an internal-only balancer."),
				"vpc_id":          strProp("VPC UUID. Optional; default = the target group's VPC."),
				"subnet_id":       strProp("Subnet UUID for the balancer's AZ. Optional; default = first project subnet."),
				"confirmed":       boolProp("Must be true to actually create — set only after the captain approved. Omit for a preview."),
			}, "name", "target_group_id", "port"),
		},
		{
			Name:        "lb_delete",
			Description: "Delete a load balancer by id (from lb_list). Mutation (async): requires confirmed=true after the captain approves.",
			InputSchema: objSchema(map[string]any{"id": strProp("Load balancer UUID. Required."), "confirmed": boolProp("Must be true — set only after the captain approved.")}, "id"),
		},
		{
			Name:        "vpc_list",
			Description: "List Evolution VPCs (virtual private clouds) in the project (id, name, default, type). Read-only. vpc.api.cloud.ru.",
			InputSchema: objSchema(map[string]any{}),
		},
		{
			Name:        "vpc_get",
			Description: "Get full details of one VPC by id (from vpc_list). Read-only.",
			InputSchema: objSchema(map[string]any{"id": strProp("VPC UUID (from vpc_list). Required.")}, "id"),
		},
		{
			Name:        "vpc_create",
			Description: "Create a new VPC (virtual private cloud) (async). Mutation: requires confirmed=true after the captain approves.",
			InputSchema: objSchema(map[string]any{
				"name":        strProp("VPC name. Required."),
				"description": strProp("Optional description."),
				"confirmed":   boolProp("Must be true — set only after the captain approved."),
			}, "name"),
		},
		{
			Name:        "vpc_delete",
			Description: "Delete a VPC by id (from vpc_list) (async). Its subnets/resources must be removed first. Mutation: requires confirmed=true after the captain approves.",
			InputSchema: objSchema(map[string]any{
				"id":        strProp("VPC UUID (from vpc_list). Required."),
				"confirmed": boolProp("Must be true — set only after the captain approved."),
			}, "id"),
		},
		{
			Name:        "vpc_operation",
			Description: "Poll a VPC async operation by id (from vpc_create/delete, vpc_route_create/delete). Returns {done, error}. Read-only.",
			InputSchema: objSchema(map[string]any{"operation_id": strProp("Operation UUID. Required."), "wait_seconds": intProp("Optional: poll up to N seconds (capped 300) with backoff and return the FINAL state in ONE call — instead of re-checking every few seconds. Omit for a single instant check.")}, "operation_id"),
		},
		{
			Name:        "vpc_routes",
			Description: "List static routes of a VPC (id, subnet, next_hop_type, status). Read-only.",
			InputSchema: objSchema(map[string]any{"vpc_id": strProp("VPC UUID (from vpc_list). Required.")}, "vpc_id"),
		},
		{
			Name:        "vpc_route_create",
			Description: "Add a static route to a VPC (async). Destination is a CIDR (subnet); the next hop is a VM interface (next_hop_vm_id+next_hop_interface_id), a VIP (next_hop_vip_id), or a Magic Router (next_hop_magic_router_id+next_hop_az). Mutation: requires confirmed=true after the captain approves.",
			InputSchema: objSchema(map[string]any{
				"vpc_id":                   strProp("VPC UUID. Required."),
				"subnet":                   strProp("Destination CIDR, e.g. '192.168.0.0/24'. Required."),
				"next_hop_vm_id":           strProp("VM UUID for a VM-interface next hop (with next_hop_interface_id)."),
				"next_hop_interface_id":    strProp("VM network interface UUID."),
				"next_hop_vip_id":          strProp("Virtual IP UUID for a VIP next hop."),
				"next_hop_magic_router_id": strProp("Magic Router UUID for a Magic-Router next hop (with next_hop_az)."),
				"next_hop_az":              strProp("Availability zone name (e.g. 'ru.AZ-1') for a Magic-Router next hop."),
				"description":              strProp("Optional route description."),
				"confirmed":                boolProp("Must be true — set only after the captain approved."),
			}, "vpc_id", "subnet"),
		},
		{
			Name:        "vpc_route_delete",
			Description: "Delete a static route from a VPC by id (from vpc_routes) (async). Mutation: requires confirmed=true after the captain approves.",
			InputSchema: objSchema(map[string]any{
				"vpc_id":    strProp("VPC UUID. Required."),
				"route_id":  strProp("Static route UUID (from vpc_routes). Required."),
				"confirmed": boolProp("Must be true — set only after the captain approved."),
			}, "vpc_id", "route_id"),
		},
		{
			Name:        "dns_zones",
			Description: "List Evolution DNS zones in the project (both public and private), with id, name, type, valid, delegated. Read-only. dns.api.cloud.ru.",
			InputSchema: objSchema(map[string]any{}),
		},
		{
			Name:        "dns_zone_get",
			Description: "Get full details of one DNS zone by id (from dns_zones). Read-only.",
			InputSchema: objSchema(map[string]any{
				"id":   strProp("Zone UUID (from dns_zones). Required."),
				"type": strProp("Zone visibility: 'public' (default) or 'private' — must match the zone."),
			}, "id"),
		},
		{
			Name:        "dns_zone_create",
			Description: "Create a DNS zone. type=public (default) or private. A public zone needs domain-ownership delegation (returns a validation record) to serve traffic; a private zone is internal. Mutation: requires confirmed=true after the captain approves.",
			InputSchema: objSchema(map[string]any{
				"domain":      strProp("Zone FQDN, e.g. 'example.com.' (required)."),
				"type":        strProp("Zone visibility: 'public' (default) or 'private'."),
				"description": strProp("Optional zone description."),
				"confirmed":   boolProp("Must be true — set only after the captain approved."),
			}, "domain"),
		},
		{
			Name:        "dns_zone_delete",
			Description: "Delete a DNS zone by id (from dns_zones) AND its records (async). type must match the zone (public|private). Mutation: requires confirmed=true after the captain approves.",
			InputSchema: objSchema(map[string]any{
				"id":        strProp("Zone UUID (from dns_zones). Required."),
				"type":      strProp("Zone visibility: 'public' (default) or 'private' — must match the zone."),
				"confirmed": boolProp("Must be true — set only after the captain approved."),
			}, "id"),
		},
		{
			Name:        "dns_operation",
			Description: "Poll a DNS async operation by id (from dns_zone_create/delete, dns_record_create/delete). Returns {done, error}. Read-only.",
			InputSchema: objSchema(map[string]any{"operation_id": strProp("Operation UUID. Required."), "wait_seconds": intProp("Optional: poll up to N seconds (capped 300) with backoff and return the FINAL state in ONE call — instead of re-checking every few seconds. Omit for a single instant check.")}, "operation_id"),
		},
		{
			Name:        "dns_records",
			Description: "List resource records of a DNS zone (id, name, type, values, ttl). Read-only. Pass the zone's type (public|private).",
			InputSchema: objSchema(map[string]any{
				"zone_id": strProp("Zone UUID (from dns_zones). Required."),
				"type":    strProp("Zone visibility: 'public' (default) or 'private' — must match the zone."),
			}, "zone_id"),
		},
		{
			Name:        "dns_record_get",
			Description: "Get one DNS record by id (from dns_records). Read-only. Pass the zone's type (public|private).",
			InputSchema: objSchema(map[string]any{
				"record_id": strProp("Record UUID (from dns_records). Required."),
				"type":      strProp("Zone visibility: 'public' (default) or 'private'."),
			}, "record_id"),
		},
		{
			Name:        "dns_record_create",
			Description: "Add a DNS record to a zone (async). Mutation: requires confirmed=true after the captain approves.",
			InputSchema: objSchema(map[string]any{
				"zone_id":     strProp("Zone UUID (from dns_zones). Required."),
				"name":        strProp("Record name/host (e.g. 'www' or '@'). Required."),
				"record_type": strProp("Record type: a, aaaa, cname, mx, ns, spf, txt, caa, srv. Required."),
				"value":       strProp("Single record value (e.g. an IP for A). Use this or 'values'."),
				"values":      map[string]any{"type": "array", "description": "Multiple record values.", "items": map[string]any{"type": "string"}},
				"ttl":         intProp("TTL seconds (default 300)."),
				"type":        strProp("Zone visibility: 'public' (default) or 'private'."),
				"confirmed":   boolProp("Must be true — set only after the captain approved."),
			}, "zone_id", "name", "record_type"),
		},
		{
			Name:        "dns_record_delete",
			Description: "Delete a DNS record by id (from dns_records) (async). Mutation: requires confirmed=true after the captain approves.",
			InputSchema: objSchema(map[string]any{
				"record_id": strProp("Record UUID (from dns_records). Required."),
				"type":      strProp("Zone visibility: 'public' (default) or 'private'."),
				"confirmed": boolProp("Must be true — set only after the captain approved."),
			}, "record_id"),
		},
		{
			Name:        "secret_list",
			Description: "List secrets in the project (id, name, path, description) from Cloud.ru Secret Management. Read-only — does NOT return values. secretmanager.api.cloud.ru (v2).",
			InputSchema: objSchema(map[string]any{}),
		},
		{
			Name:        "secret_get",
			Description: "Get one secret's metadata by path (from secret_list): folder, labels, versions, KMS key. Read-only — does NOT return the value.",
			InputSchema: objSchema(map[string]any{"path": strProp("Secret path (folder/name, from secret_list). Required.")}, "path"),
		},
		{
			Name:        "secret_value",
			Description: "Retrieve the DECRYPTED value of a secret by path. Sensitive — returns the plaintext value; never store it in logs or files. Read-only.",
			InputSchema: objSchema(map[string]any{
				"path":    strProp("Secret path (folder/name). Required."),
				"version": intProp("Specific version number (default: latest)."),
			}, "path"),
		},
		{
			Name:        "secret_create",
			Description: "Store a new secret at a path (value is Base64-encoded internally; never echoed back). Path 'folder/name' places it in a folder. Mutation: requires confirmed=true after the captain approves.",
			InputSchema: objSchema(map[string]any{
				"path":        strProp("Secret path: 'name' at root, or 'folder/name'. Required."),
				"value":       strProp("Secret value (plaintext; stored encrypted). Required."),
				"description": strProp("Optional description."),
				"kms_key_id":  strProp("Optional KMS key id for encryption."),
				"confirmed":   boolProp("Must be true — set only after the captain approved."),
			}, "path", "value"),
		},
		{
			Name:        "secret_add_version",
			Description: "Add a new value version to an existing secret (rotation). Mutation: requires confirmed=true after the captain approves.",
			InputSchema: objSchema(map[string]any{
				"path":      strProp("Secret path (from secret_list). Required."),
				"value":     strProp("New secret value (plaintext). Required."),
				"confirmed": boolProp("Must be true — set only after the captain approved."),
			}, "path", "value"),
		},
		{
			Name:        "secret_delete",
			Description: "Delete a secret by path AND all its versions. Set force=true to delete one that is in use. Mutation: requires confirmed=true after the captain approves.",
			InputSchema: objSchema(map[string]any{
				"path":      strProp("Secret path (from secret_list). Required."),
				"force":     boolProp("Delete even if the secret is in use by other resources."),
				"confirmed": boolProp("Must be true — set only after the captain approved."),
			}, "path"),
		},
		{
			Name:        "ar_list",
			Description: "List Artifact Registry registries in the project (id, name, type, status, public, tariff). Read-only. ar.api.cloud.ru. A registry holds Docker images / Helm charts (the image source for Container Apps / mk8s that docker.io can't provide).",
			InputSchema: objSchema(map[string]any{}),
		},
		{
			Name:        "ar_get",
			Description: "Get full details of one Artifact Registry registry by id (from ar_list). Read-only.",
			InputSchema: objSchema(map[string]any{"id": strProp("Registry UUID (from ar_list). Required.")}, "id"),
		},
		{
			Name:        "ar_create",
			Description: "Create an Artifact Registry registry (async). type=DOCKER (default), DEBIAN, RPM, GENERIC, or PYPI. Mutation (BILLABLE): requires confirmed=true after the captain approves. Poll ar_operation; then push images via docker login to <registry>.cr.cloud.ru.",
			InputSchema: objSchema(map[string]any{
				"name":          strProp("Registry name. Required."),
				"registry_type": strProp("Registry type: DOCKER (default), DEBIAN, RPM, GENERIC, PYPI."),
				"public":        boolProp("Make the registry public (default false)."),
				"tariff":        strProp("Tariff: BASIC (default) or PREMIUM."),
				"confirmed":     boolProp("Must be true — set only after the captain approved."),
			}, "name"),
		},
		{
			Name:        "ar_delete",
			Description: "Delete an Artifact Registry registry by id (from ar_list) AND its artifacts (async). Mutation: requires confirmed=true after the captain approves.",
			InputSchema: objSchema(map[string]any{
				"id":        strProp("Registry UUID (from ar_list). Required."),
				"confirmed": boolProp("Must be true — set only after the captain approved."),
			}, "id"),
		},
		{
			Name:        "ar_operation",
			Description: "Poll an Artifact Registry async operation by id (from ar_create/ar_delete). Returns {done, error}. Read-only.",
			InputSchema: objSchema(map[string]any{"operation_id": strProp("Operation UUID. Required."), "wait_seconds": intProp("Optional: poll up to N seconds (capped 300) with backoff and return the FINAL state in ONE call — instead of re-checking every few seconds. Omit for a single instant check.")}, "operation_id"),
		},
		{
			Name:        "billing_balance",
			Description: "Account balance for the billing agreement (RUB, active flag, raw agreement object). Read-only. billing.api.cloud.ru/v1/agreement/{agreement_id}. Use to check the tenant's remaining runway.",
			InputSchema: objSchema(map[string]any{}),
		},
		{
			Name:        "billing_consumption",
			Description: "Usage/consumption cost for a date range, aggregated by service (total, total_with_nds, records, by_service breakdown sorted by cost). Read-only. organization.api.cloud.ru/v2/consumption (paginated). Use for FinOps/cost analysis.",
			InputSchema: objSchema(map[string]any{
				"start": strProp("Start date YYYY-MM-DD. Required."),
				"end":   strProp("End date YYYY-MM-DD (default = start)."),
			}, "start"),
		},
		{
			Name:        "billing_finops",
			Description: "FinOps report (read-only): INFORM (cost allocation by service, anomalies vs the previous same-length period) + QUANTIFY (daily burn-rate, balance runway in days, projected zero date, optional budget check). Ported from finops_report.py. Use for cost analysis, runway warnings, budget checks.",
			InputSchema: objSchema(map[string]any{
				"start":  strProp("Start date YYYY-MM-DD. Optional; default = end - days + 1."),
				"end":    strProp("End date YYYY-MM-DD. Optional; default = today."),
				"days":   intProp("Window length if start omitted (default 30)."),
				"budget": intProp("Monthly budget in RUB → projected-monthly threshold check (optional)."),
			}),
		},
		{
			Name:        "metrics_history",
			Description: "Service Monitoring — read a metric's history over a recent window (read-only). Prefer THIS over polling vm_get/k8s_get to answer 'is the service healthy / how's CPU/load'. Returns time series {metric labels, values, timestamps}. NB: the metric only returns data if the project has a monitoring tenant provisioned; otherwise the API says 'tenant not found'.",
			InputSchema: objSchema(map[string]any{
				"match": strProp("Metric name / PromQL name selector (e.g. cpu_usage, node_load1). Required."),
				"since": strProp("Look-back window: 30m, 1h, 6h, 2d… (default 1h, max 7d)."),
			}, "match"),
		},
		{
			Name:        "metrics_scrape",
			Description: "Service Monitoring — current metric values in Prometheus text exposition (read-only snapshot of all/matched metrics right now). Use match to filter; omit for everything.",
			InputSchema: objSchema(map[string]any{
				"match": strProp("PromQL selector, e.g. {__name__=~\"cpu.*\"} (optional; default = all metrics)."),
			}),
		},
		{
			Name:        "audit_events",
			Description: "Audit Logging — who did what in the project (read-only): control-plane events (create/delete/modify of VMs, DBs, k8s, S3, IAM, …) with actor, action, resource, time. Use to answer 'what happened on the stand / who deleted X / recent changes'. Paginated via next_page_token. NB: needs audit logging enabled on the project ('project not found' otherwise).",
			InputSchema: objSchema(map[string]any{
				"since":      strProp("Look-back window: 30m, 1h, 3h… (default 1h; keep short — API recommends ≤30min for chatty projects)."),
				"page_size":  intProp("Max events per page (default 100)."),
				"page_token": strProp("Continuation token from a previous call's next_page_token (optional)."),
			}),
		},
		{
			Name:        "cert_list",
			Description: "Certificate Manager — list TLS certificates in the project (read-only). Filter by name/dns_name/common_name/type/state. Use to find a cert for an HTTPS load balancer / site.",
			InputSchema: objSchema(map[string]any{
				"name":        strProp("Name or part of the certificate name (optional)."),
				"dns_name":    strProp("One of the certificate's DNS names, e.g. shkrbkv.ru (optional)."),
				"common_name": strProp("Partial common-name (CN) filter (optional)."),
				"type":        strProp("CERTIFICATE_TYPE_IMPORTED | CERTIFICATE_TYPE_SELF_SIGNED | CERTIFICATE_TYPE_LETS_ENCRYPT (optional)."),
				"state":       strProp("CERTIFICATE_STATE_ACTIVE | CERTIFICATE_STATE_EXPIRED (optional)."),
			}),
		},
		{
			Name:        "cert_get",
			Description: "Certificate Manager — get one certificate by id (metadata, primary version, expiry, status). Read-only.",
			InputSchema: objSchema(map[string]any{"id": strProp("Certificate id. Required.")}, "id"),
		},
		{
			Name:        "cert_issue_le",
			Description: "Certificate Manager — issue a FREE Let's Encrypt TLS certificate for one or more domains (async). This is the path for HTTPS on a site (e.g. shkrbkv.ru): issue → bind to the LB listener. ⚠️ The domains MUST have delegated zones in Evolution DNS first (else ERROR_CODE_NOT_DELEGATED). Returns an operation id — poll cert_operation until done=true.",
			InputSchema: objSchema(map[string]any{
				"name":        strProp("Display name for the certificate. Required."),
				"dns_names":   strProp("Domains for CN/SAN, e.g. [\"shkrbkv.ru\",\"www.shkrbkv.ru\"] (array or comma-separated). Required."),
				"description": strProp("Optional description."),
			}, "name", "dns_names"),
		},
		{
			Name:        "cert_import",
			Description: "Certificate Manager — import a user-managed certificate (bring-your-own). server_certificate and private_key are Base64-encoded PEM.",
			InputSchema: objSchema(map[string]any{
				"name":                     strProp("Display name. Required."),
				"server_certificate":       strProp("Public cert, Base64-encoded PEM. Required."),
				"private_key":              strProp("Private key, Base64-encoded PEM. Required."),
				"intermediate_certificate": strProp("Intermediate chain, Base64-encoded PEM (optional)."),
				"description":              strProp("Optional description."),
			}, "name", "server_certificate", "private_key"),
		},
		{
			Name:        "cert_renew",
			Description: "Certificate Manager — trigger Let's Encrypt renewal for a certificate (async). Returns an operation id.",
			InputSchema: objSchema(map[string]any{"certificate_id": strProp("Certificate id. Required.")}, "certificate_id"),
		},
		{
			Name:        "cert_delete",
			Description: "Certificate Manager — permanently delete a certificate. Gated: requires confirmed=true. If bound to a load balancer, deletion can break HTTPS (use force=true to delete anyway).",
			InputSchema: objSchema(map[string]any{
				"id":        strProp("Certificate id. Required."),
				"confirmed": boolProp("Must be true — set only after the captain approves."),
				"force":     boolProp("Delete even if the certificate is in use by other services (default false)."),
			}, "id"),
		},
		{
			Name:        "cert_usage",
			Description: "Certificate Manager — list resources (load balancers/listeners) that USE a certificate. Check this before deleting. Read-only.",
			InputSchema: objSchema(map[string]any{"id": strProp("Certificate id. Required.")}, "id"),
		},
		{
			Name:        "cert_versions",
			Description: "Certificate Manager — list a certificate's versions (id, CN, DNS names, status, expiry). Read-only.",
			InputSchema: objSchema(map[string]any{"certificate_id": strProp("Certificate id. Required.")}, "certificate_id"),
		},
		{
			Name:        "cert_private_key",
			Description: "Certificate Manager — return the SECRET private key (decoded PEM) for a version. Gated: requires confirmed=true (returns plaintext key material).",
			InputSchema: objSchema(map[string]any{
				"certificate_id": strProp("Certificate id. Required."),
				"version_id":     intProp("Version number; 0 or omitted = latest version."),
				"confirmed":      boolProp("Must be true — returns the plaintext private key."),
			}, "certificate_id"),
		},
		{
			Name:        "cert_operation",
			Description: "Certificate Manager — poll an async operation (issue/renew) by id until done=true. Read-only.",
			InputSchema: objSchema(map[string]any{"operation_id": strProp("Operation id from cert_issue_le/cert_renew. Required."), "wait_seconds": intProp("Optional: poll up to N seconds (capped 300) with backoff and return the FINAL state in ONE call — instead of re-checking every few seconds. Omit for a single instant check.")}, "operation_id"),
		},
		{
			Name:        "s3_buckets",
			Description: "List Object Storage (S3) buckets. Read-only. s3.cloud.ru (S3-compatible).",
			InputSchema: objSchema(map[string]any{}),
		},
		{
			Name:        "s3_ls",
			Description: "List objects in an S3 bucket (key, size, last_modified). Read-only.",
			InputSchema: objSchema(map[string]any{
				"bucket":   strProp("Bucket name. Required."),
				"prefix":   strProp("Key prefix filter (optional)."),
				"max_keys": intProp("Max objects to return (default 1000)."),
			}, "bucket"),
		},
		{
			Name:        "s3_get",
			Description: "Download an S3 object. Returns text inline (utf-8) when small & valid, else base64. Read-only.",
			InputSchema: objSchema(map[string]any{
				"bucket": strProp("Bucket name. Required."),
				"key":    strProp("Object key. Required."),
			}, "bucket", "key"),
		},
		{
			Name:        "s3_put",
			Description: "Upload an S3 object from inline content (content) or base64 (content_base64). Refuses to replace an existing key unless overwrite=true. For a file on disk — and for anything above a few MB — use s3_put_file instead.",
			InputSchema: objSchema(map[string]any{
				"bucket":         strProp("Bucket name. Required."),
				"key":            strProp("Object key. Required."),
				"content":        strProp("Inline text content to upload."),
				"content_base64": strProp("Base64-encoded binary content (alternative to content)."),
				"content_type":   strProp("MIME type (default application/octet-stream)."),
				"overwrite":      boolProp("Replace the object if the key already exists (default false — an existing key is refused)."),
			}, "bucket", "key"),
		},
		{
			Name:        "s3_put_file",
			Description: "Upload a file from this host's disk to S3, streamed (multipart for large files). This is the only upload path for a product artifact: a RAW VM image or a plugin archive cannot pass through inline content. Refuses to replace an existing key unless overwrite=true. Content type is guessed from the extension.",
			InputSchema: objSchema(map[string]any{
				"bucket":       strProp("Bucket name. Required."),
				"key":          strProp("Object key. Required."),
				"path":         strProp("Path to the file on this host. Required."),
				"content_type": strProp("MIME type (default: guessed from the extension, else application/octet-stream)."),
				"overwrite":    boolProp("Replace the object if the key already exists (default false — an existing key is refused)."),
			}, "bucket", "key", "path"),
		},
		{
			Name:        "s3_presign",
			Description: "Mint a time-limited download URL for an S3 object (GET, SigV4). Use it to hand a built product to a partner without making the bucket public. THE URL IS A BEARER CREDENTIAL — anyone holding it can download the object until it expires; never commit it. The signature covers the method, so `curl -I` returns 403: verify with a range GET (`curl -r 0-1023`), which returns 206. Read-only.",
			InputSchema: objSchema(map[string]any{
				"bucket":          strProp("Bucket name. Required."),
				"key":             strProp("Object key. Required."),
				"expires_seconds": intProp("Lifetime in seconds (default 604800 = 7 days, which is also the SigV4 maximum)."),
			}, "bucket", "key"),
		},
		{
			Name:        "s3_rm",
			Description: "Delete an S3 object. Destructive mutation: requires confirmed=true after the captain approves.",
			InputSchema: objSchema(map[string]any{
				"bucket":    strProp("Bucket name. Required."),
				"key":       strProp("Object key. Required."),
				"confirmed": boolProp("Must be true — set only after the captain approved."),
			}, "bucket", "key"),
		},
		{
			Name:        "s3_bucket_create",
			Description: "Create an Object Storage (S3) bucket. Use this when a bucket doesn't exist yet (s3_put needs an existing bucket). Idempotent — a bucket you already own is not an error.",
			InputSchema: objSchema(map[string]any{
				"bucket": strProp("New bucket name (globally-unique, lowercase). Required."),
				"region": strProp("Region (optional; defaults to the configured S3 region, e.g. ru-central-1)."),
			}, "bucket"),
		},
		{
			Name:        "s3_bucket_delete",
			Description: "Delete an EMPTY S3 bucket. Destructive mutation: requires confirmed=true after the captain approves.",
			InputSchema: objSchema(map[string]any{
				"bucket":    strProp("Bucket name. Required."),
				"confirmed": boolProp("Must be true — set only after the captain approved."),
			}, "bucket"),
		},
		{
			Name:        "stack_plan",
			Description: "Plan a multi-resource STACK from ONE declarative manifest. Use this for ANY multi-service deploy instead of calling pg_create/k8s_create/provision/tg_create/… yourself — the engine runs the whole chain server-side so your context stays small. Returns a job_id AND a rate-card cost estimate (show it to the captain, get «го», THEN stack_apply). Read-only (creates nothing). manifest = {name, vpc?{name}, postgres?{name,cpu,ram,storage_gb,no_backup}, kafka?{name,cpu,ram,storage_gib,brokers}, redis?{name,cpu,ram,storage_gb,shards,replicas,type}, k8s?{name,version,cpu,ram_gb,master_count,kube_api_internet}, registry?{name,registry_type,public}, snat?{name,zone}, vm?{name,private,flavor}, nlb?{name,port,target_port,external,target(default 'vm')}, dns?{zone_id,name,record_type,target(default 'lb'→LB public IP)}, secrets?[{path,value}], s3?[{bucket,key,content}]}. (container-apps: not yet — needs a registry image.)",
			InputSchema: objSchema(map[string]any{
				"manifest": map[string]any{"type": "object", "description": "Declarative stack spec (see description)."},
			}, "manifest"),
		},
		{
			Name:        "stack_apply",
			Description: "Apply a planned stack (from stack_plan). BILLABLE mutation: requires confirmed=true after the captain approved the cost. Returns immediately with a compact status; the engine runs the deploy across cranks — poll stack_status(job_id) until status=done. You do NOT orchestrate the resources yourself.",
			InputSchema: objSchema(map[string]any{
				"job_id":    strProp("Job id from stack_plan. Required."),
				"confirmed": boolProp("Must be true — set only after the captain approved."),
			}, "job_id"),
		},
		{
			Name:        "stack_status",
			Description: "Poll a stack deploy: returns COMPACT progress {status, done[], running[], pending[], outputs{postgres,vm,tg,lb,lb_external_ip,…}}. Each call advances the deploy AND waits internally (up to wait_seconds) so you need only a handful of calls for a long deploy. Keep polling until status=done (then read outputs.lb_external_ip etc.) or status=error. Resilient to context loss — the job lives on disk, just re-poll the job_id.",
			InputSchema: objSchema(map[string]any{
				"job_id":       strProp("Job id. Required."),
				"wait_seconds": intProp("How long this call may block while advancing (default 90, max 240). Fewer calls for a long deploy."),
			}, "job_id"),
		},
		{
			Name:        "stack_destroy",
			Description: "Tear down every resource in a stack (reverse order, idempotent). Destructive mutation: requires confirmed=true after the captain approved.",
			InputSchema: objSchema(map[string]any{
				"job_id":    strProp("Job id. Required."),
				"confirmed": boolProp("Must be true — set only after the captain approved."),
			}, "job_id"),
		},
	}

	s.handlers = map[string]toolHandler{
		"deploy":               s.handleDeploy,
		"provision":            s.handleProvision,
		"vm_exec":              s.handleVMExec,
		"vm_exec_status":       s.handleVMExecStatus,
		"vm_upload":            s.handleVMUpload,
		"vm_download":          s.handleVMDownload,
		"status":               s.handleStatus,
		"list":                 s.handleList,
		"vm_get":               s.handleVMGet,
		"logs":                 s.handleLogs,
		"destroy":              s.handleDestroy,
		"list_zones":           s.handleListZones,
		"list_images":          s.handleListImages,
		"image_create":         s.handleImageCreate,
		"image_get":            s.handleImageGet,
		"fip_attach":           s.handleFipAttach,
		"vm_power":             s.handleVMPower,
		"image_delete":         s.handleImageDelete,
		"deploy_plan":          s.handleDeployPlan,
		"deploy_apply":         s.handleDeployApply,
		"list_flavors":         s.handleListFlavors,
		"list_disk_types":      s.handleListDiskTypes,
		"list_subnets":         s.handleListSubnets,
		"list_security_groups": s.handleListSecurityGroups,
		"list_floating_ips":    s.handleListFloatingIPs,
		"release_floating_ip":  s.handleReleaseFloatingIP,
		"open_ports":           s.handleOpenPorts,
		"snat_list":            s.handleSnatList,
		"snat_create":          s.handleSnatCreate,
		"snat_delete":          s.handleSnatDelete,
		"k8s_list":             s.handleK8sList,
		"k8s_get":              s.handleK8sGet,
		"k8s_options":          s.handleK8sOptions,
		"k8s_plan":             s.handleK8sPlan,
		"k8s_create":           s.handleK8sCreate,
		"k8s_kubeconfig":       s.handleK8sKubeconfig,
		"k8s_delete":           s.handleK8sDelete,
		"k8s_add_nodepool":     s.handleK8sAddNodePool,
		"k8s_nodepools":        s.handleK8sNodePools,
		"k8s_kubectl":          s.handleK8sKubectl,
		"k8s_apply":            s.handleK8sApply,
		"k8s_helm":             s.handleK8sHelm,
		"disk_list":            s.handleDiskList,
		"disk_create":          s.handleDiskCreate,
		"disk_attach":          s.handleDiskAttach,
		"disk_detach":          s.handleDiskDetach,
		"disk_resize":          s.handleDiskResize,
		"disk_delete":          s.handleDiskDelete,
		"ca_list":              s.handleCaList,
		"ca_get":               s.handleCaGet,
		"ca_deploy":            s.handleCaDeploy,
		"ca_delete":            s.handleCaDelete,
		"ca_logs":              s.handleCaLogs,
		"ca_start":             s.handleCaStart,
		"ca_stop":              s.handleCaStop,
		"iam_service_accounts": s.handleIamServiceAccounts,
		"org_projects":         s.handleOrgProjects,
		"org_quotas":           s.handleOrgQuotas,
		"pg_list":              s.handlePgList,
		"pg_get":               s.handlePgGet,
		"pg_versions":          s.handlePgVersions,
		"pg_specs":             s.handlePgSpecs,
		"pg_create":            s.handlePgCreate,
		"pg_delete":            s.handlePgDelete,
		"pg_operation":         s.handlePgOperation,
		"kafka_list":           s.handleKafkaList,
		"kafka_get":            s.handleKafkaGet,
		"kafka_versions":       s.handleKafkaVersions,
		"kafka_specs":          s.handleKafkaSpecs,
		"kafka_create":         s.handleKafkaCreate,
		"kafka_delete":         s.handleKafkaDelete,
		"kafka_operation":      s.handleKafkaOperation,
		"redis_list":           s.handleRedisList,
		"redis_get":            s.handleRedisGet,
		"redis_versions":       s.handleRedisVersions,
		"redis_specs":          s.handleRedisSpecs,
		"redis_create":         s.handleRedisCreate,
		"redis_delete":         s.handleRedisDelete,
		"redis_operation":      s.handleRedisOperation,
		"pg_databases":         s.handlePgDatabases,
		"pg_db_create":         s.handlePgDbCreate,
		"pg_db_delete":         s.handlePgDbDelete,
		"pg_users":             s.handlePgUsers,
		"pg_user_create":       s.handlePgUserCreate,
		"pg_user_delete":       s.handlePgUserDelete,
		"pg_backups":           s.handlePgBackups,
		"pg_backup_create":     s.handlePgBackupCreate,
		"lb_list":              s.handleLbList,
		"lb_get":               s.handleLbGet,
		"lb_operation":         s.handleLbOperation,
		"lb_create":            s.handleLbCreate,
		"lb_delete":            s.handleLbDelete,
		"tg_list":              s.handleTgList,
		"tg_get":               s.handleTgGet,
		"tg_create":            s.handleTgCreate,
		"tg_delete":            s.handleTgDelete,
		"vpc_list":             s.handleVpcList,
		"vpc_get":              s.handleVpcGet,
		"vpc_create":           s.handleVpcCreate,
		"vpc_delete":           s.handleVpcDelete,
		"vpc_operation":        s.handleVpcOperation,
		"vpc_routes":           s.handleVpcRoutes,
		"vpc_route_create":     s.handleVpcRouteCreate,
		"vpc_route_delete":     s.handleVpcRouteDelete,
		"dns_zones":            s.handleDnsZones,
		"dns_zone_get":         s.handleDnsZoneGet,
		"dns_zone_create":      s.handleDnsZoneCreate,
		"dns_zone_delete":      s.handleDnsZoneDelete,
		"dns_operation":        s.handleDnsOperation,
		"dns_records":          s.handleDnsRecords,
		"dns_record_get":       s.handleDnsRecordGet,
		"dns_record_create":    s.handleDnsRecordCreate,
		"dns_record_delete":    s.handleDnsRecordDelete,
		"secret_list":          s.handleSecretList,
		"secret_get":           s.handleSecretGet,
		"secret_value":         s.handleSecretValue,
		"secret_create":        s.handleSecretCreate,
		"secret_add_version":   s.handleSecretAddVersion,
		"secret_delete":        s.handleSecretDelete,
		"ar_list":              s.handleArList,
		"ar_get":               s.handleArGet,
		"ar_create":            s.handleArCreate,
		"ar_delete":            s.handleArDelete,
		"ar_operation":         s.handleArOperation,
		"billing_balance":      s.handleBillingBalance,
		"billing_consumption":  s.handleBillingConsumption,
		"billing_finops":       s.handleBillingFinops,
		"metrics_history":      s.handleMetricsHistory,
		"metrics_scrape":       s.handleMetricsScrape,
		"audit_events":         s.handleAuditEvents,
		"cert_list":            s.handleCertList,
		"cert_get":             s.handleCertGet,
		"cert_issue_le":        s.handleCertIssueLE,
		"cert_import":          s.handleCertImport,
		"cert_renew":           s.handleCertRenew,
		"cert_delete":          s.handleCertDelete,
		"cert_usage":           s.handleCertUsage,
		"cert_versions":        s.handleCertVersions,
		"cert_private_key":     s.handleCertPrivateKey,
		"cert_operation":       s.handleCertOperation,
		"s3_buckets":           s.handleS3Buckets,
		"s3_ls":                s.handleS3Ls,
		"s3_get":               s.handleS3Get,
		"s3_put":               s.handleS3Put,
		"s3_put_file":          s.handleS3PutFile,
		"s3_presign":           s.handleS3Presign,
		"s3_rm":                s.handleS3Rm,
		"s3_bucket_create":     s.handleS3BucketCreate,
		"s3_bucket_delete":     s.handleS3BucketDelete,
		"stack_plan":           s.handleStackPlan,
		"stack_apply":          s.handleStackApply,
		"stack_status":         s.handleStackStatus,
		"stack_destroy":        s.handleStackDestroy,
	}
}

// --- handlers ---

func (s *Server) handleDeploy(ctx context.Context, args map[string]any) (string, bool) {
	overrides := map[string]any{}
	for arg, field := range map[string]string{
		"compose_file": "ComposeFile",
		"flavor":       "Flavor",
		"region":       "Region",
		"project_id":   "ProjectID",
		"image_id":     "ImageID",
		"disk_size":    "DiskSize",
	} {
		if v, ok := args[arg]; ok && v != nil && v != "" {
			overrides[field] = v
		}
	}

	eng, err := s.newEngine(overrides)
	if err != nil {
		return errResult(err), true
	}
	var buf bytes.Buffer
	eng.Printer = output.NewWithWriter(&buf, output.ModeText, false)

	force, _ := args["force"].(bool)
	opts := engine.DeployOptions{
		ComposeFile:    eng.Config.ComposeFile,
		Flavor:         eng.Config.Flavor,
		Region:         eng.Config.Region,
		Force:          force,
		NonInteractive: true,
	}

	deployErr := eng.Deploy(ctx, opts)

	result := map[string]any{"log": buf.String()}
	if sf, e := state.Read(state.DefaultPath(eng.StateDir)); e == nil {
		if active := sf.ActiveDeployment(); active != nil {
			result["deployment"] = active
		}
	}
	if deployErr != nil {
		result["status"] = "error"
		result["error"] = deployErr.Error()
		return toJSON(result), true
	}
	result["status"] = "ok"
	return toJSON(result), false
}

func (s *Server) handleProvision(ctx context.Context, args map[string]any) (string, bool) {
	overrides := map[string]any{}
	for arg, field := range map[string]string{
		"flavor":     "Flavor",
		"region":     "Region",
		"project_id": "ProjectID",
		"image_id":   "ImageID",
		"disk_size":  "DiskSize",
	} {
		if v, ok := args[arg]; ok && v != nil && v != "" {
			overrides[field] = v
		}
	}

	eng, err := s.newEngine(overrides)
	if err != nil {
		return errResult(err), true
	}

	// Auth
	tok, err := eng.Auth.Token(ctx)
	if err != nil {
		return errResult(err), true
	}
	eng.Cloud.SetToken(tok.Value)

	projectID := eng.Config.ProjectID
	if projectID == "" {
		return errResult(fmt.Errorf("CLOUDRU_PROJECT_ID is required")), true
	}

	imageID := eng.Config.ImageID
	if imageID == "" {
		return errResult(fmt.Errorf("CLOUDRU_IMAGE_ID is required")), true
	}

	region := eng.Config.Region
	if region == "" {
		return errResult(fmt.Errorf("region (availability zone) is required")), true
	}

	vmName := stringArg(args, "name")
	if vmName == "" {
		vmName = "cloudru-vm-" + fmt.Sprintf("%d", time.Now().Unix())
	}

	diskSize := eng.Config.DiskSize
	if diskSize <= 0 {
		diskSize = 10
	}

	// Тип диска выбирается, а не зашивается. Ёмкость по типам расходится
	// неравномерно, и зона может временно не отдавать SSD, продолжая отдавать
	// HDD: 16.09.2026 в ru.AZ-2 именно так — создание ВМ падает в
	// error_creating, и причина не видна ниоткуда. Для одноразовой сборочной
	// ВМ тип безразличен, а образ с неё покупатель развернёт на своём диске.
	diskTypeName := stringArg(args, "disk_type")
	if diskTypeName == "" {
		diskTypeName = "SSD"
	}

	// Resolve flavor: name -> UUID, "auto" -> smallest lowcost, UUID passthrough.
	// Free-tier is limited to 1 VM (usually already in use), so auto skips it.
	flavorID := eng.Config.Flavor
	flavors, err := eng.Cloud.ListFlavors(ctx)
	if err != nil {
		return errResult(fmt.Errorf("list flavors: %w", err)), true
	}
	if flavorID == "" || flavorID == "auto" {
		// Pick smallest lowcost flavor by CPU then RAM (skip free-tier)
		type scored struct {
			id  string
			cpu int
			ram int
		}
		var candidates []scored
		for _, f := range flavors {
			if f.FreeTier || strings.Contains(strings.ToLower(f.Name), "free") {
				continue
			}
			if strings.Contains(strings.ToLower(f.Name), "low") || strings.Contains(strings.ToLower(f.Name), "lowcost") {
				candidates = append(candidates, scored{f.ID, f.VCPUs, f.RAMGB})
			}
		}
		if len(candidates) > 0 {
			best := candidates[0]
			for _, c := range candidates[1:] {
				if c.cpu < best.cpu || (c.cpu == best.cpu && c.ram < best.ram) {
					best = c
				}
			}
			flavorID = best.id
		} else if len(flavors) > 0 {
			// Fallback: first non-free-tier
			for _, f := range flavors {
				if !f.FreeTier {
					flavorID = f.ID
					break
				}
			}
			if flavorID == "" {
				flavorID = flavors[0].ID
			}
		}
	} else {
		// If not a UUID, resolve by name
		if !isUUID(flavorID) {
			resolved := ""
			for _, f := range flavors {
				if f.Name == flavorID || f.ID == flavorID {
					resolved = f.ID
					break
				}
			}
			if resolved == "" {
				names := make([]string, len(flavors))
				for i, f := range flavors {
					names[i] = f.Name + " (" + f.ID + ")"
				}
				return errResult(fmt.Errorf("flavor %q not found. Available: %s", flavorID, strings.Join(names, ", "))), true
			}
			flavorID = resolved
		}
	}

	// Fetch image metadata
	imgDetail, err := eng.Cloud.GetImage(ctx, imageID)
	if err != nil {
		return errResult(fmt.Errorf("get image details: %w", err)), true
	}

	imgMeta := map[string]string{}
	satisfiedGroups := map[string]bool{}
	for _, mf := range imgDetail.MetadataFields {
		switch mf.Name {
		case "ssh_key", "public_key", "ssh_public_key":
			// An EMPTY public_key triggers a 500; a trailing newline (the keygen
			// appends one) makes a malformed key value → also 500. Trim it.
			if key := strings.TrimSpace(eng.SSHPubKey); key != "" {
				imgMeta[mf.Name] = key
				if mf.MandatoryGroup != "" {
					satisfiedGroups[mf.MandatoryGroup] = true
				}
			}
		case "hostname", "instance_name":
			imgMeta[mf.Name] = vmName
			if mf.MandatoryGroup != "" {
				satisfiedGroups[mf.MandatoryGroup] = true
			}
		case "linux_password":
			// Mandatory for most images — deploy_apply generates one; without it
			// the VM boots to error_creating. Guaranteed to satisfy complexity.
			imgMeta[mf.Name] = genPassword(16)
			if mf.MandatoryGroup != "" {
				satisfiedGroups[mf.MandatoryGroup] = true
			}
		case "name", "user", "username", "default_user":
			val := mf.DefaultValue
			if val == "" {
				val = "user1"
			}
			imgMeta[mf.Name] = val
			if mf.MandatoryGroup != "" {
				satisfiedGroups[mf.MandatoryGroup] = true
			}
		}
	}
	for _, mf := range imgDetail.MetadataFields {
		if _, exists := imgMeta[mf.Name]; exists {
			continue
		}
		if mf.MandatoryGroup != "" && satisfiedGroups[mf.MandatoryGroup] {
			continue
		}
		// vmm_enabled / guest_agent_ready are READ-ONLY status fields — sending
		// them (or any empty-valued field) 500s the backend. Mirror deploy_apply
		// (deployplan/resolve.go): only send optional fields that have a real
		// default value, and never these two.
		if mf.Name == "vmm_enabled" || mf.Name == "guest_agent_ready" {
			continue
		}
		if mf.DefaultValue != "" {
			imgMeta[mf.Name] = mf.DefaultValue
		}
	}

	sshUser := "user1"
	for _, key := range []string{"name", "user", "username", "default_user"} {
		if v, ok := imgMeta[key]; ok && v != "" {
			sshUser = v
			break
		}
	}

	// Resolve a subnet + security group for the network interface. Omitting the
	// interfaces section makes the create API 500 — the request MUST carry an
	// explicit interface (subnet + SG). Prefer a subnet/SG in the target zone.
	// The subnet/SG availability_zone_id is empty in the API — match by NAME
	// containing the zone name (e.g. "ru.AZ-2"), mirroring deploy_apply. Picking
	// by the (empty) az id defaults to the FIRST SG, which is usually the WRONG
	// zone (e.g. an AZ-1 SG on an AZ-2 VM) → the VM fails to boot (error_creating).
	zoneName := ""
	if zones, e := eng.Cloud.ListAvailabilityZones(ctx); e == nil {
		for _, z := range zones {
			if z.ID == region {
				zoneName = z.Name
				break
			}
		}
	}
	subnets, err := eng.Cloud.ListSubnets(ctx, projectID)
	if err != nil {
		return errResult(fmt.Errorf("list subnets: %w", err)), true
	}
	if len(subnets) == 0 {
		return errResult(fmt.Errorf("no subnets in project — cannot attach a network interface")), true
	}
	// Подсеть берётся ТОЛЬКО из целевой зоны. Прежний запасной вариант
	// «первая в проекте» подсовывал подсеть AZ-2 машине в AZ-1, и платформа
	// отвечала не отказом, а error_creating без причины — ловушка, описанная
	// ниже для групп, работала и здесь. Отказ с внятным текстом дешевле.
	subnetID := ""
	if zoneName != "" {
		for _, sn := range subnets {
			if strings.Contains(sn.Name, zoneName) {
				subnetID = sn.ID
				break
			}
		}
	}
	if subnetID == "" && stringArg(args, "subnet_id") == "" {
		if zoneName == "" {
			subnetID = subnets[0].ID
		} else {
			return errResult(fmt.Errorf("no subnet in zone %s: create one (e.g. Default_%s) or pass subnet_id", zoneName, zoneName)), true
		}
	}
	if v := stringArg(args, "subnet_id"); v != "" {
		subnetID = v
	}
	var sgIDs []string
	if sgs, e := eng.Cloud.ListSecurityGroups(ctx, projectID); e == nil && len(sgs) > 0 {
		chosen := ""
		for _, g := range sgs {
			if zoneName != "" && strings.Contains(g.Name, zoneName) {
				chosen = g.ID
				break
			}
		}
		if chosen == "" && zoneName == "" {
			chosen = sgs[0].ID
		}
		if chosen == "" {
			// Та же ловушка, что с подсетью: группа из чужой зоны — это
			// error_creating без причины, а не отказ API.
			return errResult(fmt.Errorf("no security group in zone %s: create one (e.g. SSH-access_%s)", zoneName, zoneName)), true
		}
		sgIDs = []string{chosen}
	}

	// no_public_ip → a private VM: interface type "regular", no floating IP.
	// This is the valid target type for a load balancer (a direct-public-IP VM
	// has a "direct_ip" interface, which NLB rejects). Skipping the FIP also
	// avoids the FIP path entirely.
	noPublicIP, _ := args["no_public_ip"].(bool)

	// The v1.1 create API expects NAME-based fields (flavor_name/image_name/
	// disk_type_name) — sending UUIDs 500s. Mirror deploy_apply (the proven path):
	// resolve the flavor UUID back to its name, use the image's name.
	flavorName := flavorID
	for _, fl := range flavors {
		if fl.ID == flavorID {
			flavorName = fl.Name
			break
		}
	}

	// Create VM
	req := provision.CreateVMRequest{
		ProjectID:          projectID,
		Name:               vmName,
		FlavorName:         flavorName,
		AvailabilityZoneID: region,
		ImageName:          imgDetail.Name,
		ImageMetadata:      imgMeta,
		Disks: []provision.DiskReq{
			{
				Name:         vmName + "-boot",
				Size:         diskSize,
				DiskTypeName: diskTypeName,
			},
		},
		Interfaces: []provision.InterfaceReq{
			{
				Type:                     "regular",
				SubnetID:                 subnetID,
				InterfaceSecurityEnabled: true,
				SecurityGroups:           sgIDs,
			},
		},
	}

	// dry_run: return the exact request body without creating anything (debug).
	if dr, _ := args["dry_run"].(bool); dr {
		b, _ := json.Marshal([]provision.CreateVMRequest{req})
		return string(b), false
	}

	// Имя освобождается не сразу. Удаление ВМ на стороне платформы идёт
	// асинхронно, и пересборка сразу после сноса упирается в 409
	// vm_with_this_data_already_exists по полю name — при том что ВМ в списке
	// проекта уже нет. 16.09.2026 сборка redmine встала так трижды подряд.
	// Ждём, пока имя освободится: полминуты между попытками, всего до трёх.
	var vm *provision.VM
	for попытка := 1; ; попытка++ {
		vm, err = eng.Cloud.CreateVM(ctx, req)
		if err == nil {
			break
		}
		if попытка >= 3 || !strings.Contains(err.Error(), "vm_with_this_data_already_exists") {
			return errResult(fmt.Errorf("create VM: %w", err)), true
		}
		select {
		case <-ctx.Done():
			return errResult(fmt.Errorf("create VM: ожидание освобождения имени %q прервано: %w", vmName, ctx.Err())), true
		case <-time.After(30 * time.Second):
		}
	}

	// Poll until active.
	//
	// Восемь минут, не пять: под нагрузкой (восемь одновременных сборок)
	// Cloud.ru выводит ВМ в running за шесть-семь минут. Потолок держим ниже
	// таймаута вызова у оркестратора (десять минут), иначе ответ не дойдёт.
	pollTimeout := 8 * time.Minute
	deadline := time.After(pollTimeout)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			// НЕ ошибка. ВМ создана и тарифицируется; если ответить ошибкой,
			// вызывающая сторона обрывается на провале и идентификатор ВМ
			// теряется — машина остаётся в облаке, и снести её автоматически
			// нечем (16.09.2026 так потерялась selenium-grid-build). Отдаём
			// идентификатор: ждать активности и выдавать адрес умеет
			// вызывающая сторона (wait_active + fip_attach).
			return toJSON(map[string]any{
				"status":  "timeout",
				"vm_id":   vm.ID,
				"message": fmt.Sprintf("VM %s created but did not become active within %s; ждите активности и выдайте адрес через fip_attach", vm.ID, pollTimeout),
			}), false
		case <-ticker.C:
			updated, err := eng.Cloud.GetVM(ctx, vm.ID)
			if err != nil {
				continue
			}
			if updated.Status == "active" || updated.Status == "running" {
				// Assign a floating (public) IP unless a private VM was requested.
				publicIP := ""
				var fipWarn string
				if !noPublicIP {
					ip, fipErr := engine.AssignFloatingIP(ctx, eng.Cloud, vm.ID, projectID, region)
					publicIP = ip
					if fipErr != nil || publicIP == "" {
						// Отказ выдачи адреса ПРОГЛАТЫВАЛСЯ, и provision отвечал
						// успехом с пустым public_ip. Правда вскрывалась через
						// пятнадцать минут на первом обращении по SSH — «VM has no
						// public IP». 16.09.2026 так прошла квота на адреса
						// (network.ip.quantity): ВМ создана, платится, войти нельзя.
						// ВМ не сносим: она создана и учтена, а адрес можно выдать
						// отдельно (fip_attach) — но молчать об этом нельзя.
						fipWarn = "ВМ создана, но публичный адрес не выдан"
						if fipErr != nil {
							fipWarn += ": " + fipErr.Error()
						}
						fipWarn += ". Освободите адрес или квоту и выполните fip_attach — по SSH ВМ сейчас недоступна."
					}
				}

				// Save state
				statePath := state.DefaultPath(eng.StateDir)
				sf, _ := state.Read(statePath)
				rec := state.DeploymentRecord{
					ID:        vm.ID,
					VMID:      vm.ID,
					VMIP:      publicIP,
					VMFlavor:  flavorID,
					Region:    region,
					SSHUser:   sshUser,
					Status:    "running",
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
				}
				if sf != nil {
					sf.UpsertDeployment(rec)
					_ = state.Write(statePath, sf)
				}

				msg := fmt.Sprintf("VM %s is active at %s. SSH: ssh %s@%s", vm.ID, publicIP, sshUser, publicIP)
				if noPublicIP {
					msg = fmt.Sprintf("VM %s is active (private — no public IP, interface type 'regular'). Usable as a load-balancer target; reach it only from inside the VPC.", vm.ID)
				}
				return toJSON(map[string]any{
					"status":   "ok",
					"vm_id":    vm.ID,
					"warning":  fipWarn,
					"vm_ip":    publicIP,
					"private":  noPublicIP,
					"ssh_user": sshUser,
					"flavor":   flavorID,
					"region":   region,
					"message":  msg,
				}), false
			}
			if updated.Status == "error" {
				return errResult(fmt.Errorf("VM %s entered error state", vm.ID)), true
			}
		case <-ctx.Done():
			return errResult(ctx.Err()), true
		}
	}
}

func (s *Server) handleStatus(ctx context.Context, args map[string]any) (string, bool) {
	eng, err := s.newEngine(nil)
	if err != nil {
		return errResult(err), true
	}
	sf, err := state.Read(state.DefaultPath(eng.StateDir))
	if err != nil {
		return errResult(err), true
	}
	active := sf.ActiveDeployment()
	if active == nil {
		return toJSON(map[string]any{"status": "none", "message": "No active deployment."}), false
	}
	return toJSON(active), false
}

func (s *Server) handleList(ctx context.Context, args map[string]any) (string, bool) {
	eng, err := s.newEngine(nil)
	if err != nil {
		return errResult(err), true
	}
	sf, err := state.Read(state.DefaultPath(eng.StateDir))
	if err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{"deployments": sf.Deployments}), false
}

func (s *Server) handleLogs(ctx context.Context, args map[string]any) (string, bool) {
	eng, err := s.newEngine(nil)
	if err != nil {
		return errResult(err), true
	}
	var buf bytes.Buffer
	eng.Printer = output.NewWithWriter(&buf, output.ModeText, false)

	opts := engine.LogsOptions{
		Service: stringArg(args, "service"),
		Tail:    intArg(args, "tail", 100),
		Follow:  false,
	}
	if err := eng.Logs(ctx, opts); err != nil {
		return buf.String() + "\n" + err.Error(), true
	}
	return buf.String(), false
}

func (s *Server) handleDestroy(ctx context.Context, args map[string]any) (string, bool) {
	// Targeted delete: a specific VM by id or name (e.g. an orphan).
	if vmID := stringArg(args, "vm_id"); vmID != "" {
		return s.destroyTargeted(ctx, args, vmID, "")
	}
	if name := stringArg(args, "name"); name != "" {
		return s.destroyTargeted(ctx, args, "", name)
	}

	// Default: delete the current local deployment. Destructive and previously
	// ungated — now requires the same explicit confirmation as targeted deletes.
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status":  "confirmation_required",
			"message": "destroy permanently deletes the current deployment VM and all its data (irreversible). Get the captain's approval, then call again with confirmed=true.",
		}), true
	}
	eng, err := s.newEngine(nil)
	if err != nil {
		return errResult(err), true
	}
	var buf bytes.Buffer
	eng.Printer = output.NewWithWriter(&buf, output.ModeText, false)

	if err := eng.Destroy(ctx, engine.DestroyOptions{Force: true}); err != nil {
		return buf.String() + "\n" + err.Error(), true
	}
	return toJSON(map[string]any{"status": "ok", "log": buf.String()}), false
}

// destroyTargeted deletes a specific VM by id or name (gated on confirmation),
// optionally releasing its floating IP and removing it from local state.
func (s *Server) destroyTargeted(ctx context.Context, args map[string]any, vmID, name string) (string, bool) {
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status":  "confirmation_required",
			"message": "deleting a specific VM requires confirmed=true. Get the captain's approval, then call again with confirmed=true.",
		}), true
	}
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}

	projectID := eng.Config.ProjectID
	vms, err := eng.Cloud.ListVMsInProject(ctx, projectID)
	if err != nil {
		return errResult(err), true
	}
	var target *provision.VM
	for _, v := range vms {
		if (vmID != "" && v.ID == vmID) || (name != "" && v.Name == name) {
			target = v
			break
		}
	}
	if target == nil {
		return errResult(fmt.Errorf("no VM matching vm_id=%q name=%q in this project", vmID, name)), true
	}

	fip := target.FloatingIP
	if err := eng.Cloud.DeleteVMInProject(ctx, target.ID, projectID); err != nil {
		return errResult(fmt.Errorf("delete VM: %w", err)), true
	}

	// Best-effort: remove from local state.
	statePath := state.DefaultPath(eng.StateDir)
	if sf, e := state.Read(statePath); e == nil {
		kept := sf.Deployments[:0]
		for _, d := range sf.Deployments {
			if d.VMID != target.ID {
				kept = append(kept, d)
			}
		}
		sf.Deployments = kept
		_ = state.Write(statePath, sf)
	}

	// Release the floating IP unless asked not to.
	releaseIP := true
	if v, ok := args["release_ip"].(bool); ok {
		releaseIP = v
	}
	msg := "VM deleted"
	if releaseIP && fip != "" {
		if fips, e := eng.Cloud.ListFloatingIPs(ctx, eng.Config.ProjectID); e == nil {
			for _, f := range fips {
				if f.IPAddress == fip {
					if de := eng.Cloud.DeleteFloatingIP(ctx, f.ID, eng.Config.ProjectID); de == nil {
						msg += "; released floating IP " + fip
					}
					break
				}
			}
		}
	}
	return toJSON(map[string]any{
		"status":     "ok",
		"deleted_vm": target.ID,
		"name":       target.Name,
		"message":    msg,
	}), false
}

func (s *Server) handleListZones(ctx context.Context, args map[string]any) (string, bool) {
	eng, err := s.newEngine(nil)
	if err != nil {
		return errResult(err), true
	}
	tok, err := eng.Auth.Token(ctx)
	if err != nil {
		return errResult(err), true
	}
	eng.Cloud.SetToken(tok.Value)

	zones, err := eng.Cloud.ListAvailabilityZones(ctx)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{"zones": zones}), false
}

func (s *Server) handleListImages(ctx context.Context, args map[string]any) (string, bool) {
	eng, err := s.newEngine(nil)
	if err != nil {
		return errResult(err), true
	}
	tok, err := eng.Auth.Token(ctx)
	if err != nil {
		return errResult(err), true
	}
	eng.Cloud.SetToken(tok.Value)

	images, err := eng.Cloud.ListImages(ctx)
	if err != nil {
		return errResult(err), true
	}
	// The shared catalogue does not contain images built inside the project, so
	// they are fetched separately and merged — otherwise a freshly built image
	// is missing from the very list used to find it.
	seen := make(map[string]bool, len(images))
	for _, img := range images {
		seen[img.ID] = true
	}
	own, ownErr := eng.Cloud.ListImagesInProject(ctx, eng.Config.ProjectID)
	for _, img := range own {
		if !seen[img.ID] {
			images = append(images, img)
		}
	}
	res := map[string]any{"images": images}
	if ownErr != nil {
		res["warning"] = "не удалось получить образы проекта: " + ownErr.Error()
	}
	return toJSON(res), false
}

// --- helpers ---

// objSchema builds a JSON Schema object with the given properties and required
// keys. Pass nil props for a no-argument tool.
// genPassword returns a random n-char password guaranteed to contain at least
// one lower, upper, digit and special char (Cloud.ru rejects weaker ones).
func genPassword(n int) string {
	if n < 8 {
		n = 16
	}
	const lower = "abcdefghijkmnpqrstuvwxyz"
	const upper = "ABCDEFGHJKLMNPQRSTUVWXYZ"
	const digit = "23456789"
	const special = "!@#$%_-"
	all := lower + upper + digit + special
	rb := make([]byte, n)
	_, _ = crand.Read(rb)
	out := make([]byte, n)
	out[0] = lower[int(rb[0])%len(lower)]
	out[1] = upper[int(rb[1])%len(upper)]
	out[2] = digit[int(rb[2])%len(digit)]
	out[3] = special[int(rb[3])%len(special)]
	for i := 4; i < n; i++ {
		out[i] = all[int(rb[i])%len(all)]
	}
	return string(out)
}

func objSchema(props map[string]any, required ...string) map[string]any {
	if props == nil {
		props = map[string]any{}
	}
	schema := map[string]any{
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

func intProp(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}

func boolProp(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}

func stringArg(args map[string]any, key string) string {
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

// stringSliceArg reads a string-array argument. Accepts a JSON array of strings
// or a single comma-separated string (tolerant of both MCP client styles).
func stringSliceArg(args map[string]any, key string) []string {
	var out []string
	switch v := args[key].(type) {
	case []any:
		for _, x := range v {
			if s, ok := x.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
	case []string:
		for _, s := range v {
			if strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
	case string:
		for _, s := range strings.Split(v, ",") {
			if strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
	}
	return out
}

// intArg reads an integer argument. JSON numbers decode as float64.
func intArg(args map[string]any, key string, def int) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return int(n)
		}
	}
	return def
}

// toJSON marshals v to an indented JSON string for tool output.
func toJSON(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return `{"error":"failed to encode result"}`
	}
	return string(b)
}

// errResult formats an error as a JSON tool result.
func errResult(err error) string {
	return toJSON(map[string]any{"status": "error", "error": err.Error()})
}

// isUUID checks if a string looks like a UUID (xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx).
func isUUID(s string) bool {
	return uuidRe.MatchString(s)
}

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
