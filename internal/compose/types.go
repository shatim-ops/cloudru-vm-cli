package compose

// PortMapping represents a host:container port pair.
type PortMapping struct {
	Host      int
	Container int
	Protocol  string // "tcp" or "udp", default "tcp"
}

// Resources represents aggregated CPU/memory needs.
type Resources struct {
	CPUCores float64
	MemoryMB int
}

// Service represents a single service from docker-compose.yml.
type Service struct {
	Name      string
	Image     string
	Ports     []PortMapping
	Resources Resources
	DependsOn []string
}

// Project represents a parsed docker-compose.yml.
type Project struct {
	Services  []Service
	Ports     []int    // deduplicated host ports
	Resources Resources // aggregated across all services
	Hash      string   // SHA-256 of raw file content
}
