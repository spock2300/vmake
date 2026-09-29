package main

import (
	"flag"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/spock2300/vmake/pkg/api"
)

var updateSkillCLIRef = flag.Bool("update-skill-ref", false, "rewrite the checked-in skill CLI reference snapshot")

var (
	skillGoFenceRE    = regexp.MustCompile("(?is)```go(?:lang)?[^\n]*\n(.*?)```")
	skillHeadingRefRE = regexp.MustCompile("SKILL\\.md - ([^\n`]+?)(?:`|$)")
	skillFileRefRE    = regexp.MustCompile(`(?:examples|references)/[A-Za-z0-9_./-]+\.md`)
	skillLineRefRE    = regexp.MustCompile(`[A-Za-z0-9_./-]+\.(?:go|md|json|sh|py):\d+`)
	skillAPIRefRE     = regexp.MustCompile(`\bapi\.([A-Z][A-Za-z0-9_]*)`)
)

var skillPredeclaredCalls = map[string]bool{
	"any": true, "append": true, "bool": true, "byte": true, "cap": true,
	"clear": true, "close": true, "complex": true, "complex64": true, "complex128": true,
	"copy": true, "delete": true, "error": true, "float32": true, "float64": true,
	"imag": true, "int": true, "int8": true, "int16": true, "int32": true, "int64": true,
	"len": true, "make": true, "max": true, "min": true, "new": true, "panic": true,
	"print": true, "println": true, "real": true, "recover": true, "rune": true,
	"string": true, "uint": true, "uint8": true, "uint16": true, "uint32": true,
	"uint64": true, "uintptr": true,
}

func skillMarkdownFiles(t *testing.T) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := fs.WalkDir(skillFS, "skills", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		data, err := skillFS.ReadFile(path)
		if err != nil {
			return err
		}
		files[strings.TrimPrefix(path, "skills/vmake/")] = strings.ReplaceAll(string(data), "\r\n", "\n")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func normalizeSkillHeading(s string) string {
	s = strings.ReplaceAll(s, "`", "")
	return strings.Join(strings.Fields(s), " ")
}

func TestSkillCLISnapshot(t *testing.T) {
	path := filepath.Join("skills", "vmake", "references", "cli.md")
	got := generateCLIRef(RootCmd)
	if *updateSkillCLIRef {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ReplaceAll(string(want), "\r\n", "\n") != got {
		t.Fatalf("references/cli.md is stale; run: go test ./cmd/vmake -run '^TestSkillCLISnapshot$' -update-skill-ref")
	}
}

func TestSkillCrossReferences(t *testing.T) {
	skillData, err := skillFS.ReadFile("skills/vmake/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	headings := map[string]bool{}
	for _, line := range strings.Split(strings.ReplaceAll(string(skillData), "\r\n", "\n"), "\n") {
		if !strings.HasPrefix(line, "##") {
			continue
		}
		headings[normalizeSkillHeading(strings.TrimSpace(strings.TrimLeft(line, "#")))] = true
	}
	for rel, content := range skillMarkdownFiles(t) {
		for i, line := range strings.Split(content, "\n") {
			if !strings.Contains(line, "SKILL.md") {
				continue
			}
			matches := skillHeadingRefRE.FindAllStringSubmatch(line, -1)
			if len(matches) == 0 {
				t.Errorf("%s:%d: SKILL.md reference must use `SKILL.md - <Heading>`: %s", rel, i+1, strings.TrimSpace(line))
				continue
			}
			for _, m := range matches {
				ref := normalizeSkillHeading(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(m[1]), ".")))
				if !headings[ref] {
					t.Errorf("%s:%d: unknown SKILL.md heading %q", rel, i+1, strings.TrimSpace(m[1]))
				}
			}
		}
		for _, ref := range skillFileRefRE.FindAllString(content, -1) {
			if _, err := fs.Stat(skillFS, "skills/vmake/"+ref); err != nil {
				t.Errorf("%s: missing referenced file %s", rel, ref)
			}
		}
	}
}

func TestSkillNoFileLineRefs(t *testing.T) {
	for rel, content := range skillMarkdownFiles(t) {
		for i, line := range strings.Split(content, "\n") {
			if match := skillLineRefRE.FindString(line); match != "" {
				t.Errorf("%s:%d: file line reference %q; refer to symbols or section names instead", rel, i+1, match)
			}
		}
	}
}

func TestSkillAPISymbols(t *testing.T) {
	symbols := api.YaegiSymbols()
	for rel, content := range skillMarkdownFiles(t) {
		for i, line := range strings.Split(content, "\n") {
			for _, m := range skillAPIRefRE.FindAllStringSubmatch(line, -1) {
				if _, ok := symbols[m[1]]; !ok {
					t.Errorf("%s:%d: api.%s is not exposed to scripts via pkg/api", rel, i+1, m[1])
				}
			}
		}
	}
}

func apiCallableNames(t *testing.T) map[string]bool {
	t.Helper()
	dir := filepath.Join("..", "..", "pkg", "api")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	names := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				names[d.Name.Name] = true
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					if s, ok := spec.(*ast.TypeSpec); ok {
						names[s.Name.Name] = true
					}
				}
			}
		}
	}
	return names
}

func parseSkillBlock(block string) (*ast.File, error) {
	fset := token.NewFileSet()
	if f, err := parser.ParseFile(fset, "block.go", block, 0); err == nil {
		return f, nil
	}
	wrapped := "package main\nfunc _() {\n" + block + "\n}\n"
	if f, err := parser.ParseFile(fset, "block.go", wrapped, 0); err == nil {
		return f, nil
	}
	return parser.ParseFile(fset, "block.go", "package main\n"+block, 0)
}

func collectSkillDefs(f *ast.File, defs map[string]bool) {
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncDecl:
			defs[x.Name.Name] = true
		case *ast.TypeSpec:
			defs[x.Name.Name] = true
		case *ast.ValueSpec:
			for _, name := range x.Names {
				defs[name.Name] = true
			}
		case *ast.AssignStmt:
			if x.Tok == token.DEFINE {
				for _, lhs := range x.Lhs {
					if id, ok := lhs.(*ast.Ident); ok {
						defs[id.Name] = true
					}
				}
			}
		case *ast.RangeStmt:
			if x.Tok == token.DEFINE {
				if id, ok := x.Key.(*ast.Ident); ok {
					defs[id.Name] = true
				}
				if id, ok := x.Value.(*ast.Ident); ok {
					defs[id.Name] = true
				}
			}
		}
		return true
	})
}

func TestSkillGoBlocksParse(t *testing.T) {
	callable := apiCallableNames(t)
	for rel, content := range skillMarkdownFiles(t) {
		blocks := skillGoFenceRE.FindAllStringSubmatch(content, -1)
		files := make([]*ast.File, len(blocks))
		for i, m := range blocks {
			f, err := parseSkillBlock(m[1])
			if err != nil {
				t.Errorf("%s: go block %d does not parse: %v", rel, i+1, err)
				continue
			}
			files[i] = f
		}
		defs := map[string]bool{}
		for _, f := range files {
			if f != nil {
				collectSkillDefs(f, defs)
			}
		}
		for i, f := range files {
			if f == nil {
				continue
			}
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				id, ok := call.Fun.(*ast.Ident)
				if !ok || defs[id.Name] || skillPredeclaredCalls[id.Name] || callable[id.Name] {
					return true
				}
				t.Errorf("%s: go block %d calls undefined %s(...)", rel, i+1, id.Name)
				return true
			})
		}
	}
}
