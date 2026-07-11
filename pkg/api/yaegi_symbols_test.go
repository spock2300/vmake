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

	var missing []string
	for _, name := range sc.Names() {
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
	for name, v := range syms {
		if !v.IsValid() {
			t.Errorf("symbol %q has invalid reflect.Value", name)
		}
	}
}
