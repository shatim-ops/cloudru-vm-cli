package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	clerrors "github.com/dzhechko/cloudru-vm-cli/internal/errors"
)

// mockKeyringStore is an in-memory KeyringStore for testing.
type mockKeyringStore struct {
	mu   sync.Mutex
	data map[string]string
}

func newMockKeyringStore() *mockKeyringStore {
	return &mockKeyringStore{data: make(map[string]string)}
}

func (m *mockKeyringStore) key(service, k string) string {
	return service + "/" + k
}

func (m *mockKeyringStore) Get(service, k string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.data[m.key(service, k)]
	if !ok {
		return "", errors.New("not found")
	}
	return v, nil
}

func (m *mockKeyringStore) Set(service, k, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[m.key(service, k)] = value
	return nil
}

func (m *mockKeyringStore) Delete(service, k string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, m.key(service, k))
	return nil
}

// newMockIAMServer creates a test server that mimics the Cloud.ru IAM token exchange endpoint.
func newMockIAMServer(t *testing.T, tokenTTL time.Duration) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var body struct {
			KeyID  string `json:"keyId"`
			Secret string `json:"secret"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if body.KeyID == "" || body.Secret == "" {
			http.Error(w, "missing keyId or secret", http.StatusUnauthorized)
			return
		}

		resp := map[string]any{
			"access_token": "test-iam-token-" + body.KeyID,
			"expires_in":   int(tokenTTL.Seconds()),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}

func TestEnvVarProvider_Token(t *testing.T) {
	srv := newMockIAMServer(t, 1*time.Hour)
	defer srv.Close()

	oldEndpoint := IAMEndpoint
	IAMEndpoint = srv.URL
	defer func() { IAMEndpoint = oldEndpoint }()

	os.Setenv("CLOUDRU_KEY_ID", "test-key-123")
	os.Setenv("CLOUDRU_SECRET", "test-secret")
	defer os.Unsetenv("CLOUDRU_KEY_ID")
	defer os.Unsetenv("CLOUDRU_SECRET")

	p := NewEnvVarProvider(srv.Client())
	tok, err := p.Token(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok.Value != "test-iam-token-test-key-123" {
		t.Errorf("unexpected token value: %s", tok.Value)
	}
	if tok.TTL() < 50*time.Minute {
		t.Errorf("expected TTL > 50min, got %s", tok.TTL())
	}
}

func TestEnvVarProvider_CachesToken(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		resp := map[string]any{
			"access_token": "cached-token",
			"expires_in":   3600,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	oldEndpoint := IAMEndpoint
	IAMEndpoint = srv.URL
	defer func() { IAMEndpoint = oldEndpoint }()

	os.Setenv("CLOUDRU_KEY_ID", "test-key")
	os.Setenv("CLOUDRU_SECRET", "test-secret")
	defer os.Unsetenv("CLOUDRU_KEY_ID")
	defer os.Unsetenv("CLOUDRU_SECRET")

	p := NewEnvVarProvider(srv.Client())

	// First call hits the server.
	_, err := p.Token(context.Background())
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	// Second call should return cached.
	_, err = p.Token(context.Background())
	if err != nil {
		t.Fatalf("second call: %v", err)
	}

	if callCount != 1 {
		t.Errorf("expected 1 server call, got %d", callCount)
	}
}

func TestEnvVarProvider_RefreshesExpired(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		resp := map[string]any{
			"access_token": "refreshed-token",
			"expires_in":   3600,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	oldEndpoint := IAMEndpoint
	IAMEndpoint = srv.URL
	defer func() { IAMEndpoint = oldEndpoint }()

	os.Setenv("CLOUDRU_KEY_ID", "test-key")
	os.Setenv("CLOUDRU_SECRET", "test-secret")
	defer os.Unsetenv("CLOUDRU_KEY_ID")
	defer os.Unsetenv("CLOUDRU_SECRET")

	p := NewEnvVarProvider(srv.Client())

	// Seed with an expired cached token.
	p.cached = &Token{
		Value:     "old-token",
		ExpiresAt: time.Now().Add(1 * time.Minute), // < 5min threshold
	}

	tok, err := p.Token(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok.Value != "refreshed-token" {
		t.Errorf("expected refreshed token, got %s", tok.Value)
	}
	if callCount != 1 {
		t.Errorf("expected 1 server call for refresh, got %d", callCount)
	}
}

func TestEnvVarProvider_NoAPIKey(t *testing.T) {
	os.Unsetenv("CLOUDRU_API_KEY")
	os.Unsetenv("CLOUDRU_KEY_ID")
	os.Unsetenv("CLOUDRU_SECRET")

	p := NewEnvVarProvider(http.DefaultClient)
	_, err := p.Token(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var cliErr *clerrors.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected CLIError, got %T: %v", err, err)
	}
	if cliErr.Code != clerrors.CodeAuthRequired {
		t.Errorf("expected code %s, got %s", clerrors.CodeAuthRequired, cliErr.Code)
	}
}

func TestEnvVarProvider_PreFlightTTL(t *testing.T) {
	// Server returns a token that expires in 5 minutes (< 10min threshold).
	srv := newMockIAMServer(t, 5*time.Minute)
	defer srv.Close()

	oldEndpoint := IAMEndpoint
	IAMEndpoint = srv.URL
	defer func() { IAMEndpoint = oldEndpoint }()

	os.Setenv("CLOUDRU_KEY_ID", "short-lived-key")
	os.Setenv("CLOUDRU_SECRET", "short-lived-secret")
	defer os.Unsetenv("CLOUDRU_KEY_ID")
	defer os.Unsetenv("CLOUDRU_SECRET")

	p := NewEnvVarProvider(srv.Client())
	_, err := p.Token(context.Background())
	if err == nil {
		t.Fatal("expected pre-flight TTL error, got nil")
	}

	var cliErr *clerrors.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected CLIError, got %T: %v", err, err)
	}
	if cliErr.Code != clerrors.CodeAuthExpired {
		t.Errorf("expected code %s, got %s", clerrors.CodeAuthExpired, cliErr.Code)
	}
}

func TestKeyringProvider_Login(t *testing.T) {
	srv := newMockIAMServer(t, 1*time.Hour)
	defer srv.Close()

	oldEndpoint := IAMEndpoint
	IAMEndpoint = srv.URL
	defer func() { IAMEndpoint = oldEndpoint }()

	store := newMockKeyringStore()
	p := NewKeyringProvider(store, srv.Client())

	err := p.Login(context.Background(), "my-key-id:my-secret")
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}

	// Verify key was stored.
	stored, err := store.Get("cloudru-vm", "api-key")
	if err != nil {
		t.Fatalf("failed to read from keyring: %v", err)
	}
	if stored != "my-key-id:my-secret" {
		t.Errorf("expected stored key 'my-key-id:my-secret', got '%s'", stored)
	}
}

func TestKeyringProvider_Token(t *testing.T) {
	srv := newMockIAMServer(t, 1*time.Hour)
	defer srv.Close()

	oldEndpoint := IAMEndpoint
	IAMEndpoint = srv.URL
	defer func() { IAMEndpoint = oldEndpoint }()

	// Pre-populate keyring with credentials in keyId:secret format.
	store := newMockKeyringStore()
	store.Set("cloudru-vm", "api-key", "stored-key:stored-secret")

	// Clear env to ensure keyring is used.
	oldKeyID := os.Getenv("CLOUDRU_KEY_ID")
	oldSecret := os.Getenv("CLOUDRU_SECRET")
	oldAPIKey := os.Getenv("CLOUDRU_API_KEY")
	os.Unsetenv("CLOUDRU_KEY_ID")
	os.Unsetenv("CLOUDRU_SECRET")
	os.Unsetenv("CLOUDRU_API_KEY")
	defer func() {
		if oldKeyID != "" {
			os.Setenv("CLOUDRU_KEY_ID", oldKeyID)
		}
		if oldSecret != "" {
			os.Setenv("CLOUDRU_SECRET", oldSecret)
		}
		if oldAPIKey != "" {
			os.Setenv("CLOUDRU_API_KEY", oldAPIKey)
		}
	}()

	p := NewKeyringProvider(store, srv.Client())
	tok, err := p.Token(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok.Value != "test-iam-token-stored-key" {
		t.Errorf("expected token from keyring key, got %s", tok.Value)
	}
}

func TestToken_IsValid(t *testing.T) {
	tests := []struct {
		name   string
		ttl    time.Duration
		minTTL time.Duration
		want   bool
	}{
		{"valid with plenty of time", 1 * time.Hour, 5 * time.Minute, true},
		{"valid at boundary", 6 * time.Minute, 5 * time.Minute, true},
		{"invalid below threshold", 4 * time.Minute, 5 * time.Minute, false},
		{"invalid expired", -1 * time.Minute, 5 * time.Minute, false},
		{"zero TTL threshold", 1 * time.Second, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tok := Token{
				Value:     "test",
				ExpiresAt: time.Now().Add(tt.ttl),
			}
			if got := tok.IsValid(tt.minTTL); got != tt.want {
				t.Errorf("IsValid(%v) = %v, want %v", tt.minTTL, got, tt.want)
			}
		})
	}
}
