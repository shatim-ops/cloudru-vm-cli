package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dzhechko/cloudru-vm-cli/internal/provision"
)

// resolveZoneID turns a zone name or UUID into a zone UUID, falling back to the
// configured region when ref is empty.
func (s *Server) resolveZoneID(ctx context.Context, ref, fallback string) (string, error) {
	if ref == "" {
		return fallback, nil
	}
	if looksUUID(ref) {
		return ref, nil
	}
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return "", err
	}
	zones, err := eng.Cloud.ListAvailabilityZones(ctx)
	if err != nil {
		return "", err
	}
	for _, z := range zones {
		if z.Name == ref {
			return z.ID, nil
		}
	}
	return "", fmt.Errorf("zone %q not found", ref)
}

// handleImageCreate builds a VM image from a bootable disk — the last step of
// producing a distributable image (e.g. a Marketplace product). The source disk
// must be bootable AND detached: power the VM off, disk_detach, wait for state
// "available". A stopped-but-attached disk is refused with
// invalid_disk_state_to_create_image.
// Mutation + storage cost: requires confirmed=true.
func (s *Server) handleImageCreate(ctx context.Context, args map[string]any) (string, bool) {
	name := stringArg(args, "name")
	diskID := stringArg(args, "disk_id")
	if name == "" || diskID == "" {
		return errResult(fmt.Errorf("'name' and 'disk_id' are required")), true
	}
	req := provision.CreateImageReq{
		Name:             name,
		DisplayName:      stringArg(args, "display_name"),
		Description:      stringArg(args, "description"),
		DiskID:           diskID,
		MinCPU:           intArg(args, "min_cpu", 0),
		MinRAM:           intArg(args, "min_ram", 0),
		MinDisk:          intArg(args, "min_disk", 0),
		UserDataTemplate: stringArg(args, "user_data_template"),
	}

	// image_metadata declares the fields a buyer fills in when creating a VM;
	// accepted as a JSON array so the caller can pass the full field spec.
	if raw, ok := args["image_metadata"]; ok && raw != nil {
		blob, err := json.Marshal(raw)
		if err != nil {
			return errResult(fmt.Errorf("image_metadata: %w", err)), true
		}
		if err := json.Unmarshal(blob, &req.ImageMetadata); err != nil {
			return errResult(fmt.Errorf("image_metadata must be an array of field objects: %w", err)), true
		}
	}

	// The gate comes before authentication so an unapproved call never reaches
	// the cloud at all — the same order every other mutating tool uses. The zone
	// is therefore echoed as asked for, not as resolved.
	zoneRef := stringArg(args, "zone")
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status":  "confirmation_required",
			"message": "image_create makes a billable image from a disk. Approve, then call again with confirmed=true.",
			"will_create": map[string]any{
				"name": name, "from_disk": diskID,
				"zone":    firstNonEmptyStr(zoneRef, "configured region"),
				"min_cpu": req.MinCPU, "min_ram": req.MinRAM, "min_disk": req.MinDisk,
				"has_user_data_template": req.UserDataTemplate != "",
				"metadata_fields":        len(req.ImageMetadata),
			},
		}), true
	}

	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	req.ProjectID = eng.Config.ProjectID

	zoneID, err := s.resolveZoneID(ctx, zoneRef, eng.Config.Region)
	if err != nil {
		return errResult(err), true
	}
	req.AvailZones = []provision.ImageAZTarget{{AvailabilityZoneID: zoneID}}

	img, err := eng.Cloud.CreateImage(ctx, req)
	if err != nil {
		return errResult(fmt.Errorf("create image: %w", err)), true
	}
	return toJSON(map[string]any{
		"status": "building",
		"image":  img,
		"note": "Image build is queued, not finished: it runs at roughly 0.7 GB per minute. " +
			"Poll list_images until the image reports state 'created'/'active' before booting from it.",
	}), false
}

