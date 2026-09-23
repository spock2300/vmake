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
	record.Version = buildFormatVersion - 1
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
