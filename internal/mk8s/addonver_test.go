package mk8s

import "testing"

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v1.14.0", "1.13.9", 1},
		{"1.2.10", "1.2.9", 1},
		{"1.2", "1.2.0", 0},
		{"1.0.0-rc.1", "1.0.0", -1},
		{"1.0.0-rc.2", "1.0.0-rc.10", -1},
		{"1.0.0-alpha", "1.0.0-1", 1},
		{"1.0.0+build.5", "1.0.0", 0},
		{"latest", "0.0.1", -1},
		{"b", "a", 1},
	}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// Порядок релизов в ответе не задан: новые — первыми по appVersion, затем
// по addonVersion; «последняя» — новейшая ACTIVE.
func TestSortAndLatestRelease(t *testing.T) {
	rs := []AddonRelease{
		{AppVersion: "v1.9.0", AddonVersion: "1", Mode: "SUPPORT_MODE_ACTIVE"},
		{AppVersion: "v1.10.0", AddonVersion: "1", Mode: "SUPPORT_MODE_EXPERIMENTAL"},
		{AppVersion: "v1.9.0", AddonVersion: "2", Mode: "ACTIVE"},
		{AppVersion: "v1.8.0", AddonVersion: "1", Mode: "SUPPORT_MODE_END_OF_LIFE"},
	}
	sorted := append([]AddonRelease(nil), rs...)
	SortReleases(sorted)
	order := ""
	for _, r := range sorted {
		order += r.AppVersion + "/" + r.AddonVersion + " "
	}
	if order != "v1.10.0/1 v1.9.0/2 v1.9.0/1 v1.8.0/1 " {
		t.Errorf("order: %s", order)
	}
	if l, ok := LatestRelease(rs); !ok || l.AppVersion != "v1.9.0" || l.AddonVersion != "2" {
		t.Errorf("latest: %+v %v", l, ok)
	}
	if l, ok := LatestRelease(rs[1:2]); !ok || l.AppVersion != "v1.10.0" {
		t.Errorf("latest without ACTIVE: %+v %v", l, ok)
	}
	if _, ok := LatestRelease(rs[3:]); ok {
		t.Error("an END_OF_LIFE release picked as latest")
	}
}

func TestNormalizeMode(t *testing.T) {
	for in, want := range map[string]string{"SUPPORT_MODE_ACTIVE": "ACTIVE", "active": "ACTIVE", " SUPPORT_MODE_END_OF_LIFE ": "END_OF_LIFE", "": ""} {
		if got := NormalizeMode(in); got != want {
			t.Errorf("NormalizeMode(%q) = %q, want %q", in, got, want)
		}
	}
}
