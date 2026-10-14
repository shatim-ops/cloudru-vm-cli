package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/shatim-ops/cloudru-vm-cli/internal/confirm"
	"github.com/shatim-ops/cloudru-vm-cli/internal/deployplan"
	"github.com/shatim-ops/cloudru-vm-cli/internal/provision"
	"github.com/shatim-ops/cloudru-vm-cli/internal/state"
)

// costNote goes next to every price shown before an operation: Cloud.ru has
// no public price API, internal/pricing is a rate card.
const costNote = "estimate, not a bill"

// errDryRun stops provisionVM before the VM is created; Deploy turns it into
// success.
var errDryRun = errors.New("dry run")

// confirmOptions wires confirm.Ask to the engine's printer: results to its
// writer, previews and prompts to its error writer.
func (e *Engine) confirmOptions(yes, dryRun, noPrompt bool) confirm.Options {
	return confirm.Options{
		Yes:      yes,
		DryRun:   dryRun,
		NoPrompt: noPrompt,
		JSON:     e.Printer.IsJSON(),
		Out:      e.Printer.Writer(),
		Err:      e.Printer.ErrWriter(),
	}
}

// deployPreview describes the VM about to be created, priced with the same
// estimator as the MCP deploy_plan. Lookups of names are best effort: the
// preview falls back to the IDs rather than failing the deploy.
func (e *Engine) deployPreview(ctx context.Context, req provision.CreateVMRequest, img *provision.ImageDetail, network vmNetwork, ports []int) confirm.Preview {
	vm := deployplan.VMSpec{
		Name:   req.Name,
		Flavor: deployplan.FlavorSpec{Name: req.FlavorName},
		Image:  deployplan.RefSpec{Name: req.ImageName},
		Zone:   deployplan.RefSpec{ID: req.AvailabilityZoneID, Name: req.AvailabilityZoneID},
	}
	if img != nil {
		vm.Image.ID = img.ID
	}
	if flavors, err := e.Cloud.ListFlavors(ctx); err == nil {
		for _, f := range flavors {
			if f.Name == req.FlavorName || f.ID == req.FlavorName {
				vm.Flavor = deployplan.FlavorSpec{ID: f.ID, Name: f.Name, VCPUs: f.VCPUs, RAMGB: f.RAMGB}
				break
			}
		}
	}
	if zones, err := e.Cloud.ListAvailabilityZones(ctx); err == nil {
		for _, z := range zones {
			if z.ID == req.AvailabilityZoneID {
				vm.Zone.Name = z.Name
				break
			}
		}
	}
	if img != nil && img.Name != "" {
		vm.Image.Name = img.Name
	}
	if len(req.Disks) > 0 {
		vm.Disk = deployplan.DiskSpec{SizeGB: req.Disks[0].Size, Type: req.Disks[0].DiskTypeName}
	}
	cost := deployplan.EstimateVM(vm)

	flavor := vm.Flavor.Name
	if vm.Flavor.VCPUs > 0 {
		flavor += fmt.Sprintf(" (%d vCPU, %d GB RAM)", vm.Flavor.VCPUs, vm.Flavor.RAMGB)
	}
	costLine := fmt.Sprintf("cost:      ~%.2f %s/month (compute %.2f + disk %.2f), %s",
		cost.TotalMonthlyRub, cost.Currency, cost.ComputeMonthlyRub, cost.DiskMonthlyRub, costNote)
	if vm.Flavor.VCPUs == 0 {
		costLine += "; flavor size unknown, compute may be missing from the total"
	}

	lines := []string{
		"flavor:    " + flavor,
		"zone:      " + refText(vm.Zone),
		"image:     " + refText(vm.Image),
		fmt.Sprintf("disk:      %d GB boot disk (priced at the SSD rate)", vm.Disk.SizeGB),
		fmt.Sprintf("network:   subnet %s, security group %s", network.SubnetName, network.SecurityGroup),
		"public IP: a floating IP will be created and attached",
	}
	if len(ports) > 0 {
		ps := make([]string, len(ports))
		for i, p := range ports {
			ps[i] = fmt.Sprint(p)
		}
		lines = append(lines, fmt.Sprintf("ports:     %s opened to 0.0.0.0/0 in security group %s (shared by every VM in it)",
			strings.Join(ps, ","), network.SecurityGroup))
	}
	lines = append(lines, costLine)

	return confirm.Preview{
		Action:  "deploy",
		Summary: fmt.Sprintf("create VM %s (%s) in %s", vm.Name, vm.Flavor.Name, vm.Zone.Name),
		Lines:   lines,
		Details: map[string]any{
			"vm":             vm,
			"floating_ip":    true,
			"subnet_id":      network.SubnetID,
			"security_group": map[string]string{"id": network.SecurityGroupID, "name": network.SecurityGroup},
			"open_ports":     ports,
			"cost":           cost,
			"cost_note":      costNote,
		},
	}
}

// redeployPreview describes an update of an already running VM.
func redeployPreview(rec *state.DeploymentRecord) confirm.Preview {
	return confirm.Preview{
		Action:  "deploy",
		Summary: fmt.Sprintf("update VM %s (%s)", rec.VMID, rec.VMIP),
		Lines: []string{
			"upload the compose file to the existing VM and run 'docker compose up -d'",
			"no new resources are created",
		},
		Details: map[string]any{"vm_id": rec.VMID, "vm_ip": rec.VMIP, "creates": []string{}},
	}
}

// destroyResource is one entry of the destroy preview.
type destroyResource struct {
	Type    string `json:"type"`
	ID      string `json:"id,omitempty"`
	Address string `json:"address,omitempty"`
	Action  string `json:"action"`
	Note    string `json:"note,omitempty"`
}

// destroyPreview lists what destroy deletes and what it leaves behind.
func destroyPreview(rec *state.DeploymentRecord) confirm.Preview {
	res := []destroyResource{
		{Type: "vm", ID: rec.VMID, Address: rec.VMIP, Action: "delete", Note: "all data on its disks is lost"},
	}
	lines := []string{fmt.Sprintf("delete VM %s and all data on its disks", rec.VMID)}
	if rec.VMIP != "" || rec.FloatingIPID != "" {
		// The public address of a deployed VM is a floating IP; destroy
		// releases it after the VM (by floating_ip_id, else by address).
		res = append(res, destroyResource{Type: "floating_ip", ID: rec.FloatingIPID, Address: rec.VMIP, Action: "release"})
		lines = append(lines, fmt.Sprintf("release floating IP %s", fipLabel(rec.VMIP, rec.FloatingIPID)))
	}
	res = append(res, destroyResource{Type: "state_record", ID: rec.ID, Action: "remove"})
	lines = append(lines, "remove the deployment from local state")

	summary := "destroy VM " + rec.VMID
	if rec.Region != "" {
		summary += " in " + rec.Region
	}
	return confirm.Preview{
		Action:  "destroy",
		Summary: summary,
		Lines:   lines,
		Details: map[string]any{"resources": res},
	}
}

func refText(r deployplan.RefSpec) string {
	if r.ID == "" {
		return r.Name
	}
	if r.Name == "" || r.Name == r.ID {
		return r.ID
	}
	return fmt.Sprintf("%s (%s)", r.Name, r.ID)
}
