package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	clerrors "github.com/shatim-ops/cloudru-vm-cli/internal/errors"
)

// IAMEndpoint is the Cloud.ru IAM token exchange URL.
var IAMEndpoint = "https://iam.api.cloud.ru/api/v1/auth/token"

// refreshTTL is the minimum remaining TTL before a cached token is refreshed.
const refreshTTL = 5 * time.Minute

// minAcceptableTTL is the pre-flight check threshold (QCSD #1).
const minAcceptableTTL = 10 * time.Minute

// Credentials is a service account access key: key ID and secret.
type Credentials struct {
	KeyID  string
	Secret string
}

// IAMProvider exchanges an access key for IAM tokens. The credentials come
// from the resolved configuration (profile, env or flags), not from the
// environment directly. Tokens are cached in memory and, if Cache is set,
// on disk, so separate runs of the CLI do not each go to IAM.
type IAMProvider struct {
	creds  Credentials
	client *http.Client
	cache  TokenCache

	// sleep waits between retries; tests replace it to avoid real pauses.
	sleep func(ctx context.Context, d time.Duration) error

	mu        sync.Mutex
	cached    *Token
	exchanges int
}

// Retries of the token exchange. Corporate proxies and flaky links reset
// connections to IAM now and then; one reset must not fail the command.
const (
	exchangeAttempts  = 3
	exchangeBaseDelay = 500 * time.Millisecond // 0.5 s, then 1 s
)

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// NewIAMProvider creates a provider for creds. If client is nil, a client
// with a 60s timeout is used: http.DefaultClient has no timeout and would
// hang forever on a stalled IAM connection. cache may be nil.
func NewIAMProvider(creds Credentials, client *http.Client, cache TokenCache) *IAMProvider {
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	return &IAMProvider{creds: creds, client: client, cache: cache, sleep: sleepCtx}
}

// KeyID returns the key ID the provider authenticates with.
func (p *IAMProvider) KeyID() string { return p.creds.KeyID }

// Exchanges returns how many times the provider went to IAM.
func (p *IAMProvider) Exchanges() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exchanges
}

// Token returns a valid IAM token. Refreshes if cached token has < 5min TTL.
// Pre-flight check: rejects if resulting token TTL < 10min (QCSD #1).
func (p *IAMProvider) Token(ctx context.Context) (Token, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.creds.KeyID == "" || p.creds.Secret == "" {
		return Token{}, clerrors.New(
			clerrors.CodeAuthRequired,
			"Cloud.ru credentials not set",
			"Run 'evo configure', or set CLOUDRU_KEY_ID and CLOUDRU_SECRET (create an access key for a service account at https://console.cloud.ru)",
		)
	}

	if p.cached != nil && p.cached.IsValid(refreshTTL) {
		return *p.cached, nil
	}
	if p.cache != nil {
		if tok, ok := p.cache.Load(p.creds.KeyID); ok && tok.IsValid(refreshTTL) {
			p.cached = &tok
			return tok, nil
		}
	}

	tok, err := p.exchange(ctx)
	if err != nil {
		return Token{}, err
	}

	// Pre-flight: reject tokens with very short TTL.
	if tok.TTL() < minAcceptableTTL {
		return Token{}, clerrors.New(
			clerrors.CodeAuthExpired,
			fmt.Sprintf("token TTL too short: %s remaining", tok.TTL().Truncate(time.Second)),
			"Your credentials may be expiring soon. Rotate them at https://console.cloud.ru",
		)
	}

	p.cached = &tok
	if p.cache != nil {
		// The cache only saves IAM round trips; failing to write it must
		// not fail the command.
		_ = p.cache.Store(p.creds.KeyID, tok)
	}
	return tok, nil
}

// exchange obtains a token, retrying transport errors and 5xx responses
// with exponential backoff. 4xx responses are final.
func (p *IAMProvider) exchange(ctx context.Context) (Token, error) {
	delay := exchangeBaseDelay
	for attempt := 1; ; attempt++ {
		tok, retryable, err := p.exchangeOnce(ctx)
		if err == nil || !retryable || attempt == exchangeAttempts || ctx.Err() != nil {
			return tok, err
		}
		if serr := p.sleep(ctx, delay); serr != nil {
			return Token{}, err
		}
		delay *= 2
	}
}

// exchangeOnce performs one HTTP POST to the IAM endpoint and reports
// whether a failure is worth retrying.
func (p *IAMProvider) exchangeOnce(ctx context.Context) (tok Token, retryable bool, err error) {
	p.exchanges++
	payload, err := json.Marshal(map[string]string{"keyId": p.creds.KeyID, "secret": p.creds.Secret})
	if err != nil {
		return Token{}, false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, IAMEndpoint, bytes.NewReader(payload))
	if err != nil {
		return Token{}, false, clerrors.Wrap(
			clerrors.CodeAPIUnreachable,
			"failed to create IAM token request",
			"Check your network connection",
			err,
		)
	}
	req.Header.Set("Content-Type", "application/json")

	// No redirects: on 307/308 the client would POST the same body, with
	// the key secret, to wherever Location points. IAM answers directly;
	// a redirect is treated as an unexpected status below.
	client := *p.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return Token{}, true, clerrors.Wrap(
			clerrors.CodeAPIUnreachable,
			"failed to reach IAM endpoint",
			"Check your network connection and try again",
			err,
		)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return Token{}, false, clerrors.New(
			clerrors.CodeAuthFailed,
			fmt.Sprintf("IAM rejected the credentials (status %d)", resp.StatusCode),
			"Verify the key ID and secret: 'evo configure', or CLOUDRU_KEY_ID / CLOUDRU_SECRET",
		)
	}
	if resp.StatusCode != http.StatusOK {
		return Token{}, resp.StatusCode >= 500, clerrors.New(
			clerrors.CodeAPIError,
			fmt.Sprintf("IAM token exchange failed with status %d", resp.StatusCode),
			"Retry later; if it persists, verify the key ID and secret",
		)
	}

	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"` // seconds
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		// Usually a connection reset mid-body: worth another attempt.
		return Token{}, true, clerrors.Wrap(
			clerrors.CodeAPIError,
			"failed to parse IAM token response",
			"Unexpected response from IAM API",
			err,
		)
	}
	if result.AccessToken == "" {
		return Token{}, false, clerrors.New(
			clerrors.CodeAPIError,
			"IAM returned empty access_token",
			"Verify your credentials are correct",
		)
	}

	return Token{
		Value:     result.AccessToken,
		ExpiresAt: time.Now().Add(time.Duration(result.ExpiresIn) * time.Second),
	}, false, nil
}
