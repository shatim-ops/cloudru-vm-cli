package provision

import "fmt"

// DefaultBootDiskGB is the boot disk size used when none is configured.
const DefaultBootDiskGB = 10

// BootDiskGB returns the boot disk size to request: the configured size
// (DefaultBootDiskGB when unset), raised to the image's min_disk. The
// create API rejects a smaller disk with 422 vm_root_disk_too_small, and
// min_disk changes when the platform rebuilds an image (ubuntu-22.04 went
// from 10 to 11 GB). The second value explains a raise; it is empty when
// the size is unchanged.
func BootDiskGB(requested, minDisk int) (int, string) {
	size := requested
	if size <= 0 {
		size = DefaultBootDiskGB
	}
	if minDisk > size {
		note := fmt.Sprintf("boot disk raised from %d to %d GB: the image requires at least %d GB", size, minDisk, minDisk)
		return minDisk, note
	}
	return size, ""
}
