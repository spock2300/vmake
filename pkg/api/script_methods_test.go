package api

import (
	"reflect"
	"slices"
	"testing"
)

func visibleScriptMethods(t *testing.T) map[string][]string {
	t.Helper()
	result := make(map[string][]string)
	for name, value := range YaegiSymbols() {
		if !value.IsValid() {
			continue
		}
		typ := value.Type()
		if typ.NumMethod() == 0 {
			continue
		}
		methods := make([]string, 0, typ.NumMethod())
		for i := 0; i < typ.NumMethod(); i++ {
			methods = append(methods, typ.Method(i).Name)
		}
		slices.Sort(methods)
		result[name] = methods
	}
	return result
}

func TestScriptMethodManifestMatchesVisibleSurface(t *testing.T) {
	visible := visibleScriptMethods(t)
	manifests := map[string]map[string][]string{
		"allowlist": scriptMethodAllowlist,
		"pending":   scriptMethodPending,
	}
	for label, manifest := range manifests {
		for typeName, methods := range manifest {
			if _, ok := visible[typeName]; !ok {
				t.Errorf("%s names unregistered type %q", label, typeName)
				continue
			}
			for i := 1; i < len(methods); i++ {
				if methods[i-1] >= methods[i] {
					t.Errorf("%s/%s is not sorted and unique", label, typeName)
					break
				}
			}
		}
	}
	for typeName, allowed := range scriptMethodAllowlist {
		for _, method := range allowed {
			if slices.Contains(scriptMethodPending[typeName], method) {
				t.Errorf("%s.%s is both allowed and pending", typeName, method)
			}
		}
	}
	for typeName, methods := range visible {
		expected := append([]string{}, scriptMethodAllowlist[typeName]...)
		expected = append(expected, scriptMethodPending[typeName]...)
		slices.Sort(expected)
		if !slices.Equal(expected, methods) {
			t.Errorf("%s visible=%v, allowlist+pending=%v; classify the difference explicitly", typeName, methods, expected)
		}
	}
}

func TestScriptTypesDoNotPromoteExportedFields(t *testing.T) {
	symbols := YaegiSymbols()
	for _, name := range []string{"Package", "BuildContext", "ConfigContext", "DepConfigContext", "InstallContext", "CleanContext", "RequireContext", "ConfigAccessor", "PackageMeta", "TargetRegistry", "InstallItemHolder"} {
		value, ok := symbols[name]
		if !ok {
			t.Fatalf("%s is not registered", name)
		}
		checkExportedFields(t, name, value.Type(), "")
	}
}

func checkExportedFields(t *testing.T, typeName string, typ reflect.Type, prefix string) {
	t.Helper()
	for typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Anonymous {
			checkExportedFields(t, typeName, field.Type, prefix+field.Name+".")
			continue
		}
		if field.PkgPath == "" {
			t.Errorf("%s promotes exported field %s%s", typeName, prefix, field.Name)
		}
	}
}
