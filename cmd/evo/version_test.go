package main

import (
	"bytes"
	"encoding/json"
	"runtime/debug"
	"strings"
	"testing"
)

func TestResolveVersion_LdflagsWin(t *testing.T) {
	info := &debug.BuildInfo{
		Main: debug.Module{Version: "v9.9.9"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "fromvcs"},
			{Key: "vcs.time", Value: "2000-01-01T00:00:00Z"},
		},
	}
	v, c, d := resolveVersion("1.0.0-rc.1", "abc123", "2026-10-14T00:00:00Z", info)
	if v != "1.0.0-rc.1" || c != "abc123" || d != "2026-10-14T00:00:00Z" {
		t.Errorf("ldflags values overwritten: %q %q %q", v, c, d)
	}
}

func TestResolveVersion_FromBuildInfo(t *testing.T) {
	info := &debug.BuildInfo{
		Main: debug.Module{Version: "v1.2.3"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "deadbeef"},
			{Key: "vcs.time", Value: "2026-10-01T10:00:00Z"},
		},
	}
	v, c, d := resolveVersion("dev", "none", "unknown", info)
	if v != "1.2.3" || c != "deadbeef" || d != "2026-10-01T10:00:00Z" {
		t.Errorf("got %q %q %q", v, c, d)
	}
}

func TestResolveVersion_DevelAndNoInfoKeepDefaults(t *testing.T) {
	for _, info := range []*debug.BuildInfo{nil, {Main: debug.Module{Version: "(devel)"}}} {
		v, c, d := resolveVersion("dev", "none", "unknown", info)
		if v != "dev" || c != "none" || d != "unknown" {
			t.Errorf("info %+v: got %q %q %q", info, v, c, d)
		}
	}
}

var testVersion = versionInfo{
	Version: "1.0.0-rc.1", Commit: "abc123", Date: "2026-10-14T12:00:00Z",
	Go: "go1.26.9", OS: "linux", Arch: "amd64",
}

func TestPrintVersion_Text(t *testing.T) {
	var buf bytes.Buffer
	if err := printVersion(&buf, false, testVersion); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	for _, want := range []string{"evo 1.0.0-rc.1\n", "abc123", "2026-10-14T12:00:00Z", "go1.26.9", "linux/amd64"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
}

func TestPrintVersion_JSON(t *testing.T) {
	var buf bytes.Buffer
	if err := printVersion(&buf, true, testVersion); err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("not one JSON object: %v: %q", err, buf.String())
	}
	want := map[string]string{
		"version": "1.0.0-rc.1", "commit": "abc123", "date": "2026-10-14T12:00:00Z",
		"go": "go1.26.9", "os": "linux", "arch": "amd64",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: got %q, want %q", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("unexpected keys: %v", got)
	}
}

func TestRun_VersionExitsZero(t *testing.T) {
	if got := run([]string{"version", "--json"}); got != 0 {
		t.Errorf("run(version --json) = %d, want 0", got)
	}
}
