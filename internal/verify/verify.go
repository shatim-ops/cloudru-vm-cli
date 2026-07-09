package verify

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// ProbeResult holds the outcome of a single health check.
type ProbeResult struct {
	Port       int
	StatusCode int
	Healthy    bool
	Latency    time.Duration
	Error      string // non-empty if probe failed
}

// Check performs HTTP health probes against the given IP on each port.
// Returns one ProbeResult per port. Failures are recorded but not returned as errors
// (health checks are non-blocking warnings).
func Check(ctx context.Context, ip string, ports []int, timeout time.Duration) []ProbeResult {
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	results := make([]ProbeResult, len(ports))
	for i, port := range ports {
		results[i] = probe(ctx, ip, port, timeout)
	}
	return results
}

func probe(ctx context.Context, ip string, port int, timeout time.Duration) ProbeResult {
	result := ProbeResult{Port: port}

	client := &http.Client{Timeout: timeout}

	url := fmt.Sprintf("http://%s:%d/", ip, port)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		result.Error = err.Error()
		return result
	}

	start := time.Now()
	resp, err := client.Do(req)
	result.Latency = time.Since(start)

	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer resp.Body.Close()

	result.StatusCode = resp.StatusCode
	result.Healthy = resp.StatusCode >= 200 && resp.StatusCode < 400

	return result
}
