package sizer

import (
	"strings"

	"github.com/dzhechko/cloudru-vm-cli/internal/compose"
)

// ImageDefaults maps well-known container images to estimated resource needs.
var ImageDefaults = map[string]compose.Resources{
	"postgres":  {CPUCores: 1.0, MemoryMB: 512},
	"mysql":     {CPUCores: 1.0, MemoryMB: 512},
	"redis":     {CPUCores: 0.5, MemoryMB: 256},
	"mongo":     {CPUCores: 1.0, MemoryMB: 512},
	"nginx":     {CPUCores: 0.25, MemoryMB: 128},
	"httpd":     {CPUCores: 0.25, MemoryMB: 128},
	"rabbitmq":  {CPUCores: 0.5, MemoryMB: 256},
	"memcached": {CPUCores: 0.25, MemoryMB: 128},
}

// fallback resources for images not found in ImageDefaults.
var fallbackResources = compose.Resources{CPUCores: 0.5, MemoryMB: 256}

// EstimateFromServices calculates total resources by looking up image defaults
// for services that don't have explicit resource limits.
func EstimateFromServices(services []compose.Service) compose.Resources {
	var total compose.Resources
	for _, svc := range services {
		r := resourcesForService(svc)
		total.CPUCores += r.CPUCores
		total.MemoryMB += r.MemoryMB
	}
	return total
}

// resourcesForService returns explicit resources if set, otherwise looks up
// the image in ImageDefaults, falling back to a conservative default.
func resourcesForService(svc compose.Service) compose.Resources {
	if svc.Resources.CPUCores > 0 || svc.Resources.MemoryMB > 0 {
		return svc.Resources
	}
	base := baseImage(svc.Image)
	if defaults, ok := ImageDefaults[base]; ok {
		return defaults
	}
	return fallbackResources
}

// baseImage strips the tag (and registry prefix path) from an image reference,
// returning just the short image name. For example:
//
//	"postgres:15"             -> "postgres"
//	"library/redis:7-alpine"  -> "redis"
func baseImage(image string) string {
	// Remove tag or digest.
	if idx := strings.LastIndex(image, ":"); idx != -1 {
		image = image[:idx]
	}
	// Take the last path component (handles registry/org prefixes).
	if idx := strings.LastIndex(image, "/"); idx != -1 {
		image = image[idx+1:]
	}
	return image
}
