// Package audit is a client for the Cloud.ru Audit Logging API
// (https://audit.api.cloud.ru). IAM Bearer auth. Project is in the PATH
// (/events/list/{projectId}). Contract confirmed live — the API validated our
// request and returned "timestamps must end with 'Z' or a valid timezone
// offset" for start/end, i.e. RFC3339 (protobuf Timestamp), NOT Unix ms.
//
//	GET /events/list/{projectId}?start=&end=&pageSize=&nextPageToken=
//
// Recommended window: ≤30min for chatty projects, 1-3h for quiet ones.
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultBaseURL = "https://audit.api.cloud.ru"

// Client talks to the Audit Logging API.
type Client struct {
	http      *http.Client
	base      string
	token     string
	projectID string
}

// New builds a client. baseURL empty → defaultBaseURL.
func New(token, projectID, baseURL string) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Client{http: &http.Client{Timeout: 60 * time.Second}, base: strings.TrimRight(baseURL, "/"), token: token, projectID: projectID}
}

// ProjectID exposes the configured project.
func (c *Client) ProjectID() string { return c.projectID }

func (c *Client) do(ctx context.Context, path string, q url.Values) ([]byte, int, error) {
	u := c.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("audit request failed: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return data, resp.StatusCode, nil
}

func apiError(status int, body []byte) error {
	var e struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	_ = json.Unmarshal(body, &e)
	msg := e.Message
	if msg == "" {
		msg = e.Error
	}
	if msg == "" {
		msg = strings.TrimSpace(string(body))
	}
	if msg == "" {
		msg = http.StatusText(status)
	}
	return fmt.Errorf("audit API %d: %s", status, msg)
}

// EventPage is one page of audit events plus the continuation token.
type EventPage struct {
	Events        []map[string]any `json:"events"`
	NextPageToken string           `json:"nextPageToken"`
}

// ListOptions filter an audit-events query. Start/End are RFC3339 (required by
// the API); if zero, the caller-provided defaults apply.
type ListOptions struct {
	Start    time.Time
	End      time.Time
	PageSize int
	// PageToken continues a previous page (from NextPageToken).
	PageToken string
}

// List returns one page of audit events for the project. The API paginates via
// nextPageToken; callers loop until it is empty. Response field names vary
// slightly by gateway version, so events are returned as raw maps.
func (c *Client) List(ctx context.Context, opts ListOptions) (*EventPage, error) {
	q := url.Values{}
	if !opts.Start.IsZero() {
		q.Set("start", opts.Start.UTC().Format(time.RFC3339))
	}
	if !opts.End.IsZero() {
		q.Set("end", opts.End.UTC().Format(time.RFC3339))
	}
	if opts.PageSize > 0 {
		q.Set("pageSize", fmt.Sprintf("%d", opts.PageSize))
	}
	if opts.PageToken != "" {
		q.Set("nextPageToken", opts.PageToken)
	}
	data, status, err := c.do(ctx, "/events/list/"+url.PathEscape(c.projectID), q)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, apiError(status, data)
	}
	page := &EventPage{}
	// Tolerate {events:[…],nextPageToken} or a bare array.
	trimmed := strings.TrimSpace(string(data))
	if strings.HasPrefix(trimmed, "[") {
		_ = json.Unmarshal(data, &page.Events)
		return page, nil
	}
	if err := json.Unmarshal(data, page); err != nil {
		return nil, fmt.Errorf("parse audit events: %w", err)
	}
	return page, nil
}
