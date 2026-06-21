package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinkStatic_RemovesStaleMembers(t *testing.T) {
	if _, err := exec.LookPath("ar"); err != nil {
		t.Skip("ar not available")
	}
	if _, err := exec.LookPath("cc"); err != nil {
		t.Skip("cc not available")
	}

	dir := t.TempDir()

	mkObj := func(name, sym string) string {
		cPath := filepath.Join(dir, name+".c")
		if err := os.WriteFile(cPath, []byte("int "+sym+"(void){return 0;}\n"), 0644); err != nil {
			t.Fatalf("write %s.c: %v", name, err)
		}
		oPath := filepath.Join(dir, name+".o")
		if out, err := exec.Command("cc", "-c", cPath, "-o", oPath).CombinedOutput(); err != nil {
			t.Fatalf("cc -c %s: %v\n%s", name, err, out)
		}
		return oPath
	}

	archive := filepath.Join(dir, "lib.a")
	linker := &Linker{ccPath: "cc", arPath: "ar"}

	objA := mkObj("a", "a")
	objB := mkObj("b", "b")

	if err := linker.LinkStatic([]string{objA, objB}, archive); err != nil {
		t.Fatalf("first LinkStatic: %v", err)
	}

	if err := linker.LinkStatic([]string{objA}, archive); err != nil {
		t.Fatalf("second LinkStatic: %v", err)
	}

	out, err := exec.Command("ar", "t", archive).CombinedOutput()
	if err != nil {
		t.Fatalf("ar t: %v\n%s", err, out)
	}
	members := strings.TrimSpace(string(out))

	if strings.Contains(members, "b.o") {
		t.Fatalf("stale member b.o still present in archive:\n%s", members)
	}
	if !strings.Contains(members, "a.o") {
		t.Fatalf("expected member a.o missing in archive:\n%s", members)
	}
}
