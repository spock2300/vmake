package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDistCleanKeepsSourcesUnlessPurge(t *testing.T) {
	dir := t.TempDir()
	state, root := filepath.Join(dir, "state"), filepath.Join(dir, "project")
	writeExtensionFile(t, filepath.Join(root, "build.go"), `package main
import "github.com/spock2300/vmake/pkg/api"
func Main(p *api.Package) { p.SetRoot(true) }
`)
	writeExtensionFile(t, filepath.Join(root, ".vmake", "config.json"), `{"version":"1","global":{"toolchain":"host"}}`)

	tree := filepath.Join(root, ".vmake_deps", "official", "zlib")
	source := filepath.Join(tree, "src", "zlib.c")
	stateFile := filepath.Join(tree, "state.json")
	remoteOutput := filepath.Join(tree, "out", "owner", "buildkey", "build", "zlib.o")
	localOutput := filepath.Join(root, "build", "buildkey", "app.o")
	compileDB := filepath.Join(root, "build", "compile_commands.json")
	installBin := filepath.Join(root, "install", "bin", "app")
	for _, path := range []string{source, stateFile, remoteOutput, localOutput, compileDB, installBin} {
		writeExtensionFile(t, path, "x")
	}

	if out, err := extensionCommand(t, state, root, "distclean"); err != nil {
		t.Fatalf("distclean: %v\n%s", err, out)
	}
	for _, path := range []string{source, stateFile} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("distclean removed downloaded source %s: %v", path, err)
		}
	}
	for _, path := range []string{remoteOutput, localOutput, compileDB, installBin} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("distclean kept build artifact %s: %v", path, err)
		}
	}

	if out, err := extensionCommand(t, state, root, "distclean", "--purge-sources"); err != nil {
		t.Fatalf("distclean --purge-sources: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(root, ".vmake_deps")); !os.IsNotExist(err) {
		t.Fatalf(".vmake_deps survived distclean --purge-sources: %v", err)
	}
}
