package deployplan

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"net"
	"strings"
	"time"

	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/sgrules"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// DefaultPollTimeout is how long Apply waits for the VM to run when the
// caller sets nothing: the same 10 minutes as the CLI deploy (a new VM in
// ru.AZ-1 took about 5, 15.10.2026).
const DefaultPollTimeout = 10 * time.Minute

// pollInterval is how often Apply asks for the VM state (a variable for tests).
var pollInterval = 10 * time.Second

// ApplyResult is the compact result returned to the agent after a deploy.
type ApplyResult struct {
	VMID       string `json:"vm_id"`
	Status     string `json:"status"`
	PublicIP   string `json:"public_ip,omitempty"`
	SSH        string `json:"ssh,omitempty"`
	Password   string `json:"password,omitempty"` // shown ONCE; not persisted
	ElapsedSec int    `json:"elapsed_sec"`
	Note       string `json:"note,omitempty"`

	// Ports is what Apply recorded about the security group: the group, the
	// ports the VM needs and the rules Apply created (the caller keeps them
	// in state so destroy deletes them). Not part of the agent's answer.
	Ports state.DeploymentRecord `json:"-"`
}

// Apply executes a resolved plan. It is idempotent: if a VM with the plan's name
// already exists, it adopts that VM instead of creating a duplicate.
func Apply(ctx context.Context, cloud provision.CloudAPI, plan *Plan) (*ApplyResult, error) {
	return ApplyWithTimeout(ctx, cloud, plan, DefaultPollTimeout)
}

