package buildscript

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
	"github.com/traefik/yaegi/stdlib/unrestricted"

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

func LoadBuildScript(src Source) (*api.Package, error) {
	i := interp.New(interp.Options{})
	if err := i.Use(stdlib.Symbols); err != nil {
		return nil, fmt.Errorf("yaegi use stdlib: %w", err)
	}
	if err := i.Use(unrestricted.Symbols); err != nil {
		return nil, fmt.Errorf("yaegi use unrestricted: %w", err)
	}
	if err := i.Use(yaegiExports()); err != nil {
		return nil, fmt.Errorf("yaegi use vmake symbols: %w", err)
	}

	merged, err := mergeGoSources(src.Dir)
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
	if dir := src.Dir; dir != "" {
		pkg.SetScriptDir(dir)
	}

	origDir, err := os.Getwd()
	if err != nil {
		vlog.Fatal("get working directory: %v", err)
	}
	defer os.Chdir(origDir)
	if dir := src.Dir; dir != "" {
		if err := os.Chdir(dir); err != nil {
			vlog.Fatal("chdir to %s: %v", dir, err)
		}
	}

	mainFunc(pkg)

	if fn := pkg.GetPackageFunc(); fn != nil {
		fn(pkg)
	}

	if len(pkg.GetRequireFuncs()) > 0 {
		ctx := api.NewRequireContextForConfig(nil, pkg.Options, pkg.GetRequireFuncs())
		for _, fn := range pkg.GetRequireFuncs() {
			fn(ctx)
		}
		pkg.GetRequires().AddInfos(ctx.GetRequires()...)
	}

	return pkg, nil
}

func mergeGoSources(dir string) (string, error) {
	goFiles, err := ListGoFiles(dir)
	if err != nil {
		return "", fmt.Errorf("list go files: %w", err)
	}
	if len(goFiles) == 0 {
		return "", fmt.Errorf("no .go files found in %s", dir)
	}

	sort.Strings(goFiles)

	fset := token.NewFileSet()
	imports := make(map[string]string)
	var body bytes.Buffer
	pkgName := "main"

	for _, goFile := range goFiles {
		srcBytes, err := os.ReadFile(goFile)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", goFile, err)
		}
		f, err := parser.ParseFile(fset, filepath.Base(goFile), srcBytes, parser.AllErrors)
		if err != nil {
			return "", fmt.Errorf("parse %s: %w", goFile, err)
		}
		if f.Name.Name != "" {
			pkgName = f.Name.Name
		}

		for _, decl := range f.Decls {
			if genDecl, ok := decl.(*ast.GenDecl); ok && genDecl.Tok == token.IMPORT {
				for _, spec := range genDecl.Specs {
					imp := spec.(*ast.ImportSpec)
					pathVal := strings.Trim(imp.Path.Value, `"`)
					alias := ""
					if imp.Name != nil {
						alias = imp.Name.Name
					}
					if existing, exists := imports[pathVal]; exists && existing != alias {
						return "", fmt.Errorf("conflicting import aliases for %q: %q vs %q in %s",
							pathVal, existing, alias, goFile)
					}
					imports[pathVal] = alias
				}
			} else {
				var buf bytes.Buffer
				printer.Fprint(&buf, fset, decl)
				body.WriteString(buf.String())
				body.WriteString("\n\n")
			}
		}
	}

	var sb strings.Builder
	sb.WriteString("package " + pkgName + "\n\n")

	if len(imports) > 0 {
		sortedPaths := make([]string, 0, len(imports))
		for path := range imports {
			sortedPaths = append(sortedPaths, path)
		}
		sort.Strings(sortedPaths)
		sb.WriteString("import (\n")
		for _, path := range sortedPaths {
			alias := imports[path]
			if alias != "" {
				sb.WriteString(fmt.Sprintf("\t%s %q\n", alias, path))
			} else {
				sb.WriteString(fmt.Sprintf("\t%q\n", path))
			}
		}
		sb.WriteString(")\n\n")
	}

	sb.WriteString(body.String())
	return sb.String(), nil
}
