package main

import (
	"errors"
	"testing"

	"github.com/spock2300/vmake/pkg/pipeline"
)

func TestEncodeInspectGraphError(t *testing.T) {
	inspection := &pipeline.Inspection{}
	inspection.AttachGraph(nil, nil, errors.New("graph exploded"))

	data, diagnostics := encodeInspect(inspection, false)
	if data != nil {
		t.Fatalf("data = %#v, want nil", data)
	}
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %#v, want exactly one", diagnostics)
	}
	got := diagnostics[0]
	if got.Severity != "error" || got.Code != "VMAKE_E_INSPECT" || got.Message != "graph exploded" {
		t.Errorf("diagnostic = %#v, want error VMAKE_E_INSPECT graph exploded", got)
	}
}