// ApplyWithTimeout is Apply waiting at most pollTimeout for the VM to run
// (CLOUDRU_TIMEOUT through the engine's timings; zero means the default).
func ApplyWithTimeout(ctx context.Context, cloud provision.CloudAPI, plan *Plan, pollTimeout time.Duration) (*ApplyResult, error) {
	if pollTimeout <= 0 {
		pollTimeout = DefaultPollTimeout
	}
	start := time.Now()

	// --- Idempotency: adopt an existing VM with the same name ---
	existing, lookErr := findVMByName(ctx, cloud, plan.ProjectID, plan.VM.Name)
	if lookErr != nil {
		// Unknown is not "absent": creating now could make a second
		// billed VM next to one a lost answer already created (QA H5).
		return nil, fmt.Errorf("cannot check whether VM %q already exists (%w); nothing was created — retry deploy_apply", plan.VM.Name, lookErr)
	}
	if existing != nil {
		if existing.Status == "error" {
			return &ApplyResult{
				VMID:   existing.ID,
				Status: "error",
				Note:   "a VM with this name already exists in error state; destroy it first, then retry",
			}, nil
		}
		ip, fipNote := ensureFloatingIP(ctx, cloud, plan, existing.ID)
		return &ApplyResult{
			VMID:       existing.ID,
			Status:     "running",
			PublicIP:   ip,
			SSH:        sshString(plan.SSH.Login, ip),
			ElapsedSec: int(time.Since(start).Seconds()),
			Note:       joinNotes("VM already existed — adopted (idempotent), nothing recreated", fipNote),
		}, nil
	}

	// --- Build image_metadata (password generated here, never persisted) ---
	// Start from the image's required defaults (copied verbatim), then fill the
	// dynamic fields. Omitting the defaults yields image_metadata_fields_missing.
	meta := map[string]string{}
	for k, v := range plan.MetaStatic {
		meta[k] = v
	}
	password := ""
	for _, f := range plan.MetaFields {
		switch f {
		case "name":
			meta["name"] = plan.SSH.Login
		case "hostname":
			meta["hostname"] = plan.VM.Name
		case "linux_password":
			password = generatePassword(16)
			meta["linux_password"] = password
		case "public_key":
			if plan.PublicKey != "" {
				meta["public_key"] = plan.PublicKey
			}
		}
	}

	// --- Build the create request (mirrors the proven v1.1 body) ---
	iface := provision.InterfaceReq{
		Type:                     "regular",
		SubnetID:                 plan.Network.SubnetID,
		InterfaceSecurityEnabled: true,
	}
	if plan.Network.SecurityGroupID != "" {
		iface.SecurityGroups = []string{plan.Network.SecurityGroupID}
	}
	if plan.Network.FloatingIP && plan.Network.AttachFIPID != "" {
		iface.AttachExternalIPID = plan.Network.AttachFIPID // variant A
	}

	req := provision.CreateVMRequest{
		ProjectID:          plan.ProjectID,
		Name:               plan.VM.Name,
		FlavorName:         plan.VM.Flavor.Name,
		ImageName:          plan.VM.Image.Name,
		AvailabilityZoneID: plan.VM.Zone.ID,
		Disks: []provision.DiskReq{{
			Name:         plan.VM.Name + "-disk",
			Size:         plan.VM.Disk.SizeGB,
			DiskTypeName: firstNonEmpty(plan.VM.Disk.Type, "SSD"),
		}},
		Interfaces: []provision.InterfaceReq{iface},
	}
	if len(meta) > 0 {
		req.ImageMetadata = meta
	}

	createStart := time.Now()
	vm, err := cloud.CreateVM(ctx, req)
	if err != nil {
		// The VM can exist although the call failed (timeout, cancel, a
		// lost answer): name it, so the caller records it, and a retry of
		// the plan adopts it.
		if found := provision.FindCreatedVM(cloud, plan.ProjectID, plan.VM.Name, createStart); found != nil {
			return &ApplyResult{
					VMID: found.ID, Status: "interrupted",
					ElapsedSec: int(time.Since(start).Seconds()),
				}, fmt.Errorf("VM %s was created, but the create call failed; it is billed — "+
					"call deploy_apply again with the same plan_id to finish (the VM is adopted, not recreated), "+
					"or delete it with destroy: %w", found.ID, err)
		}
		return nil, fmt.Errorf("create VM: %w", err)
	}

	// Cancelled from here on (the MCP client went away or cancelled the
	// call): the VM exists and is billed. The result names it, so the
	// caller can record it in the state, together with an error that says
	// what to do with it.
	interrupted := func(ip string) (*ApplyResult, error) {
		return &ApplyResult{
				VMID: vm.ID, Status: "interrupted", PublicIP: ip,
				ElapsedSec: int(time.Since(start).Seconds()),
			}, fmt.Errorf("VM %s was created, but deploy_apply was interrupted; it is still running — "+
				"call deploy_apply again with the same plan_id to finish (the VM is adopted, not recreated), "+
				"check it with vm_get, or delete it with destroy: %w", vm.ID, ctx.Err())
	}

	// --- Poll until running ---
	status := "building"
	deadline := time.After(pollTimeout)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
poll:
	for {
		select {
		case <-ctx.Done():
			// The VM exists: say which, or nobody will find it.
			return interrupted("")
		case <-deadline:
			return &ApplyResult{
				VMID: vm.ID, Status: "timeout", Password: password,
				ElapsedSec: int(time.Since(start).Seconds()),
				Note:       fmt.Sprintf("VM did not reach running within %s; check status later with the status tool", pollTimeout),
			}, nil
		case <-ticker.C:
			cur, e := cloud.GetVM(ctx, vm.ID)
			if e != nil {
				continue
			}
			switch cur.Status {
			case "running", "active":
				status = "running"
				break poll
			case "error":
				return &ApplyResult{
					VMID: vm.ID, Status: "error", Password: password,
					ElapsedSec: int(time.Since(start).Seconds()),
					Note:       "VM entered error state; destroy it and retry",
				}, nil
			}
		}
	}

	// --- Floating IP ---
	ip, fipNote := ensureFloatingIP(ctx, cloud, plan, vm.ID)
	if ctx.Err() != nil {
		// The steps below would only fail on the cancelled context and
		// report a half-done VM as "running".
		return interrupted(ip)
	}

	// --- Wait until SSH (port 22) is actually reachable, so the agent's very
	// first ssh attempt succeeds instead of hitting a boot-time timeout (which
	// has been misread as a "security group blocks 22" problem). Bounded so the
	// tool call stays well under the MCP timeout. ---
	sshNote := ""
	if ip != "" {
		if waitForSSHPort(ctx, ip, 120*time.Second) {
			sshNote = "SSH port 22 is open — connect now with the ssh command above."
		} else {
			sshNote = "VM is up but SSH port 22 was not reachable yet — wait ~1 min and retry the same ssh command. The VM is still booting; do NOT blame security groups, do NOT recreate the VM."
		}
	}

	// --- Open the product's ports in the security group (auto). Same code
	// as the CLI deploy: the rules created are recorded for destroy. ---
	var ports state.DeploymentRecord
	opened := sgrules.Open(ctx, cloud, plan.ProjectID, plan.Network.SecurityGroupID, plan.Ports, nil,
		func(label string) string { return fmt.Sprintf("auto: %s port %s", plan.Product, label) }, &ports)

	return &ApplyResult{
		VMID:       vm.ID,
		Status:     status,
		PublicIP:   ip,
		SSH:        sshString(plan.SSH.Login, ip),
		Password:   password,
		ElapsedSec: int(time.Since(start).Seconds()),
		Note:       joinNotes(fipNote, sshNote, opened.Note()),
		Ports:      ports,
	}, nil
}

// waitForSSHPort polls TCP port 22 on ip until it accepts a connection or the
// timeout elapses. Returns true if the port became reachable.
func waitForSSHPort(ctx context.Context, ip string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return false
		default:
		}
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(ip, "22"), 5*time.Second)
		if err == nil {
			_ = conn.Close()
			return true
		}
		time.Sleep(5 * time.Second)
	}
	return false
}

