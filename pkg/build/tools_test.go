package build

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/toolchain"
)

func toolProbeFixture(t *testing.T) (*toolchain.Toolchain, string, func() int) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell version probe fixture")
	}
	dir := t.TempDir()
	counter := filepath.Join(dir, "probes")
	failure := filepath.Join(dir, "fail")
	program := filepath.Join(dir, "compiler.sh")
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
	script := "#!/bin/sh\nprintf 'probe\\n' >> " + quote(counter) + "\nif [ -f " + quote(failure) + " ]; then printf 'probe failure\\n' >&2; exit 1; fi\nprintf 'fixture compiler version 1\\n'\n"
	if err := os.WriteFile(program, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return &toolchain.Toolchain{Name: "fixture", Tools: toolchain.Tools{CC: program, CXX: program, AR: program}}, failure, func() int {
		t.Helper()
		data, err := os.ReadFile(counter)
		if err != nil {
			t.Fatal(err)
		}
		return strings.Count(string(data), "probe\n")
	}
}

func TestSessionToolsReuseNormalizedPlatformAndRefreshNextSession(t *testing.T) {
	tc, _, probes := toolProbeFixture(t)
	session := NewSession(nil)
	first, err := session.ResolveTools(tc, api.Platform{})
	if err != nil {
		t.Fatal(err)
	}
	again, err := session.ResolveTools(tc, api.Platform{OS: runtime.GOOS})
	if err != nil {
		t.Fatal(err)
	}
	if probes() != 2 || first.CCKey() != again.CCKey() {
		t.Fatalf("same normalized toolchain was reprobed: %d", probes())
	}
	data, err := os.ReadFile(tc.Tools.CC)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tc.Tools.CC, append(data, []byte(":\n")...), 0755); err != nil {
		t.Fatal(err)
	}
	fresh, err := NewSession(nil).ResolveTools(tc, api.Platform{})
	if err != nil {
		t.Fatal(err)
	}
	if probes() != 4 || first.CCVersion != fresh.CCVersion || first.CCKey() == fresh.CCKey() {
		t.Fatalf("new session did not revalidate same-version compiler bytes: probes=%d", probes())
	}
}

func TestSessionToolsSeparateSameNameDefinitions(t *testing.T) {
	tc, _, probes := toolProbeFixture(t)
	session := NewSession(nil)
	first, err := session.ResolveTools(tc, api.Platform{})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(tc.Tools.CXX)
	if err != nil {
		t.Fatal(err)
	}
	alternate := filepath.Join(filepath.Dir(tc.Tools.CXX), "alternate.sh")
	if err := os.WriteFile(alternate, data, 0755); err != nil {
		t.Fatal(err)
	}
	tc.Tools.CXX = alternate
	second, err := session.ResolveTools(tc, api.Platform{})
	if err != nil {
		t.Fatal(err)
	}
	if probes() != 4 || second.CXX != alternate || first.CXX == second.CXX {
		t.Fatalf("same-name tool definitions shared stale results: probes=%d CXX=%s", probes(), second.CXX)
	}
}

func TestSessionToolsSeparateEnvironmentAndPlatform(t *testing.T) {
	tc, _, probes := toolProbeFixture(t)
	session := NewSession(nil)
	t.Setenv("VMAKE_TOOL_PROBE_TEST", "first")
	if _, err := session.ResolveTools(tc, api.Platform{}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VMAKE_TOOL_PROBE_TEST", "second")
	if _, err := session.ResolveTools(tc, api.Platform{}); err != nil {
		t.Fatal(err)
	}
	if probes() != 4 {
		t.Fatalf("environment change reused old result: %d probes", probes())
	}
	bare, err := session.ResolveTools(tc, api.Platform{OS: "none"})
	if err != nil {
		t.Fatal(err)
	}
	if probes() != 6 || bare.targetOS != "none" {
		t.Fatalf("target OS change reused old result: %d probes, OS=%s", probes(), bare.targetOS)
	}
	platform := api.Platform{OS: "none", Triple: "arm-none-eabi"}
	if _, err := session.ResolveTools(tc, platform); err != nil {
		t.Fatal(err)
	}
	if _, err := session.ResolveTools(tc, platform); err != nil {
		t.Fatal(err)
	}
	if probes() != 8 {
		t.Fatalf("triple change was not isolated and reused: %d probes", probes())
	}
}

func TestSessionToolsRetryAfterProbeFailure(t *testing.T) {
	tc, failure, probes := toolProbeFixture(t)
	if err := os.WriteFile(failure, nil, 0644); err != nil {
		t.Fatal(err)
	}
	session := NewSession(nil)
	if _, err := session.ResolveTools(tc, api.Platform{}); err == nil {
		t.Fatal("failed version probe accepted")
	}
	if probes() != 1 {
		t.Fatalf("failed CC probe continued probing: %d", probes())
	}
	if err := os.Remove(failure); err != nil {
		t.Fatal(err)
	}
	if _, err := session.ResolveTools(tc, api.Platform{}); err != nil {
		t.Fatal(err)
	}
	if _, err := session.ResolveTools(tc, api.Platform{}); err != nil {
		t.Fatal(err)
	}
	if probes() != 3 {
		t.Fatalf("failed resolution poisoned cache or successful retry was not cached: %d", probes())
	}
}
