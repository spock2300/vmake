package buildscript

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
)

func TestPostLinkOutputDeclarations(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "build.go")
	script := `package main

import "github.com/spock2300/vmake/pkg/api"

func Main(p *api.Package) {
	p.OnBuild(func(ctx *api.BuildContext) {
		ctx.Target("app").SetKind(api.TargetBinary).
			AddPostLink("objcopy", "--only-keep-debug", "{output}", "{output}.debug").
			AddPostLinkOutputs("{output}.debug").AddPostLinkHex()
	})
}
`
	if err := os.WriteFile(path, []byte(script), 0644); err != nil {
		t.Fatal(err)
	}
	pkg, err := LoadBuildScript(Source{Name: "postlink-test", Path: path, Dir: dir, Origin: api.SourceLocal})
	if err != nil {
		t.Fatal(err)
	}
	ctx := api.NewBuildContext("postlink-test", nil)
	ctx.SetPackage(pkg)
	pkg.ExecBuildFuncs(dir, func(fn api.BuildFunc) { fn(ctx) })
	got := ctx.GetTargets()["app"].PostLinkOutputs()
	want := []string{"{output}.debug", "{output}.hex"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("outputs=%v, want %v", got, want)
	}
}
