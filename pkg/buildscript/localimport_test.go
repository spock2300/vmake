package buildscript

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
)

func writeLocalImportProject(t *testing.T, root, helperValue string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "helper"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "util"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "vmake.json"), []byte(`{
		"script": {"module": "example.com/app", "root": "."}
	}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "build.go"), []byte(`package main

import (
	"github.com/spock2300/vmake/pkg/api"
	"example.com/app/helper"
)

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary).
			AddCFlags(helper.Flags())
	})
}
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "util", "util.go"), []byte(
		"package util\n\nfunc Value() string {\n\treturn \""+helperValue+"\"\n}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "helper", "helper.go"), []byte(
		"package helper\n\nimport \"example.com/app/util\"\n\nfunc Flags() []string {\n\treturn []string{\"-DHELPER=\"+util.Value()}\n}\n"), 0644); err != nil {
		t.Fatal(err)
	}
}

func loadTargetFlags(t *testing.T, src Source) []string {
	t.Helper()
	var pkg *api.Package
	var err error
	if src.IsRemote() {
		pkg, err = LoadBuildScriptWithTrust(src, func(Source) error { return nil })
	} else {
		pkg, err = LoadBuildScript(src)
	}
	if err != nil {
		t.Fatal(err)
	}
	ctx := api.NewBuildContext(src.Name, nil)
	ctx.SetPackage(pkg)
	pkg.ExecBuildFuncs(src.Dir, func(fn api.BuildFunc) {
		fn(ctx)
	})
	target := ctx.GetTargets()["app"]
	if target == nil {
		t.Fatal("target app missing")
	}
	return target.CFlags()
}

func TestLocalImportInBuildScript(t *testing.T) {
	root := t.TempDir()
	writeLocalImportProject(t, root, "local")
	sources, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 {
		t.Fatalf("sources = %d", len(sources))
	}
	if sources[0].Script.ModulePath != "example.com/app" {
		t.Fatalf("script options = %+v", sources[0].Script)
	}
	flags := loadTargetFlags(t, sources[0])
	if len(flags) != 1 || flags[0] != "-DHELPER=local" {
		t.Fatalf("flags = %v", flags)
	}
}

func TestRemoteSourceUsesItsOwnModuleRoot(t *testing.T) {
	local := t.TempDir()
	writeLocalImportProject(t, local, "local")

	remote := t.TempDir()
	writeLocalImportProject(t, remote, "remote")
	src := Source{
		Name:   "test/remote",
		Path:   filepath.Join(remote, "build.go"),
		Dir:    remote,
		Origin: api.SourceRemote,
		Repo:   "test",
		Commit: "deadbeef",
	}
	opts, err := DetectScriptOptions(remote, remote)
	if err != nil {
		t.Fatal(err)
	}
	src.Script = opts
	flags := loadTargetFlags(t, src)
	if len(flags) != 1 || flags[0] != "-DHELPER=remote" {
		t.Fatalf("flags = %v", flags)
	}
}
