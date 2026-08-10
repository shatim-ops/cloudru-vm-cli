package provision

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	clierrors "github.com/dzhechko/cloudru-vm-cli/internal/errors"
)

const (
	defaultBaseURL    = "https://compute.api.cloud.ru"
	flavorCacheTTL    = 1 * time.Hour
	maxRetries        = 3
	initialBackoff    = 1 * time.Second
	backoffMultiplier = 2
)

// CloudRuAdapter implements CloudAPI using the Cloud.ru Compute REST API.
type CloudRuAdapter struct {
	client    *http.Client
	baseURL   string
	token     string
	mu        sync.RWMutex
	flavors   []*Flavor
	flavorExp time.Time
}

// NewCloudRuAdapter creates a new adapter. If client is nil, http.DefaultClient is used.
// If baseURL is empty, the default Cloud.ru compute endpoint is used.
func NewCloudRuAdapter(client *http.Client, baseURL string) *CloudRuAdapter {
	if client == nil {
		client = http.DefaultClient
	}
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &CloudRuAdapter{
		client:  client,
		baseURL: baseURL,
	}
}

// SetToken updates the bearer token used for API calls.
func (a *CloudRuAdapter) SetToken(token string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.token = token
}

func (a *CloudRuAdapter) getToken() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.token
}

// CreateVM creates a new virtual machine via POST /api/v1.1/vms.
// The API expects a JSON array of VM specs and returns an array of created VMs.
func (a *CloudRuAdapter) CreateVM(ctx context.Context, req CreateVMRequest) (*VM, error) {
	body, err := json.Marshal([]CreateVMRequest{req})
	if err != nil {
		return nil, clierrors.Wrap(clierrors.CodeAPIError, "failed to marshal create request", "Check request parameters", err)
	}

	resp, err := a.doWithRetry(ctx, http.MethodPost, "/api/v1.1/vms", body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, clierrors.Wrap(clierrors.CodeAPIError, "failed to read create response", "API returned unexpected format", err)
	}

	// Try array first, then single object.
	var vms []VM
	if err := json.Unmarshal(respBody, &vms); err == nil && len(vms) > 0 {
		return &vms[0], nil
	}

	var vm VM
	if err := json.Unmarshal(respBody, &vm); err == nil && vm.ID != "" {
		return &vm, nil
	}

	return nil, clierrors.New(clierrors.CodeAPIError, "create VM returned unexpected response: "+string(respBody), "Check API documentation")
}

