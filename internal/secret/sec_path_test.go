package secret

import "testing"

// A secret path keeps its folder separators, but "." and ".." segments must
// not survive: "/v2/version/app/../other" would read another secret than the
// one the caller (and a confirmation preview) named.
func TestEscapePathDotSegments(t *testing.T) {
	for in, want := range map[string]string{
		"app/db":       "app/db",
		"app/../other": "app/%2E%2E/other",
		"./x":          "%2E/x",
		"a b/c?d":      "a%20b/c%3Fd",
		"..":           "%2E%2E",
	} {
		if got := escapePath(in); got != want {
			t.Errorf("escapePath(%q) = %q, want %q", in, got, want)
		}
	}
}
