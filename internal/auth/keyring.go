package auth

import (
	"context"
	"net/http"
	"os"
)

const (
	defaultService = "cloudru-vm"
	keyAPIKey      = "api-key"
)

// KeyringStore abstracts OS keyring operations (for testing).
type KeyringStore interface {
	Get(service, key string) (string, error)
	Set(service, key, value string) error
	Delete(service, key string) error
}

// KeyringProvider uses the OS keyring for credential storage.
type KeyringProvider struct {
	store   KeyringStore
	env     *EnvVarProvider // delegates token exchange to EnvVarProvider
	service string          // keyring service name, e.g. "cloudru-vm"
}

// NewKeyringProvider creates a KeyringProvider backed by the given keyring store.
func NewKeyringProvider(store KeyringStore, client *http.Client) *KeyringProvider {
	return &KeyringProvider{
		store:   store,
		env:     NewEnvVarProvider(client),
		service: defaultService,
	}
}

// Login stores the API key in the keyring after validating it via a test token exchange.
func (p *KeyringProvider) Login(ctx context.Context, apiKey string) error {
	// Validate the key by performing a token exchange.
	os.Setenv("CLOUDRU_API_KEY", apiKey)
	defer os.Unsetenv("CLOUDRU_API_KEY")

	// Clear any cached token so exchange is forced.
	p.env.mu.Lock()
	p.env.cached = nil
	p.env.mu.Unlock()

	if _, err := p.env.Token(ctx); err != nil {
		return err
	}

	// Key is valid; store it in the keyring.
	if err := p.store.Set(p.service, keyAPIKey, apiKey); err != nil {
		return err
	}

	return nil
}

// Token reads the API key from the keyring and exchanges it for an IAM token.
func (p *KeyringProvider) Token(ctx context.Context) (Token, error) {
	apiKey, err := p.store.Get(p.service, keyAPIKey)
	if err != nil || apiKey == "" {
		return Token{}, &tokenError{
			code:    "AUTH_REQUIRED",
			message: "no API key found in keyring",
			hint:    "Run 'cloudru-vm login' to store your credentials",
		}
	}

	// Set env var temporarily so EnvVarProvider can use it.
	old := os.Getenv("CLOUDRU_API_KEY")
	os.Setenv("CLOUDRU_API_KEY", apiKey)
	defer func() {
		if old == "" {
			os.Unsetenv("CLOUDRU_API_KEY")
		} else {
			os.Setenv("CLOUDRU_API_KEY", old)
		}
	}()

	return p.env.Token(ctx)
}

// tokenError is a simple error for keyring-specific failures.
type tokenError struct {
	code    string
	message string
	hint    string
}

func (e *tokenError) Error() string {
	return e.code + ": " + e.message
}
