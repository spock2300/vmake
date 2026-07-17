package plugin

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

func writePluginFixture(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

const pluginJSON = `{
  "name": "demo",
  "version": "1.0.0",
  "description": "demo plugin",
  "entry": "src/main.go",
  "enabled": true
}
`

func TestLoad_BasicCommand(t *testing.T) {
	dir := t.TempDir()
	writePluginFixture(t, dir, map[string]string{
		"plugin.json": pluginJSON,
		"src/main.go": `package main

import (
	"github.com/spock2300/vmake/pkg/plugin"
	"github.com/spf13/cobra"
)

func Main(ctx *plugin.Context) {
	ctx.AddSubCommand(&cobra.Command{
		Use:   "greet",
		Short: "greeting",
		Run: func(cmd *cobra.Command, args []string) {
			ctx.AddGlobalCFlags("-greet")
		},
	})
}
`,
	})

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Info.Name != "demo" {
		t.Fatalf("plugin name = %q, want demo", loaded.Info.Name)
	}

	root := &cobra.Command{Use: "demo"}
	var gotCFlags []string
	ctx := &Context{
		PluginDir: dir,
		AddSubCommand: func(cmd *cobra.Command) {
			root.AddCommand(cmd)
		},
		AddGlobalCFlags: func(flags ...string) {
			gotCFlags = append(gotCFlags, flags...)
		},
	}
	RunMain(loaded, ctx)

	if len(root.Commands()) != 1 {
		t.Fatalf("expected 1 subcommand, got %d", len(root.Commands()))
	}
	root.SetArgs([]string{"greet"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(gotCFlags) != 1 || gotCFlags[0] != "-greet" {
		t.Fatalf("Run closure side effect not observed, gotCFlags=%v", gotCFlags)
	}
}

func TestLoad_ArgsValidator(t *testing.T) {
	dir := t.TempDir()
	writePluginFixture(t, dir, map[string]string{
		"plugin.json": pluginJSON,
		"src/main.go": `package main

import (
	"github.com/spock2300/vmake/pkg/plugin"
	"github.com/spf13/cobra"
)

func Main(ctx *plugin.Context) {
	ctx.AddSubCommand(&cobra.Command{
		Use:  "flash",
		Args: cobra.ExactArgs(1),
		Run:  func(cmd *cobra.Command, args []string) {},
	})
}
`,
	})

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	root := &cobra.Command{Use: "demo"}
	ctx := &Context{
		PluginDir: dir,
		AddSubCommand: func(cmd *cobra.Command) {
			root.AddCommand(cmd)
		},
	}
	RunMain(loaded, ctx)

	root.SetArgs([]string{"flash"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected ExactArgs(1) to reject 0 args")
	}
}

func TestLoad_Flags(t *testing.T) {
	dir := t.TempDir()
	writePluginFixture(t, dir, map[string]string{
		"plugin.json": pluginJSON,
		"src/main.go": `package main

import (
	"github.com/spock2300/vmake/pkg/plugin"
	"github.com/spf13/cobra"
)

func Main(ctx *plugin.Context) {
	var name string
	cmd := &cobra.Command{
		Use:  "say",
		Run: func(cmd *cobra.Command, args []string) {
			ctx.AddGlobalCFlags(name)
		},
	}
	cmd.Flags().StringVarP(&name, "name", "n", "world", "name")
	ctx.AddSubCommand(cmd)
}
`,
	})

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	root := &cobra.Command{Use: "demo"}
	var gotFlags []string
	ctx := &Context{
		PluginDir: dir,
		AddSubCommand: func(cmd *cobra.Command) {
			root.AddCommand(cmd)
		},
		AddGlobalCFlags: func(flags ...string) {
			gotFlags = append(gotFlags, flags...)
		},
	}
	RunMain(loaded, ctx)

	root.SetArgs([]string{"say", "-n", "alice"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(gotFlags) != 1 || gotFlags[0] != "alice" {
		t.Fatalf("flag not parsed: gotFlags=%v", gotFlags)
	}
}

func TestLoad_MultiFile(t *testing.T) {
	dir := t.TempDir()
	writePluginFixture(t, dir, map[string]string{
		"plugin.json": pluginJSON,
		"src/main.go": `package main

import (
	"github.com/spock2300/vmake/pkg/plugin"
)

func Main(ctx *plugin.Context) {
	ctx.AddGlobalCFlags(commonFlag())
}
`,
		"src/helpers.go": `package main

func commonFlag() string {
	return "-from-helper"
}
`,
	})

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	var gotFlags []string
	ctx := &Context{
		PluginDir: dir,
		AddGlobalCFlags: func(flags ...string) {
			gotFlags = append(gotFlags, flags...)
		},
	}
	RunMain(loaded, ctx)

	if len(gotFlags) != 1 || gotFlags[0] != "-from-helper" {
		t.Fatalf("multi-file call failed: gotFlags=%v", gotFlags)
	}
}

func TestLoad_WrongMainSignature(t *testing.T) {
	dir := t.TempDir()
	writePluginFixture(t, dir, map[string]string{
		"plugin.json": pluginJSON,
		"src/main.go": `package main

func Main() {}
`,
	})

	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error for wrong Main signature")
	}
}
