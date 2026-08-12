package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
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

// EnvVarProvider obtains tokens using the CLOUDRU_API_KEY environment variable.
type EnvVarProvider struct {
	client *http.Client
	mu     sync.Mutex
	cached *Token
}

// NewEnvVarProvider creates an EnvVarProvider with the given HTTP client.
func NewEnvVarProvider(client *http.Client) *EnvVarProvider {
	if client == nil {
		client = http.DefaultClient
	}
	return &EnvVarProvider{
		client: client,
	}
}

// Token returns a valid IAM token. Refreshes if cached token has < 5min TTL.
// Pre-flight check: rejects if resulting token TTL < 10min (QCSD #1).
func (p *EnvVarProvider) Token(ctx context.Context) (Token, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Return cached token if still valid.
	if p.cached != nil && p.cached.IsValid(refreshTTL) {
		return *p.cached, nil
	}

	keyID := os.Getenv("CLOUDRU_KEY_ID")
	secret := os.Getenv("CLOUDRU_SECRET")

	// Fallback: accept CLOUDRU_API_KEY as "keyId:secret" for backward compat.
	if keyID == "" && secret == "" {
		apiKey := os.Getenv("CLOUDRU_API_KEY")
		if apiKey != "" {
			if parts := strings.SplitN(apiKey, ":", 2); len(parts) == 2 {
				keyID = parts[0]
				secret = parts[1]
			} else {
				return Token{}, clerrors.New(
					clerrors.CodeAuthRequired,
					"CLOUDRU_API_KEY must be in format 'keyId:secret'",
					"Set CLOUDRU_KEY_ID and CLOUDRU_SECRET separately, or use CLOUDRU_API_KEY=keyId:secret",
				)
			}
		}
	}

	if keyID == "" || secret == "" {
		return Token{}, clerrors.New(
			clerrors.CodeAuthRequired,
			"Cloud.ru credentials not set",
			"Set CLOUDRU_KEY_ID and CLOUDRU_SECRET environment variables (get them at https://console.cloud.ru)",
		)
	}

	tok, err := p.exchange(ctx, keyID, secret)
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
	return tok, nil
}

// exchange performs the HTTP POST to the IAM endpoint.
func (p *EnvVarProvider) exchange(ctx context.Context, keyID, secret string) (Token, error) {
	body := fmt.Sprintf(`{"keyId":%q,"secret":%q}`, keyID, secret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, IAMEndpoint, strings.NewReader(body))
	if err != nil {
		return Token{}, clerrors.Wrap(
			clerrors.CodeAPIUnreachable,
			"failed to create IAM token request",
			"Check your network connection",
			err,
		)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return Token{}, clerrors.Wrap(
			clerrors.CodeAPIUnreachable,
			"failed to reach IAM endpoint",
			"Check your network connection and try again",
			err,
		)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Token{}, clerrors.New(
			clerrors.CodeAPIError,
			fmt.Sprintf("IAM token exchange failed with status %d", resp.StatusCode),
			"Verify your CLOUDRU_KEY_ID and CLOUDRU_SECRET are correct",
		)
	}

	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"` // seconds
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return Token{}, clerrors.Wrap(
			clerrors.CodeAPIError,
			"failed to parse IAM token response",
			"Unexpected response from IAM API",
			err,
		)
	}

	if result.AccessToken == "" {
		return Token{}, clerrors.New(
			clerrors.CodeAPIError,
			"IAM returned empty access_token",
			"Verify your credentials are correct",
		)
	}

	expiresAt := time.Now().Add(time.Duration(result.ExpiresIn) * time.Second)

	return Token{
		Value:     result.AccessToken,
		ExpiresAt: expiresAt,
	}, nil
}
