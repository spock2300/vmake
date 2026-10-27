package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/spock2300/vmake/pkg/api"
)

var updateSkillCLIRef = flag.Bool("update-skill-ref", false, "rewrite the checked-in skill CLI reference snapshot")

var (
	skillGoFenceRE      = regexp.MustCompile("(?is)```go(?:lang)?[^\n]*\n(.*?)```")
	skillBashFenceRE    = regexp.MustCompile("(?ms)```(?:bash|sh)[^\n]*\n(.*?)```")
	skillShellCommentRE = regexp.MustCompile(`\s+#.*$`)
	skillHeadingRefRE   = regexp.MustCompile("SKILL\\.md - ([^\n`]+?)(?:`|$)")
	skillFileRefRE      = regexp.MustCompile(`(?:examples|references)/[A-Za-z0-9_./-]+\.md`)
	skillLineRefRE      = regexp.MustCompile(`[A-Za-z0-9_./-]+\.(?:go|md|json|sh|py):\d+`)
	skillAPIRefRE       = regexp.MustCompile(`\bapi\.([A-Z][A-Za-z0-9_]*)`)
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

func TestSkillCLIExamples(t *testing.T) {
	for rel, content := range skillMarkdownFiles(t) {
		if rel == "references/cli.md" {
			continue
		}
		for _, example := range skillCLIExamples(content) {
			args := splitSkillShellArgs(example)
			if len(args) == 0 || args[0] != "vmake" {
				continue
			}
			if err := validateSkillCLICommand(RootCmd, args[1:]); err != nil {
				t.Errorf("%s: %v: %s", rel, err, example)
			}
		}
	}
}

func skillCLIExamples(content string) []string {
	var examples []string
	add := func(line string) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			return
		}
		line = strings.TrimPrefix(line, "$ ")
		line = strings.TrimSpace(skillShellCommentRE.ReplaceAllString(line, ""))
		for _, part := range strings.FieldsFunc(line, func(r rune) bool {
			return r == '&' || r == '|' || r == ';'
		}) {
			part = strings.TrimSpace(part)
			if part == "vmake" || strings.HasPrefix(part, "vmake ") {
				examples = append(examples, part)
			}
		}
	}
	for _, block := range skillBashFenceRE.FindAllStringSubmatch(content, -1) {
		for _, line := range strings.Split(block[1], "\n") {
			add(line)
		}
	}
	return examples
}

func splitSkillShellArgs(line string) []string {
	var args []string
	var current strings.Builder
	quote := rune(0)
	flush := func() {
		if current.Len() > 0 {
			args = append(args, current.String())
			current.Reset()
		}
	}
	for _, r := range line {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				current.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
		case r == ' ' || r == '\t':
			flush()
		default:
			current.WriteRune(r)
		}
	}
	flush()
	return args
}

func validateSkillCLICommand(root *cobra.Command, args []string) error {
	cmd := root
	var positional []string
	for i := 0; i < len(args); i++ {
		token := strings.Trim(args[i], "[]")
		if token == "" {
			continue
		}
		if token == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if strings.HasPrefix(token, "-") && token != "-" {
			name, _, hasValue := splitSkillCLIFlag(token)
			flag := lookupSkillCLIFlag(cmd, name)
			if flag == nil {
				return fmt.Errorf("unknown flag %q for %q", token, cmd.CommandPath())
			}
			if !hasValue && flag.NoOptDefVal == "" {
				if i+1 >= len(args) {
					return fmt.Errorf("flag %q needs a value", token)
				}
				i++
			}
			continue
		}
		if sub := skillSubCommand(cmd, token); sub != nil {
			cmd = sub
			continue
		}
		if cmd.HasSubCommands() {
			return fmt.Errorf("unknown subcommand %q for %q", token, cmd.CommandPath())
		}
		positional = append(positional, token)
	}
	if cmd.Args != nil {
		if err := cmd.Args(cmd, positional); err != nil {
			return err
		}
	}
	return nil
}

func splitSkillCLIFlag(token string) (name, value string, hasValue bool) {
	body := strings.TrimLeft(token, "-")
	if idx := strings.IndexByte(body, '='); idx >= 0 {
		return body[:idx], body[idx+1:], true
	}
	if len(token) == 2 || strings.HasPrefix(token, "--") {
		return body, "", false
	}
	return body[:1], body[1:], true
}