// ensureFloatingIP returns the VM's public IP plus a human note describing what
// happened. It attaches a floating IP if the plan asked for one: variant A id
// (attached at creation), then variant B (reuse an existing free FIP), then
// create a new FIP. If the FIP quota is exhausted, it does NOT fail — the VM
// stays up without a public IP and the note explains how to fix it.
func ensureFloatingIP(ctx context.Context, cloud provision.CloudAPI, plan *Plan, vmID string) (string, string) {
	if !plan.Network.FloatingIP {
		return "", ""
	}
	// Variant A: FIP id was attached at creation — just read its address.
	if plan.Network.AttachFIPID != "" {
		if f, err := cloud.GetFloatingIP(ctx, plan.Network.AttachFIPID); err == nil && f.IPAddress != "" {
			return f.IPAddress, "public IP attached (existing floating IP)"
		}
	}

	detail, err := cloud.GetVMDetail(ctx, vmID)
	if err != nil || len(detail.Interfaces) == 0 {
		return "", "could not read the VM interface yet — attach a floating IP later via the Cloud.ru console"
	}
	ifaceID := detail.Interfaces[0].ID
	for _, in := range detail.Interfaces {
		if in.Primary {
			ifaceID = in.ID
			break
		}
	}

	// Variant B: attach an existing available FIP.
	if fips, e := cloud.ListFloatingIPs(ctx, plan.ProjectID); e == nil {
		for _, f := range fips {
			if f.Available() {
				if _, ae := cloud.AttachFloatingIP(ctx, f.ID, plan.ProjectID, ifaceID, plan.VM.Name+"-fip"); ae == nil {
					return f.IPAddress, "public IP attached (reused a free floating IP)"
				}
			}
		}
	}

	// No free FIP: request a new one. Quota exhaustion is reported, not fatal.
	f, e := cloud.CreateFloatingIP(ctx, provision.CreateFloatingIPRequest{
		Name:               plan.VM.Name + "-fip",
		ProjectID:          plan.ProjectID,
		InterfaceID:        ifaceID,
		AvailabilityZoneID: plan.VM.Zone.ID,
	})
	if e == nil {
		return f.IPAddress, "public IP attached (created a new floating IP)"
	}
	if provision.IsQuotaError(e) {
		return "", "⚠️ Floating-IP quota exhausted — the VM was created WITHOUT a public IP. Increase the floating-IP quota in the Cloud.ru console, then attach an IP to this VM manually."
	}
	return "", "could not attach a floating IP (" + e.Error() + ") — the VM was created without a public IP; attach one via the console"
}

// joinNotes concatenates non-empty notes with "; ".
func joinNotes(notes ...string) string {
	var parts []string
	for _, n := range notes {
		if n != "" {
			parts = append(parts, n)
		}
	}
	return strings.Join(parts, "; ")
}

// findVMByName looks the plan's VM up in its project. GET /vms without a
// project_id returns no VMs (.github/review-context/cloudru-api.md), so the
// adoption that a retry of deploy_apply promises never happened and the
// retry created a second VM (QA H5).
func findVMByName(ctx context.Context, cloud provision.CloudAPI, projectID, name string) (*provision.VM, error) {
	vms, err := cloud.ListVMsInProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	for _, v := range vms {
		if v.Name == name {
			return v, nil
		}
	}
	return nil, nil
}

func sshString(login, ip string) string {
	if ip == "" {
		return ""
	}
	return fmt.Sprintf("ssh %s@%s", firstNonEmpty(login, "user1"), ip)
}

// generatePassword returns a random password of n chars that is GUARANTEED to
// contain at least one lowercase, one uppercase, one digit and one special
// character. Cloud.ru rejects passwords without a digit
// ("the password must contain at least one number"), so a purely random draw is
// not safe — we seed one char from each class, then fill and shuffle.
// Ambiguous characters (0/O/1/l/I) are avoided.
func generatePassword(n int) string {
	if n < 8 {
		n = 8
	}
	const (
		lower   = "abcdefghijkmnpqrstuvwxyz"
		upper   = "ABCDEFGHJKLMNPQRSTUVWXYZ"
		digits  = "23456789"
		special = "!@#%-_+="
	)
	all := lower + upper + digits + special

	pw := make([]byte, 0, n)
	pw = append(pw, pickChar(lower), pickChar(upper), pickChar(digits), pickChar(special))
	for len(pw) < n {
		pw = append(pw, pickChar(all))
	}
	// Fisher-Yates shuffle so the seeded classes aren't in fixed positions.
	for i := len(pw) - 1; i > 0; i-- {
		j := randIntn(i + 1)
		pw[i], pw[j] = pw[j], pw[i]
	}
	return string(pw)
}

func pickChar(set string) byte {
	return set[randIntn(len(set))]
}

func randIntn(n int) int {
	if n <= 0 {
		return 0
	}
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0
	}
	return int(v.Int64())
}
