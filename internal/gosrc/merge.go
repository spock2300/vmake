package gosrc

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

func ListGoFiles(dir string) ([]string, error) {
	ctx := build.Default
	ctx.GOOS = runtime.GOOS
	ctx.GOARCH = runtime.GOARCH
	ctx.CgoEnabled = false
	return listGoFiles(dir, ctx)
}

func listGoFiles(dir string, ctx build.Context) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		matched, err := ctx.MatchFile(dir, name)
		if err != nil {
			return nil, fmt.Errorf("select %s: %w", filepath.Join(dir, name), err)
		}
		if !matched {
			continue
		}
		fullPath := filepath.Join(dir, name)
		parsed, err := parser.ParseFile(token.NewFileSet(), fullPath, nil, parser.ImportsOnly)
		if err != nil {
			return nil, fmt.Errorf("parse imports in %s: %w", fullPath, err)
		}
		usesCgo := false
		for _, imp := range parsed.Imports {
			if imp.Path.Value == `"C"` || imp.Path.Value == "`C`" {
				usesCgo = true
				break
			}
		}
		if !usesCgo {
			files = append(files, fullPath)
		}
	}
	return files, nil
}

func MergeGoSources(dir string) (string, error) {
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
