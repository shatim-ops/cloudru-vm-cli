package mk8s

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// kubeconfig приходит из ответа API Managed Kubernetes — недоверенный ввод.
// По --iam-url из него уходят client_id и client_secret ключа доступа
// сервисного аккаунта: адрес вне Cloud.ru или без TLS отдал бы секрет
// постороннему.
func kcWithIAMURL(u string) string {
	return strings.Replace(sampleKC, "https://id.cloud.ru/auth/system/openid/token", u, 1)
}

func TestOIDCURLFromKubeconfig_OnlyCloudRuHTTPS(t *testing.T) {
	for _, tc := range []struct {
		url  string
		want string
	}{
		{"https://id.cloud.ru/auth/system/openid/token", "https://id.cloud.ru/auth/system/openid/token"},
		{"https://auth.iam.cloud.ru/token", "https://auth.iam.cloud.ru/token"},
		{"https://evil.example/token", ""},
		{"http://id.cloud.ru/auth/system/openid/token", ""},
		{"https://id.cloud.ru.evil.example/token", ""},
		{"https://evilcloud.ru/token", ""},
		{"https://user@id.cloud.ru@evil.example/token", ""},
		{"https://id.cloud.ru:8443/token", ""},
		{"//id.cloud.ru/token", ""},
		{"file:///etc/passwd", ""},
	} {
		if got := OIDCURLFromKubeconfig(kcWithIAMURL(tc.url)); got != tc.want {
			t.Errorf("--iam-url=%s: got %q, want %q", tc.url, got, tc.want)
		}
	}
}

func TestFetchOIDCToken_RedirectDoesNotResendSecret(t *testing.T) {
	var leaked string
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		leaked = string(b)
		w.Write([]byte(`{"access_token":"x"}`))
	}))
	defer sink.Close()
	// 307 повторяет POST с тем же телом — с client_secret — по новому адресу.
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, sink.URL+"/token", http.StatusTemporaryRedirect)
	}))
	defer front.Close()

	_, err := FetchOIDCToken(context.Background(), front.URL+"/token", "key-id", "top-secret-value")
	if strings.Contains(leaked, "top-secret-value") {
		t.Fatalf("секрет ушёл по редиректу: %q", leaked)
	}
	if err == nil {
		t.Fatal("редирект принят как успешный ответ")
	}
}

// Что бы ни пришло в kubeconfig, наружу выходит либо "", либо https-адрес
// на cloud.ru: секрет уходит только туда.
func FuzzOIDCURLFromKubeconfig(f *testing.F) {
	for _, u := range []string{
		"https://id.cloud.ru/auth/system/openid/token",
		"https://evil.example/token",
		"http://id.cloud.ru/x",
		"https://id.cloud.ru.evil.example/",
		"https://a@id.cloud.ru/",
		"https://ID.CLOUD.RU./x",
		"https://[::1]/x",
	} {
		f.Add(kcWithIAMURL(u))
	}
	f.Add(`users: [{user: {exec: {args: ["--iam-url=https://x.cloud.ru"]}}}]`)
	f.Fuzz(func(t *testing.T, kc string) {
		got := OIDCURLFromKubeconfig(kc)
		if got == "" {
			return
		}
		p, err := url.Parse(got)
		if err != nil {
			t.Fatalf("%q: %v", got, err)
		}
		h := strings.ToLower(p.Hostname())
		if p.Scheme != "https" || p.User != nil || p.Port() != "" ||
			(h != "cloud.ru" && !strings.HasSuffix(h, ".cloud.ru")) {
			t.Fatalf("недоверенный адрес пропущен: %q", got)
		}
	})
}
