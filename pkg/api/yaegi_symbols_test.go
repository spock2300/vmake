package api

import (
	"go/importer"
	"go/token"
	"go/types"
	"strings"
	"testing"
)

func TestYaegiSymbolsComplete(t *testing.T) {
	fset := token.NewFileSet()
	imp := importer.ForCompiler(fset, "source", nil)
	pkg, err := imp.Import("github.com/spock2300/vmake/pkg/api")
	if err != nil {
		imp = importer.Default()
		pkg, err = imp.Import("github.com/spock2300/vmake/pkg/api")
		if err != nil {
			t.Skipf("cannot import pkg/api: %v", err)
		}
	}

	syms := YaegiSymbols()
	sc := pkg.Scope()

	hostOnly := map[string]bool{
		"BindBuildRuntime":       true,
		"SnapshotTarget":         true,
		"LocalContainerID":       true,
		"IsLocalPackageID":       true,
		"LocalRelPath":           true,
		"CanonicalizePackageRef": true,
		"ValidatePackageRef":     true,
		"ValidateContainerID":    true,
	}

	var missing []string
	for _, name := range sc.Names() {
		if hostOnly[name] || strings.HasPrefix(name, "Bind") {
			continue
		}
		o := sc.Lookup(name)
		if !o.Exported() {
			continue
		}

		switch o.(type) {
		case *types.Func:
			if _, ok := syms[name]; !ok {
				missing = append(missing, name+" (func)")
			}
		case *types.Var:
			if _, ok := syms[name]; !ok {
				missing = append(missing, name+" (var)")
			}
		case *types.Const:
			if _, ok := syms[name]; !ok {
				missing = append(missing, name+" (const)")
			}
		case *types.TypeName:
			if _, ok := syms[name]; !ok {
				missing = append(missing, name+" (type)")
			}
		}
	}

	if len(missing) > 0 {
		t.Errorf("YaegiSymbols() is missing %d exported symbols:\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}

	for name := range syms {
		if sc.Lookup(name) == nil {
			t.Errorf("YaegiSymbols() has extra symbol %q not in pkg/api", name)
		}
	}
}

func TestYaegiSymbolsNotNil(t *testing.T) {
	syms := YaegiSymbols()
	for _, name := range []string{
		"BindBuildRuntime", "SnapshotTarget", "BindSelectVersion",
		"LocalContainerID", "IsLocalPackageID", "LocalRelPath",
		"CanonicalizePackageRef", "ValidatePackageRef", "ValidateContainerID",
	} {
		if _, exists := syms[name]; exists {
			t.Fatalf("native function %s is exposed to scripts", name)
		}
	}
	for name, v := range syms {
		if !v.IsValid() {
			t.Errorf("symbol %q has invalid reflect.Value", name)
		}
	}
}