// handleFipAttach binds an existing free floating IP to a VM's primary
// interface. Needed whenever a VM ends up without a public address — e.g. when
// provisioning from a large custom image takes longer than the provision
// wizard's wait and it returns before assigning one.
// Mutation: requires confirmed=true.
func (s *Server) handleFipAttach(ctx context.Context, args map[string]any) (string, bool) {
	fipRef := stringArg(args, "fip")
	vmRef := stringArg(args, "vm")
	if fipRef == "" || vmRef == "" {
		return errResult(fmt.Errorf("'fip' (address or UUID) and 'vm' (name or UUID) are required")), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status":  "confirmation_required",
			"message": "fip_attach gives the VM a public address. Approve, then call again with confirmed=true.",
			"will_attach": map[string]any{"fip": fipRef, "vm": vmRef},
		}), true
	}
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	pid := eng.Config.ProjectID

	// Resolve the floating IP by address or UUID, and refuse one already in use.
	fips, err := eng.Cloud.ListFloatingIPs(ctx, pid)
	if err != nil {
		return errResult(fmt.Errorf("list floating ips: %w", err)), true
	}
	fipID, fipName := "", ""
	for _, f := range fips {
		if f.ID == fipRef || f.IPAddress == fipRef {
			fipID, fipName = f.ID, f.Name
			break
		}
	}
	if fipID == "" {
		return errResult(fmt.Errorf("floating IP %q not found", fipRef)), true
	}

	// Find the VM's primary interface — that is what the address binds to.
	vmID, _, err := s.resolveVM(ctx, vmRef, pid)
	if err != nil {
		return errResult(err), true
	}
	detail, err := eng.Cloud.GetVMDetail(ctx, vmID)
	if err != nil {
		return errResult(fmt.Errorf("get vm detail: %w", err)), true
	}
	ifaceID := ""
	for _, itf := range detail.Interfaces {
		if itf.Primary {
			ifaceID = itf.ID
			break
		}
	}
	if ifaceID == "" && len(detail.Interfaces) > 0 {
		ifaceID = detail.Interfaces[0].ID
	}
	if ifaceID == "" {
		return errResult(fmt.Errorf("VM %s has no interface to attach to", vmRef)), true
	}

	// The API rejects an empty name ("Value should have at least 1 item"), so
	// fall back to the address's existing name, then to the VM's.
	name := fipName
	if name == "" {
		name = "fip-" + detail.Name
	}
	res, err := eng.Cloud.AttachFloatingIP(ctx, fipID, pid, ifaceID, name)
	if err != nil {
		return errResult(fmt.Errorf("attach floating ip: %w", err)), true
	}
	return toJSON(map[string]any{"status": "attached", "floating_ip": res, "vm_id": vmID, "interface_id": ifaceID}), false
}

// handleImageGet returns one image with its full spec: the cloud-init template
// the platform renders on VM creation and the metadata fields a buyer fills in.
// Read-only — also the way to watch an image build finish, and to study how
// existing Marketplace images are put together.
func (s *Server) handleImageGet(ctx context.Context, args map[string]any) (string, bool) {
	imageID := stringArg(args, "image_id")
	if imageID == "" {
		return errResult(fmt.Errorf("'image_id' is required")), true
	}
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	img, err := eng.Cloud.GetImage(ctx, imageID)
	if err != nil {
		return errResult(fmt.Errorf("get image: %w", err)), true
	}
	return toJSON(map[string]any{"image": img}), false
}

// handleImageDelete removes an image. Mutation: requires confirmed=true.
func (s *Server) handleImageDelete(ctx context.Context, args map[string]any) (string, bool) {
	imageID := stringArg(args, "image_id")
	if imageID == "" {
		return errResult(fmt.Errorf("'image_id' is required")), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status":  "confirmation_required",
			"message": "image_delete permanently removes the image. VMs already running keep working, but no new VM can be created from it. Approve, then confirmed=true.",
			"will_delete": map[string]any{"image_id": imageID},
		}), true
	}
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}
	if err := eng.Cloud.DeleteImage(ctx, imageID, eng.Config.ProjectID); err != nil {
		return errResult(fmt.Errorf("delete image: %w", err)), true
	}
	return toJSON(map[string]any{"status": "deleted", "image_id": imageID}), false
}
