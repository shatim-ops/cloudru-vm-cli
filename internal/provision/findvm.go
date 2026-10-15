package provision

import (
	"context"
	"time"
)

// createdVMLookupTimeout bounds FindCreatedVM: it runs after a failed or
// cancelled create, often with the caller's context already done.
const createdVMLookupTimeout = 30 * time.Second

// createdVMClockSkew is how much earlier than the create call a VM's
// created_time may read and still count as made by that call.
const createdVMClockSkew = 5 * time.Minute

// FindCreatedVM looks for the VM a CreateVM call may have made although it
// returned an error: the platform can create the VM and the answer be lost
// (timeout, Ctrl-C, a dropped connection). The VM is matched by name and by
// a creation time no earlier than since (less clock skew), so an older VM
// that happens to share the name is not taken for it. It uses its own
// context and returns nil when nothing matches or the lookup fails.
func FindCreatedVM(cloud CloudAPI, projectID, name string, since time.Time) *VM {
	ctx, cancel := context.WithTimeout(context.Background(), createdVMLookupTimeout)
	defer cancel()
	vms, err := cloud.ListVMsInProject(ctx, projectID)
	if err != nil {
		return nil
	}
	for _, vm := range vms {
		if vm == nil || vm.ID == "" || vm.Name != name || vm.CreatedAt.IsZero() {
			continue
		}
		if vm.CreatedAt.After(since.Add(-createdVMClockSkew)) {
			return vm
		}
	}
	return nil
}
