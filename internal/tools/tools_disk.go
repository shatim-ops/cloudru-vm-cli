package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/deployplan"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// looksUUID is a cheap check to tell a UUID from a human name.
func looksUUID(s string) bool {
	return len(s) == 36 && strings.Count(s, "-") == 4
}

// resolveVM finds a VM by id or name, returning (id, zoneID).
func (s *Registry) resolveVM(ctx context.Context, ref, projectID string) (string, string, error) {
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return "", "", err
	}
	vms, err := eng.Cloud.ListVMsInProject(ctx, projectID)
	if err != nil {
		return "", "", err
	}
	for _, v := range vms {
		if v.ID == ref || v.Name == ref {
			return v.ID, v.ZoneID(), nil
		}
	}
	return "", "", fmt.Errorf("VM %q not found", ref)
}

// handleDiskList lists disks (block storage) with a compact projection.
func (s *Registry) handleDiskList(ctx context.Context, args map[string]any) (string, bool) {
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	disks, err := eng.Cloud.ListDisks(ctx, eng.Config.ProjectID)
	if err != nil {
		return errResult(err), true
	}
	out := make([]map[string]any, 0, len(disks))
	for _, d := range disks {
		row := map[string]any{
			"id": d["id"], "name": d["name"], "size_gb": d["size"],
			"state": d["state"], "bootable": d["bootable"],
		}
		if dt, ok := d["disk_type"].(map[string]any); ok {
			row["disk_type"] = dt["name"]
		}
		if az, ok := d["availability_zone"].(map[string]any); ok {
			row["zone"] = az["name"]
		}
		// attached VMs
		if vms, ok := d["vms"].([]any); ok && len(vms) > 0 {
			names := make([]any, 0, len(vms))
			for _, x := range vms {
				if vm, ok := x.(map[string]any); ok {
					names = append(names, vm["name"])
				}
			}
			row["attached_to"] = names
		}
		out = append(out, row)
	}
	return toJSON(map[string]any{"disks": out}), false
}

// handleDiskCreate creates a disk. If attach_to (a VM) is given, the disk is
// created in that VM's zone and (with confirmed) attached to it — the ergonomic
// "add a disk to my VM" path. Mutation: requires confirmed=true.
func (s *Registry) handleDiskCreate(ctx context.Context, args map[string]any) (string, bool) {
	name := stringArg(args, "name")
	sizeGB := intArg(args, "size_gb", 0)
	if name == "" || sizeGB <= 0 {
		return usageResult(fmt.Errorf("'name' and 'size_gb' (>0) are required")), true
	}
	if sizeGB > provision.MaxDiskGB {
		return usageResult(fmt.Errorf("size_gb %d is over the disk maximum of %d GB", sizeGB, provision.MaxDiskGB)), true
	}
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	pid := eng.Config.ProjectID

	// Resolve the zone: from the attach-target VM (zones must match), else an
	// explicit zone, else the configured region.
	attachTo := stringArg(args, "attach_to")
	var zoneID, vmID string
	if attachTo != "" {
		vmID, zoneID, err = s.resolveVM(ctx, attachTo, pid)
		if err != nil {
			return errResult(err), true
		}
	} else if z := stringArg(args, "zone"); z != "" {
		if looksUUID(z) {
			zoneID = z
		} else if zs, e := eng.Cloud.ListAvailabilityZones(ctx); e == nil {
			for _, az := range zs {
				if az.Name == z {
					zoneID = az.ID
					break
				}
			}
		}
		if zoneID == "" {
			return errResult(fmt.Errorf("zone %q not found", z)), true
		}
	} else {
		zoneID = eng.Config.Region
	}

	// Resolve disk type (name or id); default to an SSD type.
	req := provision.CreateDiskReq{Name: name, Size: sizeGB, AvailabilityZoneID: zoneID, ProjectID: pid}
	if dt := stringArg(args, "disk_type"); dt != "" {
		if looksUUID(dt) {
			req.DiskTypeID = dt
		} else {
			req.DiskTypeName = dt
		}
	} else if dts, e := eng.Cloud.ListDiskTypes(ctx, pid); e == nil {
		for _, t := range dts {
			if strings.Contains(strings.ToLower(t.Name), "ssd") {
				req.DiskTypeName = t.Name
				break
			}
		}
		if req.DiskTypeName == "" && len(dts) > 0 {
			req.DiskTypeName = dts[0].Name
		}
	}

	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status":  "confirmation_required",
			"message": "disk_create makes a billable disk. Approve, then call again with confirmed=true.",
			"will_create": map[string]any{
				"name": name, "size_gb": sizeGB, "disk_type": firstNonEmptyStr(req.DiskTypeName, req.DiskTypeID),
				"zone_id": zoneID, "attach_to": attachTo,
			},
			"preview": diskCreatePreview(eng, req, attachTo, stringArg(args, "zone")),
		}), true
	}

	disk, err := eng.Cloud.CreateDisk(ctx, req)
	if err != nil {
		return errResult(fmt.Errorf("create disk: %w", err)), true
	}
	res := map[string]any{"status": "created", "disk": disk}

	// Auto-attach if requested.
	if attachTo != "" && vmID != "" {
		diskID, _ := disk["id"].(string)
		if diskID == "" {
			res["attach"] = "skipped: could not read new disk id from create response"
		} else if at, e := eng.Cloud.AttachDisk(ctx, diskID, vmID, pid); e != nil {
			res["attach"] = "FAILED: " + e.Error() + " — attach manually with disk_attach"
		} else {
			res["attach"] = at
			res["note"] = "Disk created and attached. On the VM: partition/format the new device (e.g. mkfs.ext4 /dev/vdX) and mount it — attaching does NOT grow the existing filesystem."
		}
	}
	return toJSON(res), false
}

