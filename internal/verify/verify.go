package verify

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
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
	result, err := get(ctx, "http", ip, port, timeout)
	// An HTTPS-only port answers plain HTTP with 400 (nginx, Go) or a TLS
	// alert that is not HTTP at all. Ask it over TLS; keep the plain result
	// if that does not work either. A timeout is not retried: it would
	// double the wait for a service that does not answer.
	var netErr net.Error
	timedOut := errors.As(err, &netErr) && netErr.Timeout()
	if (err != nil && !timedOut && ctx.Err() == nil) || result.StatusCode == http.StatusBadRequest {
		if tlsResult, tlsErr := get(ctx, "https", ip, port, timeout); tlsErr == nil {
			return tlsResult
		}
	}
	return result
}

// get probes scheme://ip:port/ once. Redirects are not followed: a 3xx is
// the service answering, and following it would probe another host (often
// https:// with a certificate for a name, not for this IP). Certificates are
// not checked for the same reason — this is a liveness check, not trust.
func get(ctx context.Context, scheme, ip string, port int, timeout time.Duration) (ProbeResult, error) {
	result := ProbeResult{Port: port}

	client := &http.Client{
		Timeout:       timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	if scheme == "https" {
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // liveness probe of an IP
		client.Transport = tr
		defer tr.CloseIdleConnections()
	}

	url := fmt.Sprintf("%s://%s/", scheme, net.JoinHostPort(ip, strconv.Itoa(port)))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		result.Error = err.Error()
		return result, err
	}

	start := time.Now()
	resp, err := client.Do(req)
	result.Latency = time.Since(start)

	if err != nil {
		result.Error = err.Error()
		return result, err
	}
	defer resp.Body.Close()

	result.StatusCode = resp.StatusCode
	result.Healthy = resp.StatusCode >= 200 && resp.StatusCode < 400

	return result, nil
}
