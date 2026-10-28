package buildscript

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/traefik/yaegi/interp"

	"github.com/spock2300/vmake/internal/gosrc"
	"github.com/spock2300/vmake/internal/scriptcall"
	"github.com/spock2300/vmake/internal/scriptfs"
	"github.com/spock2300/vmake/internal/yaegibase"
	"github.com/spock2300/vmake/pkg/api"
	"github.com/spock2300/vmake/pkg/toolchain"
)

func yaegiExports() interp.Exports {
	return interp.Exports{
		"github.com/spock2300/vmake/pkg/api/api":             api.YaegiSymbols(),
		"github.com/spock2300/vmake/pkg/toolchain/toolchain": toolchain.YaegiSymbols(),
	}
}

// ScriptTrustChecker decides whether scripts from a remote repository may be
// executed. The source carries the resolved commit so trust can be bound to an
// exact revision; an empty commit must be treated as untrusted.
type ScriptTrustChecker func(src Source) error

// LoadBuildScript loads a buildscript without a trust checker. Remote sources
// are rejected by LoadBuildScriptWithTrust; use that entry point for remote
// packages.
func LoadBuildScript(src Source) (*api.Package, error) {
	return LoadBuildScriptWithTrust(src, nil)
}

// LoadBuildScriptWithTrust gates remote scripts on a trust decision before
// interpretation. Remote scripts without a repository identity or a trust
// checker are refused rather than executed by default.
func LoadBuildScriptWithTrust(src Source, trustChecker ScriptTrustChecker) (*api.Package, error) {
	if src.IsRemote() {
		if src.Repo == "" {
			return nil, fmt.Errorf("remote buildscript %q has no repository identity", src.Name)
		}
		if trustChecker == nil {
			return nil, fmt.Errorf("remote buildscript %q has no trust checker", src.Name)
		}
		if err := trustChecker(src); err != nil {
			return nil, fmt.Errorf("untrusted repository %q: %w", src.Repo, err)
		}
	}
	return loadBuildScript(src)
}

func loadBuildScript(src Source) (_ *api.Package, err error) {
	defer scriptcall.Recover(&err)
	interpOpts := interp.Options{Stderr: io.Discard}
	if src.Script.ModulePath != "" && src.Script.ModuleRoot != "" {
		projectDir := src.Script.ProjectDir
		if projectDir == "" {
			projectDir = src.Script.ModuleRoot
		}
		goPath := filepath.Join(projectDir, ".vmake", "script-gopath")
		interpOpts.GoPath = goPath
		interpOpts.SourcecodeFilesystem = moduleFS{
			prefix: filepath.Join(goPath, "src", filepath.FromSlash(src.Script.ModulePath)),
			root:   src.Script.ModuleRoot,
		}
	}
	i, err := yaegibase.NewWithOptions(interpOpts, yaegiExports())
	if err != nil {
		return nil, err
	}

	scriptFS := scriptfs.New(src.Dir)
	if src.Dir != "" {
		if err := i.Use(scriptFS.Exports()); err != nil {
			return nil, fmt.Errorf("use script fs %s: %w", src.Dir, err)
		}
	}

	merged, err := gosrc.MergeGoSources(src.Dir)
	if err != nil {
		return nil, fmt.Errorf("merge go files in %s: %w", src.Dir, err)
	}

	if _, err := i.Eval(merged); err != nil {
		return nil, fmt.Errorf("yaegi eval %s: %w", src.Name, err)
	}

	v, err := i.Eval("Main")
	if err != nil {
		return nil, fmt.Errorf("yaegi lookup Main in %s: %w", src.Name, err)
	}
	mainFunc, ok := v.Interface().(func(*api.Package))
	if !ok {
		return nil, fmt.Errorf("yaegi: Main in %s has wrong signature: %T", src.Name, v.Interface())
	}

	pkg := api.NewPackage()
	scriptFS.SetBaseFunc(func() string {
		if dir := pkg.SourceDir(); dir != "" {
			return dir
		}
		return src.Dir
	})
	pkg.SetName(src.Name)
	if dir := src.Dir; dir != "" {
		pkg.SetScriptDir(dir)
	}

	api.RunScriptSafe(src.Name, func() {
		mainFunc(pkg)
	})

	if fn := pkg.GetPackageFunc(); fn != nil {
		api.RunScriptSafe(src.Name, func() {
			fn(pkg)
		})
	}

	if len(pkg.GetRequireFuncs()) > 0 {
		ctx := api.NewRequireContextForConfig(src.Name, nil, pkg.Options, pkg.GetRequireFuncs())
		for _, fn := range pkg.GetRequireFuncs() {
			api.RunScriptSafe(src.Name, func() {
				fn(ctx)
			})
		}
		pkg.GetRequires().AddInfos(ctx.GetRequires()...)
	}

	return pkg, nil
}
