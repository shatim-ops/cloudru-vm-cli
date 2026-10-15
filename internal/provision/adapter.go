package provision

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
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

	// Backoff is the delay before the first retry; it doubles on each next
	// attempt. Zero means the production default (1s). Tests set milliseconds.
	Backoff time.Duration
}

// NewCloudRuAdapter creates a new adapter. If client is nil, a client with a
// 60s timeout is used (http.DefaultClient has none).
// If baseURL is empty, the default Cloud.ru compute endpoint is used.
func NewCloudRuAdapter(client *http.Client, baseURL string) *CloudRuAdapter {
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
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
	path := fmt.Sprintf("/api/v1/vms/%s", seg(id))
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
	path := fmt.Sprintf("/api/v1/vms/%s", seg(id))
	resp, err := a.doWithRetry(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
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

	// Every page: the default page is 50, and a flavor named in .evo.yaml
	// past it was "not found" (api-contract §4.7).
	flavors, err := listAll[Flavor](a, ctx, "/api/v1/flavors", url.Values{}, true)
	if err != nil {
		return nil, err
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

// ListImages lists boot images via GET /api/v1/images, every page. A bare
// call returns only the shared catalogue — images built inside a project
// are invisible there. ListImagesInProject exists for those.
func (a *CloudRuAdapter) ListImages(ctx context.Context) ([]*Image, error) {
	return listAll[Image](a, ctx, "/api/v1/images", url.Values{}, true)
}

// ListImagesInProject returns the images owned by one project — the ones
// image_create makes.
func (a *CloudRuAdapter) ListImagesInProject(ctx context.Context, projectID string) ([]*Image, error) {
	if projectID == "" {
		return nil, nil
	}
	return listAll[Image](a, ctx, "/api/v1/images", url.Values{"project_id": {projectID}}, true)
}

// GetImage retrieves image details including metadata fields via GET /api/v1/images/{id}.
// The response contains an SVG icon field that can include raw control
// characters, which are invalid in JSON strings and make the strict decoder
// fail, so the body is sanitized before decoding.
func (a *CloudRuAdapter) GetImage(ctx context.Context, id string) (*ImageDetail, error) {
	path := fmt.Sprintf("/api/v1/images/%s", seg(id))
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
	path := fmt.Sprintf("/api/v1/vms/%s", seg(id))
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
	path := fmt.Sprintf("/api/v1/floating-ips/%s", seg(id))
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

// maxErrorBody bounds how much of an error response is read into a message.
const maxErrorBody = 64 << 10

// StatusError is the HTTP status of a failed API call; it is the cause
// wrapped in the CLIError the adapter returns.
type StatusError struct {
	Status int
	Body   string
}

func (e *StatusError) Error() string { return fmt.Sprintf("HTTP %d: %s", e.Status, e.Body) }

// HTTPStatus returns the HTTP status behind err, or 0.
func HTTPStatus(err error) int {
	var se *StatusError
	if errors.As(err, &se) {
		return se.Status
	}
	return 0
}

// firstBackoff returns the delay before the first retry.
func (a *CloudRuAdapter) firstBackoff() time.Duration {
	if a.Backoff > 0 {
		return a.Backoff
	}
	return initialBackoff
}

// doWithRetry executes an HTTP request with exponential-backoff retry on 429
// responses, plus transient 5xx (500/502/503/504) on idempotent GET reads.
func (a *CloudRuAdapter) doWithRetry(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	backoff := a.firstBackoff()

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
			if ctx.Err() != nil {
				return nil, clierrors.Wrap(clierrors.CodeAPIUnreachable, "request cancelled", "The operation was cancelled.", err)
			}
			// A dropped connection is common (about 1 call in 3 from some
			// networks). GET and DELETE are safe to repeat — a repeated
			// DELETE answers 404, which callers read as "already gone";
			// POST/PUT are not: the server may have applied them.
			idempotent := method == http.MethodGet || method == http.MethodDelete
			if idempotent && attempt < maxRetries {
				select {
				case <-ctx.Done():
					return nil, clierrors.Wrap(clierrors.CodeAPIUnreachable, "request cancelled during retry backoff", "The operation was cancelled.", ctx.Err())
				case <-time.After(backoff):
				}
				backoff *= backoffMultiplier
				continue
			}
			hint := "Check your network connection and try again"
			if !idempotent {
				hint = "The request may have reached Cloud.ru before the connection broke: check the Cloud.ru console for a resource it created before retrying."
			}
			return nil, clierrors.Wrap(clierrors.CodeAPIUnreachable, "cannot reach Cloud.ru API", hint, err)
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

		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
			resp.Body.Close()
			cause := &StatusError{Status: resp.StatusCode, Body: string(respBody)}
			if resp.StatusCode == http.StatusUnauthorized {
				return nil, clierrors.Wrap(clierrors.CodeAuthExpired, "Cloud.ru API rejected the token (401)",
					"Check the access key with 'evo doctor', or run 'evo configure' again.", cause)
			}
			return nil, clierrors.Wrap(clierrors.CodeAuthFailed, fmt.Sprintf("access denied (403) for %s %s: %s", method, path, respBody),
				"The service account lacks a role for this operation in the project.", cause)

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
			respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
			resp.Body.Close()
			msg := fmt.Sprintf("API returned status %d: %s", resp.StatusCode, string(respBody))
			hint := "Check Cloud.ru status page or try again later"
			if resp.StatusCode < 500 {
				// 400/409/422: this request is refused; waiting will not help.
				hint = "Cloud.ru rejected the request; the message above says why."
			}
			return nil, clierrors.Wrap(clierrors.CodeAPIError, msg, hint, &StatusError{Status: resp.StatusCode, Body: string(respBody)})
		}
	}

	return nil, clierrors.New(clierrors.CodeAPIRateLimited, "API rate limit exceeded after retries", "Wait a moment and try again")
}
