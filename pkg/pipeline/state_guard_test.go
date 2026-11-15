package pipeline

import (
	"reflect"
	"testing"
)

// Configuration state is written by the configuration phase and committed to
// the context; keeping it behind accessors makes "committed, therefore read
// only" a property of the type rather than a convention. AllOptions stays
// exported: command-level option lookup helpers take the whole context and
// their unit tests build it directly (DD-31).
func TestRuntimeContextHidesConfigurationState(t *testing.T) {
	hidden := []string{
		"AllKConfigs",
		"BufferedGlobalFlags",
		"ChipProfile",
		"ChipProfiles",
		"DerivedOptions",
		"GlobalMacroDefines",
		"GlobalOptions",
	}
	typ := reflect.TypeOf(RuntimeContext{})
	for _, name := range hidden {
		if field, ok := typ.FieldByName(name); ok {
			t.Fatalf("RuntimeContext still exposes %s (field %v); use its accessor", name, field)
		}
	}
}
