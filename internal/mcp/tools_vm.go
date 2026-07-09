package mcp

import (
	"context"
	"fmt"
)

// handleVMGet returns LIVE details for a single VM (by vm_id or name): its
// private IP, any public/floating IP, state, and every network interface.
//
// This closes a real gap: the `list` and `status` tools read local state and do
// NOT expose a VM's private IP, so an agent that needed a freshly-created VM's
// private address (e.g. to wire a service endpoint) had no tool for it and would
// fall back to raw shell/API probing. GetVMDetail already carries the data
// (interfaces[].ip_address); this tool surfaces and normalizes it.
//
// IP normalization is grounded in the real API shape:
//   - a "regular" interface's ip_address is the PRIVATE (subnet) address;
//   - a "direct_ip" interface's ip_address is a directly-attached PUBLIC address;
//   - a floating_ip (when present on a regular interface) is its PUBLIC address.
//
// Read-only; touches eng.Cloud, so it uses authedEngine.
func (s *Server) handleVMGet(ctx context.Context, args map[string]any) (string, bool) {
	ref := stringArg(args, "vm_id")
	if ref == "" {
		ref = stringArg(args, "name")
	}
	if ref == "" {
		return errResult(fmt.Errorf("vm_get requires vm_id or name")), true
	}

	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return errResult(err), true
	}

	// Accept either a UUID or a human name.
	vmID := ref
	if !looksUUID(ref) {
		id, _, e := s.resolveVM(ctx, ref, eng.Config.ProjectID)
		if e != nil {
			return errResult(e), true
		}
		vmID = id
	}

	det, err := eng.Cloud.GetVMDetail(ctx, vmID)
	if err != nil {
		return errResult(err), true
	}

	var privateIP, publicIP string
	ifaces := make([]map[string]any, 0, len(det.Interfaces))
	for _, in := range det.Interfaces {
		row := map[string]any{
			"id":         in.ID,
			"name":       in.Name,
			"ip_address": in.IPAddress,
			"type":       in.Type,
			"primary":    in.Primary,
			"state":      in.State,
		}
		if in.FloatingIP != nil && in.FloatingIP.IPAddress != "" {
			row["floating_ip"] = in.FloatingIP.IPAddress
			if publicIP == "" || in.Primary {
				publicIP = in.FloatingIP.IPAddress
			}
		}
		if in.Type == "direct_ip" {
			// The interface IP itself is the public address.
			if in.IPAddress != "" && (publicIP == "" || in.Primary) {
				publicIP = in.IPAddress
			}
		} else if in.IPAddress != "" && (privateIP == "" || in.Primary) {
			// "regular" (or unspecified) => subnet-internal private IP.
			privateIP = in.IPAddress
		}
		ifaces = append(ifaces, row)
	}

	return toJSON(map[string]any{
		"id":         det.ID,
		"name":       det.Name,
		"state":      det.State,
		"private_ip": privateIP,
		"public_ip":  publicIP,
		"interfaces": ifaces,
	}), false
}
