package provision

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

// Имена диска и floating IP — не длиннее 64 (maxLength в спецификации
// compute): имя ВМ до 63 символов плюс «-boot» или «fip-…-<время>» давали
// 422 при создании.
func TestFitNameAndBootFIPName(t *testing.T) {
	valid := regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9.\-_]*$`)
	long := "evo-" + strings.Repeat("a", 59)
	other := "evo-" + strings.Repeat("a", 58) + "b"
	if got := FitName("web", "-boot"); got != "web-boot" {
		t.Errorf("short name changed: %q", got)
	}
	d1, d2 := FitName(long, "-boot"), FitName(other, "-boot")
	for _, n := range []string{d1, d2} {
		if len(n) > MaxNameLen || !strings.HasSuffix(n, "-boot") || !valid.MatchString(n) {
			t.Errorf("disk name %q (%d)", n, len(n))
		}
	}
	if d1 == d2 {
		t.Errorf("two VMs, one disk name: %q", d1)
	}
	now := time.Unix(1792000000, 0)
	if got := BootFIPName("web", now); !strings.HasPrefix(got, "fip-web-") {
		t.Errorf("short FIP name: %q", got)
	}
	f := BootFIPName(long, now)
	if len(f) > MaxNameLen || !strings.HasPrefix(f, BootFIPPrefix(long)) || !valid.MatchString(f) {
		t.Errorf("FIP name %q (%d), prefix %q", f, len(f), BootFIPPrefix(long))
	}
	if far := BootFIPName(long, time.Unix(1<<35, 0)); len(far) > MaxNameLen {
		t.Errorf("FIP name of a far date: %q (%d)", far, len(far))
	}
}
