package auth

import (
	"encoding/json"
	"os"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
)

// TokenCache persists IAM tokens between runs. Entries are bound to the key
// ID they were issued for: a token of another key is never returned.
type TokenCache interface {
	Load(keyID string) (Token, bool)
	Store(keyID string, tok Token) error
}

// FileCache stores one token in a JSON file with mode 0600.
type FileCache struct {
	Path string
}

type cachedToken struct {
	KeyID       string    `json:"key_id"`
	AccessToken string    `json:"access_token"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// Load returns the cached token for keyID. A missing, corrupt or foreign
// file is a cache miss, not an error.
func (c FileCache) Load(keyID string) (Token, bool) {
	data, err := os.ReadFile(c.Path)
	if err != nil {
		return Token{}, false
	}
	var ct cachedToken
	if json.Unmarshal(data, &ct) != nil || ct.KeyID != keyID || ct.AccessToken == "" {
		return Token{}, false
	}
	return Token{Value: ct.AccessToken, ExpiresAt: ct.ExpiresAt}, true
}

// Store writes the token, replacing the previous one.
func (c FileCache) Store(keyID string, tok Token) error {
	data, err := json.Marshal(cachedToken{KeyID: keyID, AccessToken: tok.Value, ExpiresAt: tok.ExpiresAt})
	if err != nil {
		return err
	}
	return config.WriteFilePrivate(c.Path, data)
}
