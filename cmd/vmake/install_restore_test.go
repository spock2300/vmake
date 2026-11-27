package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spock2300/vmake/internal/fs"
)

func TestFailedInstallRestoresLastDelivery(t *testing.T) {
	root := canonicalTempDir(t)
	project, state := filepath.Join(root, "project"), filepath.Join(root, "state")
	prefix := filepath.Join(root, "deliverables")
	writeHealthyDefinition(t, filepath.Join(state, "extensions", "fixture", "healthy", "toolchain.json"), "healthy")
	writeExtensionFile(t, filepath.Join(project, "vmake.json"), `{}`)
	writeExtensionFile(t, filepath.Join(project, "input"), "previous")
	setScript := func(hook string) {
		imports := `"github.com/spock2300/vmake/pkg/api"`
		if strings.Contains(hook, "os.WriteFile") {
			imports += `; "os"; "time"`
		}
		writeExtensionFile(t, filepath.Join(project, "build.go"), `package main
import (`+imports+`)
func Main(p *api.Package) {
    p.OnBuild(func(c *api.BuildContext) { c.Target("job").SetKind(api.TargetVoid).SetBuildFunc(func(p *api.Package) error { return nil }); c.AddInstalls("input", "asset.txt") })
    p.OnInstall(func(c *api.InstallContext) { `+hook+` })
}`)
	}
	runWithPrefix := func(targetPrefix, wantError string, extraArgs ...string) {
		t.Helper()
		args := append([]string{"build", "-i", "-y", "--toolchain", "healthy"}, extraArgs...)
		if targetPrefix != "" {
			args = append(args, "--prefix", targetPrefix)
		}
		out, stderr, code := runSubprocess(t, project, state, []string{"VMAKE_TRUST_ALL=1"}, args...)
		if wantError == "" && code != 0 || wantError != "" && (code == 0 || !strings.Contains(out+stderr, wantError)) {
			t.Fatalf("build exit %d, wanted %q: %s%s", code, wantError, out, stderr)
		}
	}
	run := func(wantError string) {
		t.Helper()
		runWithPrefix(prefix, wantError)
	}
	readAt := func(dir, name string) []byte {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	read := func(name string) []byte {
		t.Helper()
		return readAt(prefix, name)
	}
	setScript("")
	run("")
	oldManifest := read("manifest.json")
	writeExtensionFile(t, filepath.Join(project, "input"), "next")
	for _, scenario := range []struct{ hook, want string }{
		{`panic("broken installer")`, "broken installer"},
		{`c.AddInstalls("input", "manifest.json/conflict")`, "write manifest"},
	} {
		setScript(scenario.hook)
		run(scenario.want)
		if string(read("asset.txt")) != "previous" || !bytes.Equal(read("manifest.json"), oldManifest) || string(read(".vmake-install")) != "vmake\n" {
			t.Fatal("previous delivery changed after installation failed")
		}
	}

	killInHook := func(targetPrefix string) {
		t.Helper()
		ready := filepath.Join(root, "ready-"+filepath.Base(targetPrefix))
		_ = os.Remove(ready)
		setScript(fmt.Sprintf(`if err:=os.WriteFile(%q,[]byte("ready"),0600); err!=nil {panic(err)}; time.Sleep(time.Minute)`, ready))
		args := []string{"build", "-i", "-y", "--toolchain", "healthy"}
		if targetPrefix != "" {
			args = append(args, "--prefix", targetPrefix)
		}
		child := newSubprocess(t, "TestExtensionBootstrapChild", project, []string{"VMAKE_TRUST_ALL=1", "VMAKE_TEST_EXT_DIR=" + state}, args...)
		var output bytes.Buffer
		child.Stdout, child.Stderr = &output, &output
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = child.Process.Kill() })
		deadline := time.Now().Add(60 * time.Second)
		for {
			if _, err := os.Stat(ready); err == nil {
				break
			}
			if time.Now().After(deadline) {
				_ = child.Process.Kill()
				_ = child.Wait()
				t.Fatalf("install hook did not start: %s", output.String())
			}
			time.Sleep(25 * time.Millisecond)
		}
		if err := child.Process.Kill(); err != nil {
			t.Fatal(err)
		}
		if err := child.Wait(); err == nil {
			t.Fatal("killed installer exited successfully")
		}
	}

	killInHook(prefix)
	if err := os.Remove(filepath.Join(prefix, ".vmake-install")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prefix, "partial.txt"), []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package){p.OnBuild(func(c *api.BuildContext){panic("build before install")})}`)
	run("build before install")
	if string(read("asset.txt")) != "previous" || !bytes.Equal(read("manifest.json"), oldManifest) || string(read(".vmake-install")) != "vmake\n" {
		t.Fatal("failed build after interruption left a partial delivery")
	}
	setScript(`panic("retry fails")`)
	run("retry fails")
	if string(read("asset.txt")) != "previous" || !bytes.Equal(read("manifest.json"), oldManifest) {
		t.Fatal("interrupted installer lost previous delivery")
	}
	setScript("")
	run("")
	if string(read("asset.txt")) != "next" {
		t.Fatal("subsequent install did not publish new files")
	}

	writeExtensionFile(t, filepath.Join(project, "input"), "project-prev")
	setScript("")
	runWithPrefix("", "")
	writeExtensionFile(t, filepath.Join(project, "input"), "project-next")
	killInHook("")
	out, stderr, code := runSubprocess(t, project, state, []string{"VMAKE_TRUST_ALL=1"}, "distclean")
	if code != 0 {
		t.Fatalf("distclean exit %d: %s%s", code, out, stderr)
	}
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package){p.OnBuild(func(c *api.BuildContext){panic("build after distclean")})}`)
	runWithPrefix("", "build after distclean")
	if _, err := os.Stat(filepath.Join(project, "install")); !os.IsNotExist(err) {
		t.Fatalf("distclean recovery resurrected project install: %v", err)
	}

	if fs.SymlinksSupported() {
		realTarget := filepath.Join(root, "symlink-target")
		if err := os.MkdirAll(realTarget, 0755); err != nil {
			t.Fatal(err)
		}
		linkPrefix := filepath.Join(root, "symlink-prefix")
		if err := os.Symlink(realTarget, linkPrefix); err != nil {
			t.Fatal(err)
		}
		writeExtensionFile(t, filepath.Join(project, "input"), "linked-v1")
		setScript("")
		runWithPrefix(linkPrefix, "")
		info, err := os.Lstat(linkPrefix)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("install replaced symlink prefix with a directory: %v, %v", info, err)
		}
		if string(readAt(realTarget, "asset.txt")) != "linked-v1" {
			t.Fatal("symlink target did not receive installed asset")
		}
		writeExtensionFile(t, filepath.Join(project, "input"), "linked-v2")
		setScript(`panic("linked install fails")`)
		runWithPrefix(linkPrefix, "linked install fails")
		info, err = os.Lstat(linkPrefix)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("rollback replaced symlink prefix: %v, %v", info, err)
		}
		if string(readAt(realTarget, "asset.txt")) != "linked-v1" {
			t.Fatal("symlink target lost previous delivery after failure")
		}
	}
}