// handleDiskAttach attaches an existing disk to a VM.
func (s *Registry) handleDiskAttach(ctx context.Context, args map[string]any) (string, bool) {
	diskID := stringArg(args, "disk_id")
	vmRef := stringArg(args, "vm")
	if diskID == "" || vmRef == "" {
		return usageResult(fmt.Errorf("'disk_id' and 'vm' are required")), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{"status": "confirmation_required", "message": "disk_attach changes a VM. Approve, then confirmed=true."}), true
	}
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	vmID, _, err := s.resolveVM(ctx, vmRef, eng.Config.ProjectID)
	if err != nil {
		return errResult(err), true
	}
	r, err := eng.Cloud.AttachDisk(ctx, diskID, vmID, eng.Config.ProjectID)
	if err != nil {
		return errResult(fmt.Errorf("attach disk: %w", err)), true
	}
	return toJSON(map[string]any{"status": "attached", "result": r}), false
}

// handleDiskDetach detaches a disk from a VM.
func (s *Registry) handleDiskDetach(ctx context.Context, args map[string]any) (string, bool) {
	diskID := stringArg(args, "disk_id")
	vmRef := stringArg(args, "vm")
	if diskID == "" || vmRef == "" {
		return usageResult(fmt.Errorf("'disk_id' and 'vm' are required")), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{"status": "confirmation_required", "message": "disk_detach changes a VM. Approve, then confirmed=true."}), true
	}
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	vmID, _, err := s.resolveVM(ctx, vmRef, eng.Config.ProjectID)
	if err != nil {
		return errResult(err), true
	}
	r, err := eng.Cloud.DetachDisk(ctx, diskID, vmID, eng.Config.ProjectID)
	if err != nil {
		return errResult(fmt.Errorf("detach disk: %w", err)), true
	}
	return toJSON(map[string]any{"status": "detached", "result": r}), false
}

// handleDiskResize grows a disk in place (PUT {size}). Only grows; shrink and
// free-tier disks are rejected by the API. Mutation: requires confirmed=true.
func (s *Registry) handleDiskResize(ctx context.Context, args map[string]any) (string, bool) {
	diskID := stringArg(args, "disk_id")
	sizeGB := intArg(args, "size_gb", 0)
	if diskID == "" || sizeGB <= 0 {
		return usageResult(fmt.Errorf("'disk_id' and 'size_gb' (>0, larger than current) are required")), true
	}
	if sizeGB > provision.MaxDiskGB {
		return usageResult(fmt.Errorf("size_gb %d is over the disk maximum of %d GB", sizeGB, provision.MaxDiskGB)), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status":  "confirmation_required",
			"message": fmt.Sprintf("disk_resize grows disk %s to %d GB (grow only; billable). Approve, then confirmed=true. NB: after resize you must grow the partition+filesystem on the VM (e.g. growpart + resize2fs). Free-tier disks are rejected by the API.", diskID, sizeGB),
			"preview": engine.CreatePreview{Resource: engine.ResourceDiskResize, Name: diskID,
				Disk: &deployplan.DiskSpec{SizeGB: sizeGB}, CostNote: "not estimated: evo has no rate card for this resource"},
		}), true
	}
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	d, err := eng.Cloud.ResizeDisk(ctx, diskID, sizeGB, eng.Config.ProjectID)
	if err != nil {
		return errResult(fmt.Errorf("resize disk: %w", err)), true
	}
	return toJSON(map[string]any{
		"status": "resized", "disk": d,
		"note": "Disk grown. On the VM grow the partition + filesystem to use it: e.g. `growpart /dev/vda 2 && resize2fs /dev/vda2`.",
	}), false
}

// handleDiskDelete deletes a disk by id.
func (s *Registry) handleDiskDelete(ctx context.Context, args map[string]any) (string, bool) {
	diskID := stringArg(args, "disk_id")
	if diskID == "" {
		return usageResult(fmt.Errorf("'disk_id' is required")), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{"status": "confirmation_required", "message": fmt.Sprintf("disk_delete permanently destroys disk %s and its data. Approve, then confirmed=true.", diskID)}), true
	}
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	if err := eng.Cloud.DeleteDisk(ctx, diskID, eng.Config.ProjectID); err != nil {
		return errResult(fmt.Errorf("delete disk: %w", err)), true
	}
	return toJSON(map[string]any{"status": "deleted", "disk_id": diskID}), false
}

// diskCreatePreview is the preview of disk_create: the disk as resolved,
// the zone (by name when the call gave one) and its price.
func diskCreatePreview(eng *engine.Engine, req provision.CreateDiskReq, attachTo, zoneArg string) engine.CreatePreview {
	zone := &deployplan.RefSpec{ID: req.AvailabilityZoneID}
	if zoneArg != "" && !looksUUID(zoneArg) {
		zone.Name = zoneArg
	}
	p := engine.CreatePreview{Resource: engine.ResourceDisk, Name: req.Name,
		ProjectID: req.ProjectID, ProjectFrom: eng.Config.ProjectOrigin, Zone: zone,
		Disk: &deployplan.DiskSpec{SizeGB: req.Size, Type: firstNonEmptyStr(req.DiskTypeName, req.DiskTypeID)}}
	p.Add("attach_to", attachTo)
	p.Price()
	return p
}
