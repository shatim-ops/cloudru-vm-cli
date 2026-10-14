package mk8s

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type recordTransport struct{ hosts []string }

func (r *recordTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.hosts = append(r.hosts, req.URL.Scheme+"://"+req.URL.Host)
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(`{"id_token":"t"}`)),
		Header:     http.Header{},
		Request:    req,
	}, nil
}

// The token URL comes from the kubeconfig the API returns, and the request
// carries the access key secret: it must go to Cloud.ru IAM over https only.
func TestFetchOIDCToken_OnlyTrustedIAMHosts(t *testing.T) {
	rt := &recordTransport{}
	orig := oidcClient
	oidcClient = &http.Client{Transport: rt}
	t.Cleanup(func() { oidcClient = orig })

	for _, u := range []string{
		"",
		"https://id.cloud.ru/auth/system/openid/token",
		"https://auth.iam.cloud.ru/auth/system/openid/token",
		"https://attacker.example/token",
		"http://id.cloud.ru/auth/system/openid/token",
		"https://id.cloud.ru.attacker.example/token",
		"https://id.cloud.ru:8443/token",
		"https://user@id.cloud.ru/token",
		"http://169.254.169.254/latest",
	} {
		rt.hosts = nil
		if _, err := FetchOIDCToken(context.Background(), u, "k", "s"); err != nil {
			t.Fatalf("%q: %v", u, err)
		}
		got := rt.hosts[0]
		if got != "https://id.cloud.ru" && got != "https://auth.iam.cloud.ru" {
			t.Errorf("iam-url %q: the access key went to %s", u, got)
		}
	}
}