func TestManifestPathModuleRoundTrip(t *testing.T) {
	root := canonicalTempDir(t)
	project, state := filepath.Join(root, "project"), filepath.Join(root, "state")
	writeHealthyDefinition(t, filepath.Join(state, "extensions", "fixture", "healthy", "toolchain.json"), "healthy")
	writeExtensionFile(t, filepath.Join(project, "vmake.json"), `{"modules":{"fixture/math":{"path":"modules/math"}}}`)
	writeExtensionFile(t, filepath.Join(project, "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package){p.OnRequire(func(c *api.RequireContext){c.AddRequires("fixture/math")});p.OnBuild(func(c *api.BuildContext){c.Target("job").SetKind(api.TargetVoid).UseRequires()})}`)
	writeExtensionFile(t, filepath.Join(project, "modules/math/build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package){p.OnBuild(func(c *api.BuildContext){c.Target("calc").SetKind(api.TargetStatic).SetPrebuilt("lib.a")})}`)
	writeExtensionFile(t, filepath.Join(project, "modules/math/lib.a"), "archive")
	run := func(wantError string, args ...string) {
		t.Helper()
		out, stderr, code := runSubprocess(t, project, state, []string{"VMAKE_TRUST_ALL=1"}, args...)
		if wantError == "" && code != 0 || wantError != "" && (code == 0 || !strings.Contains(out+stderr, wantError)) {
			t.Fatalf("%v: exit %d, wanted %q: %s%s", args, code, wantError, out, stderr)
		}
	}
	run("", "build", "-i", "-y", "--toolchain", "healthy")
	mf := filepath.Join(project, "install", "manifest.json")
	run("", "build", "--manifest", mf, "-y", "--toolchain", "healthy")
	run("", "manifest", "checkout", mf, "fixture/math")
	b, err := os.ReadFile(mf)
	if err != nil {
		t.Fatal(err)
	}
	var doc installManifest
	if err = json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	for i, e := range doc.Packages {
		if e.Name == "fixture/math" {
			doc.Packages[i].Path = "."
		}
	}
	b, err = json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	writeExtensionFile(t, mf, string(b))
	run("does not match module", "build", "--manifest", mf, "-y", "--toolchain", "healthy")
	bad := filepath.Join(project, "bad.json")
	missing, _ := json.Marshal(filepath.Join(root, "unavailable.git"))
	writeExtensionFile(t, bad, fmt.Sprintf(`{"packages":[{"name":"fixture/bad","source":"native","version":"0.0.0-path","url":%s,"ref":"v0.0.0-path"}]}`, missing))
	run("fixture/bad", "manifest", "checkout", bad)
	run("resolving v0.0.0-path", "build", "--manifest", bad, "-y", "--toolchain", "healthy")
}