func lookupSkillCLIFlag(cmd *cobra.Command, name string) *pflag.Flag {
	if flag := cmd.Flags().Lookup(name); flag != nil {
		return flag
	}
	if flag := cmd.PersistentFlags().Lookup(name); flag != nil {
		return flag
	}
	if flag := cmd.InheritedFlags().Lookup(name); flag != nil {
		return flag
	}
	if len(name) != 1 {
		return nil
	}
	if flag := cmd.Flags().ShorthandLookup(name); flag != nil {
		return flag
	}
	if flag := cmd.PersistentFlags().ShorthandLookup(name); flag != nil {
		return flag
	}
	return cmd.InheritedFlags().ShorthandLookup(name)
}

func skillSubCommand(cmd *cobra.Command, name string) *cobra.Command {
	for _, sub := range cmd.Commands() {
		if sub.Name() == name {
			return sub
		}
	}
	return nil
}

var removedSkillCLIFlags = []string{
	"--require-tests",
	"--allow-dry-run-writes",
	"--check",
	"--plan",
	"--output",
}

func TestSkillRemovedCLIFlags(t *testing.T) {
	for rel, content := range skillMarkdownFiles(t) {
		for _, flag := range removedSkillCLIFlags {
			pattern := regexp.MustCompile(regexp.QuoteMeta(flag) + `\b`)
			if pattern.MatchString(content) {
				t.Errorf("%s: documents removed flag %s", rel, flag)
			}
		}
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

type skillAPITypeInfo struct {
	methods map[string]map[string]bool
	embeds  map[string][]string
}

func loadSkillAPITypeInfo(t *testing.T) skillAPITypeInfo {
	t.Helper()
	dir := filepath.Join("..", "..", "pkg", "api")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	info := skillAPITypeInfo{
		methods: map[string]map[string]bool{},
		embeds:  map[string][]string{},
	}
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil || len(d.Recv.List) == 0 {
					continue
				}
				typ := skillAPITypeName(d.Recv.List[0].Type)
				if typ == "" {
					continue
				}
				if info.methods[typ] == nil {
					info.methods[typ] = map[string]bool{}
				}
				info.methods[typ][d.Name.Name] = true
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					typeSpec, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					structType, ok := typeSpec.Type.(*ast.StructType)
					if !ok {
						continue
					}
					for _, field := range structType.Fields.List {
						if len(field.Names) != 0 {
							continue
						}
						if embedded := skillAPITypeName(field.Type); embedded != "" {
							info.embeds[typeSpec.Name.Name] = append(info.embeds[typeSpec.Name.Name], embedded)
						}
					}
				}
			}
		}
	}
	return info
}

func skillAPITypeName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return skillAPITypeName(e.X)
	default:
		return ""
	}
}

func (info skillAPITypeInfo) allMethods(typ string) map[string]bool {
	result := map[string]bool{}
	visited := map[string]bool{}
	var walk func(string)
	walk = func(current string) {
		if visited[current] {
			return
		}
		visited[current] = true
		for name := range info.methods[current] {
			result[name] = true
		}
		for _, embedded := range info.embeds[current] {
			walk(embedded)
		}
	}
	walk(typ)
	return result
}

func TestSkillMethodReferences(t *testing.T) {
	info := loadSkillAPITypeInfo(t)
	allowed := map[string]map[string]bool{
		"p":      info.allMethods("Package"),
		"ctx":    map[string]bool{},
		"t":      info.allMethods("Target"),
		"target": info.allMethods("Target"),
	}
	for _, ctxType := range []string{"BuildContext", "ConfigContext", "RequireContext", "InstallContext", "CleanContext"} {
		for method := range info.allMethods(ctxType) {
			allowed["ctx"][method] = true
		}
	}
	for rel, content := range skillMarkdownFiles(t) {
		for blockIndex, block := range skillGoFenceRE.FindAllStringSubmatch(content, -1) {
			file, err := parseSkillBlock(block[1])
			if err != nil {
				continue
			}
			seen := map[string]bool{}
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				receiver, ok := selector.X.(*ast.Ident)
				if !ok {
					return true
				}
				methods, known := allowed[receiver.Name]
				if !known {
					return true
				}
				key := receiver.Name + "." + selector.Sel.Name
				if methods[selector.Sel.Name] || seen[key] {
					return true
				}
				seen[key] = true
				t.Errorf("%s: go block %d calls %s, which is not a method on the corresponding pkg/api type",
					rel, blockIndex+1, key)
				return true
			})
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
			if f.Name.Name != "main" {
				t.Errorf("%s: go block %d uses package %q, want main", rel, i+1, f.Name.Name)
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