// GetVM retrieves a virtual machine by ID via GET /api/v1/vms/{id}.
func (a *CloudRuAdapter) GetVM(ctx context.Context, id string) (*VM, error) {
	path := fmt.Sprintf("/api/v1/vms/%s", id)
	resp, err := a.doWithRetry(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var vm VM
	if err := json.NewDecoder(resp.Body).Decode(&vm); err != nil {
		return nil, clierrors.Wrap(clierrors.CodeAPIError, "failed to decode VM response", "API returned unexpected format", err)
	}
	return &vm, nil
}

// DeleteVM deletes a virtual machine by ID via DELETE /api/v1/vms/{id}.
func (a *CloudRuAdapter) DeleteVM(ctx context.Context, id string) error {
	path := fmt.Sprintf("/api/v1/vms/%s", id)
	resp, err := a.doWithRetry(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// ListVMs lists all virtual machines via GET /api/v1/vms.
func (a *CloudRuAdapter) ListVMs(ctx context.Context) ([]*VM, error) {
	resp, err := a.doWithRetry(ctx, http.MethodGet, "/api/v1/vms", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var page paginatedResponse[VM]
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return nil, clierrors.Wrap(clierrors.CodeAPIError, "failed to decode VM list response", "API returned unexpected format", err)
	}
	result := make([]*VM, len(page.Items))
	for i := range page.Items {
		result[i] = &page.Items[i]
	}
	return result, nil
}

// ListFlavors lists available flavors via GET /api/v1/flavors.
// Results are cached for 1 hour.
func (a *CloudRuAdapter) ListFlavors(ctx context.Context) ([]*Flavor, error) {
	a.mu.RLock()
	if a.flavors != nil && time.Now().Before(a.flavorExp) {
		cached := make([]*Flavor, len(a.flavors))
		copy(cached, a.flavors)
		a.mu.RUnlock()
		return cached, nil
	}
	a.mu.RUnlock()

	resp, err := a.doWithRetry(ctx, http.MethodGet, "/api/v1/flavors", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var page paginatedResponse[Flavor]
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return nil, clierrors.Wrap(clierrors.CodeAPIError, "failed to decode flavors response", "API returned unexpected format", err)
	}

	flavors := make([]*Flavor, len(page.Items))
	for i := range page.Items {
		flavors[i] = &page.Items[i]
	}

	a.mu.Lock()
	a.flavors = flavors
	a.flavorExp = time.Now().Add(flavorCacheTTL)
	a.mu.Unlock()

	result := make([]*Flavor, len(flavors))
	copy(result, flavors)
	return result, nil
}

// ListAvailabilityZones lists available zones via GET /api/v1/availability-zones.
func (a *CloudRuAdapter) ListAvailabilityZones(ctx context.Context) ([]*AvailabilityZone, error) {
	resp, err := a.doWithRetry(ctx, http.MethodGet, "/api/v1/availability-zones", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, clierrors.Wrap(clierrors.CodeAPIError, "failed to read availability zones response", "API returned unexpected format", err)
	}

	// Try paginated response first, then plain array.
	var page paginatedResponse[AvailabilityZone]
	if err := json.Unmarshal(body, &page); err == nil && len(page.Items) > 0 {
		result := make([]*AvailabilityZone, len(page.Items))
		for i := range page.Items {
			result[i] = &page.Items[i]
		}
		return result, nil
	}

	var zones []AvailabilityZone
	if err := json.Unmarshal(body, &zones); err == nil {
		result := make([]*AvailabilityZone, len(zones))
		for i := range zones {
			result[i] = &zones[i]
		}
		return result, nil
	}

	return nil, clierrors.New(clierrors.CodeAPIError, "failed to decode availability zones response", "API returned unexpected format — run with --verbose to debug")
}

// ListImages lists boot images via GET /api/v1/images. A bare call returns only
// the shared catalogue, and only its first page — images built inside a project
// are invisible there. ListImagesInProject exists for those.
func (a *CloudRuAdapter) ListImages(ctx context.Context) ([]*Image, error) {
	return a.listImages(ctx, "/api/v1/images?limit=200")
}

// ListImagesInProject returns the images owned by one project — the ones
// image_create makes.
func (a *CloudRuAdapter) ListImagesInProject(ctx context.Context, projectID string) ([]*Image, error) {
	if projectID == "" {
		return nil, nil
	}
	return a.listImages(ctx, "/api/v1/images?limit=200&project_id="+projectID)
}

func (a *CloudRuAdapter) listImages(ctx context.Context, path string) ([]*Image, error) {
	resp, err := a.doWithRetry(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, clierrors.Wrap(clierrors.CodeAPIError, "failed to read images response", "API returned unexpected format", err)
	}

	// Try paginated response first, then plain array.
	var page paginatedResponse[Image]
	if err := json.Unmarshal(body, &page); err == nil && len(page.Items) > 0 {
		result := make([]*Image, len(page.Items))
		for i := range page.Items {
			result[i] = &page.Items[i]
		}
		return result, nil
	}

	var images []Image
	if err := json.Unmarshal(body, &images); err == nil {
		result := make([]*Image, len(images))
		for i := range images {
			result[i] = &images[i]
		}
		return result, nil
	}

	return nil, clierrors.New(clierrors.CodeAPIError, "failed to decode images response", "API returned unexpected format — run with --verbose to debug")
}

// GetImage retrieves image details including metadata fields via GET /api/v1/images/{id}.
// The response contains an SVG icon field that can include raw control
// characters, which are invalid in JSON strings and make the strict decoder
// fail, so the body is sanitized before decoding.
func (a *CloudRuAdapter) GetImage(ctx context.Context, id string) (*ImageDetail, error) {
	path := fmt.Sprintf("/api/v1/images/%s", id)
	resp, err := a.doWithRetry(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, clierrors.Wrap(clierrors.CodeAPIError, "failed to read image response", "API returned unexpected format", err)
	}

	var img ImageDetail
	if err := json.Unmarshal(sanitizeJSONControlChars(body), &img); err != nil {
		return nil, clierrors.Wrap(clierrors.CodeAPIError, "failed to decode image response", "API returned unexpected format", err)
	}
	return &img, nil
}

// sanitizeJSONControlChars replaces raw control characters (U+0000–U+001F,
// except the JSON-legal whitespace \t \n \r) with spaces, so a response whose
// string values contain literal control bytes (e.g. an embedded SVG icon) still
// parses. Structural JSON never contains other raw control bytes, so this is
// safe for decoding.
func sanitizeJSONControlChars(b []byte) []byte {
	out := make([]byte, len(b))
	for i, c := range b {
		if c < 0x20 && c != '\t' && c != '\n' && c != '\r' {
			out[i] = ' '
		} else {
			out[i] = c
		}
	}
	return out
}

// GetVMDetail retrieves full VM info including interfaces via GET /api/v1/vms/{id}.
func (a *CloudRuAdapter) GetVMDetail(ctx context.Context, id string) (*VMDetail, error) {
	path := fmt.Sprintf("/api/v1/vms/%s", id)
	resp, err := a.doWithRetry(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var vm VMDetail
	if err := json.NewDecoder(resp.Body).Decode(&vm); err != nil {
		return nil, clierrors.Wrap(clierrors.CodeAPIError, "failed to decode VM detail response", "API returned unexpected format", err)
	}
	return &vm, nil
}

// CreateFloatingIP creates a floating IP and attaches it to a VM interface.
func (a *CloudRuAdapter) CreateFloatingIP(ctx context.Context, req CreateFloatingIPRequest) (*FloatingIP, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, clierrors.Wrap(clierrors.CodeAPIError, "failed to marshal floating IP request", "Check request parameters", err)
	}

	resp, err := a.doWithRetry(ctx, http.MethodPost, "/api/v1/floating-ips", body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var fip FloatingIP
	if err := json.NewDecoder(resp.Body).Decode(&fip); err != nil {
		return nil, clierrors.Wrap(clierrors.CodeAPIError, "failed to decode floating IP response", "API returned unexpected format", err)
	}
	return &fip, nil
}

// GetFloatingIP retrieves a floating IP by ID via GET /api/v1/floating-ips/{id}.
func (a *CloudRuAdapter) GetFloatingIP(ctx context.Context, id string) (*FloatingIP, error) {
	path := fmt.Sprintf("/api/v1/floating-ips/%s", id)
	resp, err := a.doWithRetry(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var fip FloatingIP
	if err := json.NewDecoder(resp.Body).Decode(&fip); err != nil {
		return nil, clierrors.Wrap(clierrors.CodeAPIError, "failed to decode floating IP response", "API returned unexpected format", err)
	}
	return &fip, nil
}

// doWithRetry executes an HTTP request with exponential-backoff retry on 429
// responses, plus transient 5xx (500/502/503/504) on idempotent GET reads.
func (a *CloudRuAdapter) doWithRetry(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	backoff := initialBackoff

	for attempt := 0; attempt <= maxRetries; attempt++ {
		var bodyReader io.Reader
		if body != nil {
			bodyReader = bytes.NewReader(body)
		}

		req, err := http.NewRequestWithContext(ctx, method, a.baseURL+path, bodyReader)
		if err != nil {
			return nil, clierrors.Wrap(clierrors.CodeAPIError, "failed to create HTTP request", "Check API configuration", err)
		}

		req.Header.Set("Authorization", "Bearer "+a.getToken())
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := a.client.Do(req)
		if err != nil {
			return nil, clierrors.Wrap(clierrors.CodeAPIUnreachable, "cannot reach Cloud.ru API", "Check your network connection and try again", err)
		}

		switch {
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			return resp, nil

		case resp.StatusCode == http.StatusTooManyRequests:
			resp.Body.Close()
			if attempt == maxRetries {
				return nil, clierrors.New(clierrors.CodeAPIRateLimited, "API rate limit exceeded after retries", "Wait a moment and try again")
			}
			select {
			case <-ctx.Done():
				return nil, clierrors.Wrap(clierrors.CodeAPIError, "request cancelled during retry backoff", "Try again later", ctx.Err())
			case <-time.After(backoff):
			}
			backoff *= backoffMultiplier

		case resp.StatusCode == http.StatusNotFound:
			resp.Body.Close()
			return nil, clierrors.New(clierrors.CodeVMNotFound, fmt.Sprintf("resource not found: %s %s", method, path), "Verify the resource ID and API endpoint")

		case resp.StatusCode >= 500 && resp.StatusCode < 600 && method == http.MethodGet:
			// Transient server error (500/502/503/504) on an idempotent READ →
			// retry with backoff. GET only: retrying a POST on 5xx could
			// double-create a resource (the server may have applied it before
			// the response failed). Cloud.ru's compute API occasionally 500s
			// under bursts of concurrent reads; a couple of retries clear it.
			respBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if attempt == maxRetries {
				msg := fmt.Sprintf("API returned status %d after %d retries: %s", resp.StatusCode, maxRetries, string(respBody))
				return nil, clierrors.New(clierrors.CodeAPIError, msg, "Check Cloud.ru status page or try again later")
			}
			select {
			case <-ctx.Done():
				return nil, clierrors.Wrap(clierrors.CodeAPIError, "request cancelled during retry backoff", "Try again later", ctx.Err())
			case <-time.After(backoff):
			}
			backoff *= backoffMultiplier

		default:
			respBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			msg := fmt.Sprintf("API returned status %d: %s", resp.StatusCode, string(respBody))
			return nil, clierrors.New(clierrors.CodeAPIError, msg, "Check Cloud.ru status page or try again later")
		}
	}

	return nil, clierrors.New(clierrors.CodeAPIRateLimited, "API rate limit exceeded after retries", "Wait a moment and try again")
}
