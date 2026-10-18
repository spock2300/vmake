package build

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spock2300/vmake/internal/jsonio"
)

func TestActionRecordRejectsOldCorruptAndIncompleteOutputs(t *testing.T) {
	dir := t.TempDir()
	writeAssemblyFixture(t, dir, "input", "input")
	writeAssemblyFixture(t, dir, "output", "output")
	recordPath := filepath.Join(dir, "action.json")
	inputs, outputs := []string{"input"}, []string{"output"}
	if err := saveActionRecord(recordPath, "signature", dir, inputs, outputs); err != nil {
		t.Fatal(err)
	}
	if !actionUpToDate(recordPath, "signature", dir, inputs, outputs) {
		t.Fatal("successful action rejected")
	}
	var record actionRecord
	if err := jsonio.Load(recordPath, &record); err != nil {
		t.Fatal(err)
	}
	record.Version = actionRecordVersion - 1
	if err := jsonio.Save(recordPath, record); err != nil {
		t.Fatal(err)
	}
	if actionUpToDate(recordPath, "signature", dir, inputs, outputs) {
		t.Fatal("old action record reused")
	}
	if err := os.WriteFile(recordPath, []byte("{incomplete"), 0644); err != nil {
		t.Fatal(err)
	}
	if actionUpToDate(recordPath, "signature", dir, inputs, outputs) {
		t.Fatal("corrupt action record reused")
	}
	if err := saveActionRecord(recordPath, "signature", dir, inputs, outputs); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "output")); err != nil {
		t.Fatal(err)
	}
	if actionUpToDate(recordPath, "signature", dir, inputs, outputs) {
		t.Fatal("missing output reused")
	}
}

func TestActionDecisionReasons(t *testing.T) {
	dir := t.TempDir()
	writeAssemblyFixture(t, dir, "input", "input")
	writeAssemblyFixture(t, dir, "output", "output")
	recordPath := filepath.Join(dir, "action.json")
	inputs, outputs := []string{"input"}, []string{"output"}

	if got := checkAction(recordPath, "sig", dir, inputs, outputs); got.UpToDate || got.Reason != "no action record" {
		t.Fatalf("missing record decision = %+v", got)
	}
	if err := saveActionRecord(recordPath, "sig", dir, inputs, outputs); err != nil {
		t.Fatal(err)
	}
	if got := checkAction(recordPath, "other", dir, inputs, outputs); got.UpToDate || got.Reason != "command, tool or environment changed" {
		t.Fatalf("signature decision = %+v", got)
	}
	if got := checkAction(recordPath, "sig", dir, inputs, []string{"missing"}); got.UpToDate || got.Reason != "output missing or unreadable" {
		t.Fatalf("missing output decision = %+v", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "input"), []byte("changed"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := checkAction(recordPath, "sig", dir, inputs, outputs); got.UpToDate || got.Reason != "inputs changed" {
		t.Fatalf("changed input decision = %+v", got)
	}
}
