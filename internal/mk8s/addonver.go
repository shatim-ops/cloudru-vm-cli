package mk8s

import (
	"sort"
	"strconv"
	"strings"
)

// Versions of addon releases.
// The API does not order releases (api-probe #5: traefik ascending, the
// others descending), so "latest" is picked here by semver.

// Support modes of a release, without the SUPPORT_MODE_ prefix the live API
// adds (the spec lists them bare).
const (
	ModeActive       = "ACTIVE"
	ModeExperimental = "EXPERIMENTAL"
	ModeEndOfSupport = "END_OF_SUPPORT"
	ModeEndOfLife    = "END_OF_LIFE"
	ModeInactive     = "INACTIVE"
)

// NormalizeMode is a release mode without its prefix, upper case:
// "SUPPORT_MODE_ACTIVE" and "active" are both "ACTIVE".
func NormalizeMode(m string) string {
	m = strings.ToUpper(strings.TrimSpace(m))
	return strings.TrimPrefix(m, "SUPPORT_MODE_")
}

// semver is a version split for comparison; ok is false for a string that
// is not one (it then sorts after every real version, by text).
type semver struct {
	nums [3]int
	pre  []string
	ok   bool
}

func parseSemver(s string) semver {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(strings.TrimPrefix(s, "v"), "V")
	s, _, _ = strings.Cut(s, "+")
	var v semver
	core, pre, hasPre := strings.Cut(s, "-")
	if hasPre {
		v.pre = strings.Split(pre, ".")
	}
	fields := strings.Split(core, ".")
	if core == "" || len(fields) > 3 {
		return semver{}
	}
	for i, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil || n < 0 {
			return semver{}
		}
		v.nums[i] = n
	}
	v.ok = true
	return v
}

// cmpNums compares the numeric parts only.
func cmpNums(a, b semver) int {
	for i := range a.nums {
		if a.nums[i] != b.nums[i] {
			if a.nums[i] < b.nums[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

// compareSemver orders two parsed versions by semver precedence.
func compareSemver(a, b semver) int {
	if c := cmpNums(a, b); c != 0 {
		return c
	}
	switch {
	case len(a.pre) == 0 && len(b.pre) == 0:
		return 0
	case len(a.pre) == 0:
		return 1
	case len(b.pre) == 0:
		return -1
	}
	for i := 0; i < len(a.pre) && i < len(b.pre); i++ {
		x, y := a.pre[i], b.pre[i]
		nx, ex := strconv.Atoi(x)
		ny, ey := strconv.Atoi(y)
		switch {
		case ex == nil && ey == nil:
			if nx != ny {
				if nx < ny {
					return -1
				}
				return 1
			}
		case ex == nil:
			return -1
		case ey == nil:
			return 1
		default:
			if c := strings.Compare(x, y); c != 0 {
				return c
			}
		}
	}
	switch {
	case len(a.pre) < len(b.pre):
		return -1
	case len(a.pre) > len(b.pre):
		return 1
	}
	return 0
}

// CompareVersions orders version strings: semver precedence, a leading "v"
// ignored; a string that is not a version is older than any version.
func CompareVersions(a, b string) int {
	va, vb := parseSemver(a), parseSemver(b)
	switch {
	case va.ok && vb.ok:
		return compareSemver(va, vb)
	case va.ok:
		return 1
	case vb.ok:
		return -1
	}
	return strings.Compare(a, b)
}

// SortReleases sorts releases newest first: by appVersion, then by
// addonVersion.
func SortReleases(rs []AddonRelease) {
	sort.SliceStable(rs, func(i, j int) bool {
		if c := CompareVersions(rs[i].AppVersion, rs[j].AppVersion); c != 0 {
			return c > 0
		}
		return CompareVersions(rs[i].AddonVersion, rs[j].AddonVersion) > 0
	})
}

// LatestRelease is the recommended release (the "latest" of
// k8s_addon_versions): the newest ACTIVE one; without one, the newest
// EXPERIMENTAL, END_OF_SUPPORT or unmarked one. END_OF_LIFE and INACTIVE
// releases are never picked.
func LatestRelease(rs []AddonRelease) (AddonRelease, bool) {
	sorted := append([]AddonRelease(nil), rs...)
	SortReleases(sorted)
	for _, r := range sorted {
		if NormalizeMode(r.Mode) == ModeActive {
			return r, true
		}
	}
	for _, r := range sorted {
		switch NormalizeMode(r.Mode) {
		case ModeEndOfLife, ModeInactive:
		default:
			return r, true
		}
	}
	return AddonRelease{}, false
}
