package compose

import (
	"crypto/sha256"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	clierr "github.com/shatim-ops/cloudru-vm-cli/internal/errors"
	"gopkg.in/yaml.v3"
)

// Parse reads a docker-compose.yml file at path and returns a populated Project.
func Parse(path string) (*Project, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, clierr.Wrap(
				clierr.CodeComposeNotFound,
				fmt.Sprintf("compose file not found: %s", path),
				"Check the file path and try again.",
				err,
			)
		}
		return nil, clierr.Wrap(
			clierr.CodeComposeNotFound,
			fmt.Sprintf("cannot read compose file: %s", path),
			"Check file permissions.",
			err,
		)
	}

	hash := fmt.Sprintf("%x", sha256.Sum256(data))

	var root map[string]interface{}
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, clierr.Wrap(
			clierr.CodeComposeInvalid,
			"invalid YAML in compose file",
			"Ensure the file contains valid YAML.",
			err,
		)
	}

	servicesRaw, ok := root["services"]
	if !ok {
		return nil, clierr.New(
			clierr.CodeComposeInvalid,
			"no services defined in compose file",
			"Add a 'services' section to your docker-compose.yml.",
		)
	}

	servicesMap, ok := servicesRaw.(map[string]interface{})
	if !ok || len(servicesMap) == 0 {
		return nil, clierr.New(
			clierr.CodeComposeInvalid,
			"no services defined in compose file",
			"Add a 'services' section to your docker-compose.yml.",
		)
	}

	// Sort service names for deterministic output.
	names := make([]string, 0, len(servicesMap))
	for name := range servicesMap {
		names = append(names, name)
	}
	sort.Strings(names)

	var (
		services []Service
		portSet  = make(map[int]struct{})
		totalCPU float64
		totalMem int
	)

	for _, name := range names {
		svcRaw := servicesMap[name]
		svcMap, ok := svcRaw.(map[string]interface{})
		if !ok {
			continue
		}

		svc := Service{Name: name}

		if img, ok := svcMap["image"].(string); ok {
			svc.Image = img
		}

		svc.Ports = parsePorts(svcMap)
		for _, p := range svc.Ports {
			portSet[p.Host] = struct{}{}
		}

		svc.Resources = parseResources(svcMap)
		totalCPU += svc.Resources.CPUCores
		totalMem += svc.Resources.MemoryMB

		svc.DependsOn = parseDependsOn(svcMap)

		services = append(services, svc)
	}

	if len(services) == 0 {
		return nil, clierr.New(
			clierr.CodeComposeInvalid,
			"no valid services found in compose file",
			"Ensure services are properly defined.",
		)
	}

	ports := make([]int, 0, len(portSet))
	for p := range portSet {
		ports = append(ports, p)
	}
	sort.Ints(ports)

	return &Project{
		Services:  services,
		Ports:     ports,
		Resources: Resources{CPUCores: totalCPU, MemoryMB: totalMem},
		Hash:      hash,
	}, nil
}

// parsePorts extracts port mappings from a service definition.
func parsePorts(svc map[string]interface{}) []PortMapping {
	portsRaw, ok := svc["ports"]
	if !ok {
		return nil
	}

	portsList, ok := portsRaw.([]interface{})
	if !ok {
		return nil
	}

	var mappings []PortMapping
	for _, p := range portsList {
		var raw string
		switch v := p.(type) {
		case string:
			raw = v
		case int:
			raw = strconv.Itoa(v)
		default:
			continue
		}

		pm, ok := parsePortString(raw)
		if ok {
			mappings = append(mappings, pm)
		}
	}
	return mappings
}

// parsePortString parses a port string like "8080:80", "8080:80/udp".
func parsePortString(s string) (PortMapping, bool) {
	pm := PortMapping{Protocol: "tcp"}

	// Check for protocol suffix.
	if idx := strings.LastIndex(s, "/"); idx != -1 {
		pm.Protocol = strings.ToLower(s[idx+1:])
		s = s[:idx]
	}

	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		// Single port: treat as both host and container.
		port, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil {
			return PortMapping{}, false
		}
		pm.Host = port
		pm.Container = port
		return pm, true
	}

	host, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return PortMapping{}, false
	}
	container, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return PortMapping{}, false
	}

	pm.Host = host
	pm.Container = container
	return pm, true
}

// parseResources extracts CPU and memory limits from deploy.resources.limits.
func parseResources(svc map[string]interface{}) Resources {
	deploy, ok := svc["deploy"].(map[string]interface{})
	if !ok {
		return Resources{}
	}
	resources, ok := deploy["resources"].(map[string]interface{})
	if !ok {
		return Resources{}
	}
	limits, ok := resources["limits"].(map[string]interface{})
	if !ok {
		return Resources{}
	}

	var res Resources

	if cpus, ok := limits["cpus"]; ok {
		res.CPUCores = parseFloat(cpus)
	}

	if mem, ok := limits["memory"]; ok {
		res.MemoryMB = parseMemory(mem)
	}

	return res
}

// parseFloat converts a YAML value (string or number) to float64.
func parseFloat(v interface{}) float64 {
	switch val := v.(type) {
	case string:
		f, _ := strconv.ParseFloat(val, 64)
		return f
	case float64:
		return val
	case int:
		return float64(val)
	default:
		return 0
	}
}

// parseMemory converts a memory string like "256M" or "1G" to megabytes.
func parseMemory(v interface{}) int {
	s, ok := v.(string)
	if !ok {
		if n, ok := v.(int); ok {
			return n
		}
		return 0
	}

	s = strings.TrimSpace(s)
	if len(s) == 0 {
		return 0
	}

	suffix := strings.ToUpper(s[len(s)-1:])
	numStr := s[:len(s)-1]

	switch suffix {
	case "G":
		n, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			return 0
		}
		return int(n * 1024)
	case "M":
		n, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			return 0
		}
		return int(n)
	default:
		// Try parsing as plain number (bytes or MB).
		n, err := strconv.Atoi(s)
		if err != nil {
			return 0
		}
		return n
	}
}

// parseDependsOn extracts the depends_on list from a service definition.
func parseDependsOn(svc map[string]interface{}) []string {
	dep, ok := svc["depends_on"]
	if !ok {
		return nil
	}

	switch v := dep.(type) {
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, d := range v {
			if s, ok := d.(string); ok {
				result = append(result, s)
			}
		}
		return result
	case map[string]interface{}:
		result := make([]string, 0, len(v))
		for name := range v {
			result = append(result, name)
		}
		sort.Strings(result)
		return result
	default:
		return nil
	}
}
