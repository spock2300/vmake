package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spock2300/vmake/internal/jsonio"
)

func TestBuildReportActualInstallsAndInvalidation(t *testing.T) {
	dir := t.TempDir()
	state, root := filepath.Join(dir, "state"), filepath.Join(dir, "project")
	writeHealthyDefinition(t, filepath.Join(state, "extensions", "fixture", "healthy", "toolchain.json"), "healthy")
	writeExtensionFile(t, filepath.Join(root, "build.go"), `package main
import ("os"; "fmt"; "github.com/spock2300/vmake/pkg/api")
func Main(p *api.Package) {
 p.SetRoot(true)
 p.OnInstall(func(c *api.InstallContext) { prefix, err := os.ReadFile("install-prefix"); if err != nil { panic(err) }; c.SetPrefix(string(prefix)) })
 p.OnConfig(func(c *api.ConfigContext) { c.Option("assets").SetType(api.OptionBool).SetDefault(true) })
 p.OnBuild(func(c *api.BuildContext) {
  c.Target("bindings").SetKind(api.TargetObject).SetPrebuilt("bindings-input")
  c.Target("runtime").SetKind(api.TargetStatic).SetPrebuilt("library-input").AddPublicIncludes("include", "@*.h")
  c.Target("firmware").SetKind(api.TargetBinary).SetPrebuilt("firmware-input").AddPostLinkOutputs("metadata.bin")
  c.Target("bundle").SetKind(api.TargetVoid).AddPostLinkOutputs("payload").SetBuildFunc(func(p *api.Package) error {
   if _, e := os.Stat("fail"); e == nil { return fmt.Errorf("fixture failure") }
   if _, e := os.Stat("change-config"); e == nil { if e := os.WriteFile(".vmake/config.json", []byte("{}"), 0644); e != nil { return e } }
   return os.WriteFile("payload", []byte("built bytes"), 0644)
  })
  if c.Bool("assets") { c.AddInstalls("resources", "share") }
 })
}
`)
	cfg := `{"version":"1","global":{"toolchain":"healthy"},"entries":{"project":{"options":{"assets":true}}}}`
	writeExtensionFile(t, filepath.Join(root, ".vmake", "config.json"), cfg)
	writeExtensionFile(t, filepath.Join(root, ".vmake", "plain.json"), strings.Replace(cfg, "true", "false", 1))
	writeExtensionFile(t, filepath.Join(root, "resources", "asset.elf"), "asset bytes")
	writeExtensionFile(t, filepath.Join(root, "bindings-input"), "object bytes")
	writeExtensionFile(t, filepath.Join(root, "library-input"), "library bytes")
	writeExtensionFile(t, filepath.Join(root, "firmware-input"), "firmware bytes")
	writeExtensionFile(t, filepath.Join(root, "metadata.bin"), "post-link bytes")
	writeExtensionFile(t, filepath.Join(root, "include", "api.h"), "header bytes")
	writeExtensionFile(t, filepath.Join(root, "include", "notes.txt"), "not a header")
	prefix := filepath.Join(dir, "custom-install")
	writeExtensionFile(t, filepath.Join(root, "install-prefix"), prefix)
	writeExtensionFile(t, filepath.Join(prefix, "share", "stale.elf"), "old bytes")
	run := func(ok bool, args ...string) {
		t.Helper()
		out, err := extensionCommand(t, state, root, args...)
		if (err == nil) != ok {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	read := func() buildReport {
		t.Helper()
		var report buildReport
		if err := jsonio.Load(reportPath(root), &report); err != nil {
			t.Fatal(err)
		}
		return report
	}
	run(true, "build", "-iV")
	first := read()
	if _, err := os.Stat(filepath.Join(prefix, "share", "stale.elf")); err != nil {
		t.Fatal("fixture must retain stale destination", err)
	}
	if first.Status != "succeeded" || len(first.Artifacts) != 3 {
		t.Fatalf("%+v", first)
	}
	objectFound := false
	for _, target := range first.Targets {
		if target.Target == "bindings" {
			objectFound = true
			if target.Kind != "object" || len(target.Outputs) != 0 {
				t.Fatalf("object target published intermediate outputs: %+v", target)
			}
		}
	}
	if !objectFound {
		t.Fatal("object target was not executed")
	}
	rawReport, err := os.ReadFile(reportPath(root))
	if err != nil {
		t.Fatal(err)
	}
	var rawDocument struct {
		Targets []struct {
			Target  string `json:"target"`
			Kind    string `json:"kind"`
			Outputs []any  `json:"outputs"`
		} `json:"targets"`
	}
	if err := json.Unmarshal(rawReport, &rawDocument); err != nil {
		t.Fatal(err)
	}
	for _, target := range rawDocument.Targets {
		if target.Target == "bindings" && (target.Kind != "object" || target.Outputs == nil || len(target.Outputs) != 0) {
			t.Fatalf("object target outputs must be an empty JSON array: %+v", target)
		}
	}
	for _, item := range first.Artifacts {
		if strings.Contains(item.BuildPath, "stale") || item.SHA256 == "" || item.InstallPath == "" || strings.HasSuffix(item.BuildPath, ".a") || filepath.Base(item.BuildPath) == "payload" {
			t.Fatalf("%+v", item)
		}
		hash, size, err := fileDigest(item.InstallPath)
		if err != nil || hash != item.SHA256 || size != item.Size {
			t.Fatalf("installed file identity: %+v, %v", item, err)
		}
		if strings.HasSuffix(item.BuildPath, "asset.elf") && (item.InstallPath != filepath.Join(prefix, "share", "asset.elf") || item.Target != "") {
			t.Fatalf("%+v", item)
		}
		if filepath.Base(item.BuildPath) == "metadata.bin" && (item.InstallPath != filepath.Join(prefix, "bin", "metadata.bin") || item.Target != "firmware") {
			t.Fatalf("%+v", item)
		}
	}
	run(true, "build", "-i")
	second := read()
	if second.BuildID == first.BuildID || len(second.Artifacts) != 3 {
		t.Fatalf("%+v", second)
	}
	run(true, "build")
	third := read()
	if third.Status != "succeeded" || third.Artifacts == nil || len(third.Artifacts) != 0 || len(third.Targets) == 0 {
		t.Fatalf("%+v", third)
	}
	run(true, "rebuild", "-i")
	if rebuilt := read(); rebuilt.Status != "succeeded" || len(rebuilt.Artifacts) != 3 {
		t.Fatalf("%+v", rebuilt)
	}
	run(true, "build", "-i", "--install-type", "sdk")
	sdk := read()
	if sdk.Status != "succeeded" || len(sdk.Artifacts) != 5 {
		t.Fatalf("%+v", sdk)
	}
	headerFound := false
	for _, item := range sdk.Artifacts {
		if filepath.Base(item.BuildPath) == "notes.txt" {
			t.Fatalf("filtered public include in artifacts: %+v", item)
		}
		if filepath.Base(item.BuildPath) == "api.h" {
			headerFound = true
			if item.Target != "runtime" || item.InstallPath != filepath.Join(prefix, "include", "api.h") {
				t.Fatalf("public include artifact: %+v", item)
			}
		}
	}
	if !headerFound {
		t.Fatal("public include was not recorded")
	}
	run(true, "config", "use", "plain.json")
	run(true, "build", "-i")
	plain := read()
	if plain.ConfigFile != "plain.json" || len(plain.Artifacts) != 2 {
		t.Fatalf("%+v", plain)
	}
	writeExtensionFile(t, filepath.Join(root, "fail"), "")
	run(false, "build")
	failed := read()
	if failed.Status != "failed" || len(failed.Artifacts) != 0 || failed.BuildID == plain.BuildID {
		t.Fatalf("%+v", failed)
	}
	if err := os.Remove(filepath.Join(root, "fail")); err != nil {
		t.Fatal(err)
	}
	run(true, "config", "use", "config.json")
	writeExtensionFile(t, filepath.Join(root, "change-config"), "")
	run(false, "build")
	if changed := read(); changed.Status != "failed" || !strings.Contains(changed.Error, "configuration changed") {
		t.Fatalf("%+v", changed)
	}
	run(true, "distclean")
	if _, err := os.Stat(reportPath(root)); !os.IsNotExist(err) {
		t.Fatalf("report survived clean: %v", err)
	}
}

func TestBuildReportNeverSucceedsBeforeCompletion(t *testing.T) {
	for _, failure := range []error{context.Canceled, errors.New("failed"), nil} {
		r := &buildReport{Root: t.TempDir(), Status: "running"}
		result := failure
		r.finish(&result)
		if result == nil || r.Status == "succeeded" {
			t.Fatalf("%+v / %v", r, result)
		}
		if errors.Is(failure, context.Canceled) && r.Status != "cancelled" {
			t.Fatal(r.Status)
		}
		data, err := os.ReadFile(reportPath(r.Root))
		if err != nil {
			t.Fatal(err)
		}
		var saved buildReport
		if err := json.Unmarshal(data, &saved); err != nil {
			t.Fatal(err)
		}
		if saved.Status != r.Status || len(saved.Artifacts) != 0 {
			t.Fatalf("%+v", saved)
		}
	}
}

func TestBuildReportPanicCannotPublishSuccess(t *testing.T) {
	r := &buildReport{Root: t.TempDir(), completed: true}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("panic was swallowed")
			}
		}()
		var err error
		defer r.finish(&err)
		panic("unexpected interruption")
	}()
	var saved buildReport
	if err := jsonio.Load(reportPath(r.Root), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Status != "failed" || len(saved.Artifacts) != 0 {
		t.Fatalf("%+v", saved)
	}
}
