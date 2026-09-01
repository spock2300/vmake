package buildscript

import (
	"fmt"

	"github.com/traefik/yaegi/interp"

	"github.com/spock2300/vmake/internal/gosrc"
	"github.com/spock2300/vmake/internal/scriptfs"
	"github.com/spock2300/vmake/internal/yaegibase"
	"github.com/spock2300/vmake/pkg/api"
	vlog "github.com/spock2300/vmake/pkg/log"
	"github.com/spock2300/vmake/pkg/toolchain"
)

func yaegiExports() interp.Exports {
	return interp.Exports{
		"github.com/spock2300/vmake/pkg/api/api":             api.YaegiSymbols(),
		"github.com/spock2300/vmake/pkg/toolchain/toolchain": toolchain.YaegiSymbols(),
	}
}

// ScriptTrustChecker decides whether scripts from a remote repository may be
// executed. It may prompt or record trust; an error refuses execution.
type ScriptTrustChecker func(repo string) error

func LoadBuildScript(src Source) (*api.Package, error) {
	return LoadBuildScriptWithTrust(src, nil)
}

// LoadBuildScriptWithTrust gates remote scripts on a trust decision before
// interpretation. Scripts from untrusted repositories are refused.
func LoadBuildScriptWithTrust(src Source, trustChecker ScriptTrustChecker) (*api.Package, error) {
	if src.IsRemote() && src.Repo != "" && trustChecker != nil {
		if err := trustChecker(src.Repo); err != nil {
			return nil, fmt.Errorf("untrusted repository %q: %w", src.Repo, err)
		}
	}
	return loadBuildScript(src)
}

func loadBuildScript(src Source) (*api.Package, error) {
	i, err := yaegibase.New(yaegiExports())
	if err != nil {
		return nil, err
	}

	if src.Dir != "" {
		if err := i.Use(scriptfs.New(src.Dir).Exports()); err != nil {
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
	pkg.SetName(src.Name)
	if dir := src.Dir; dir != "" {
		pkg.SetScriptDir(dir)
	}

	runScriptFunc(src.Name, func() {
		mainFunc(pkg)
	})

	if fn := pkg.GetPackageFunc(); fn != nil {
		runScriptFunc(src.Name, func() {
			fn(pkg)
		})
	}

	if len(pkg.GetRequireFuncs()) > 0 {
		ctx := api.NewRequireContextForConfig(src.Name, nil, pkg.Options, pkg.GetRequireFuncs())
		for _, fn := range pkg.GetRequireFuncs() {
			runScriptFunc(src.Name, func() {
				fn(ctx)
			})
		}
		pkg.GetRequires().AddInfos(ctx.GetRequires()...)
	}

	return pkg, nil
}

func runScriptFunc(pkgName string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			var bse *api.BuildScriptError
			if existing, ok := r.(*api.BuildScriptError); ok {
				bse = existing
				if bse.Package == "" {
					bse.Package = pkgName
				}
			} else {
				bse = &api.BuildScriptError{
					Package: pkgName,
					Op:      "panic",
					Err:     fmt.Errorf("%v", r),
				}
			}
			vlog.Fatal("%v", bse)
		}
	}()
	fn()
}
