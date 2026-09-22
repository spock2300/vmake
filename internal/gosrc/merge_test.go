package gosrc

import (
	"go/build"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"testing"
)

func TestListGoFilesBuildConstraints(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"build.go":                  "package main\n",
		"platform_windows.go":       "package main\n",
		"platform_linux.go":         "package main\n",
		"arch_amd64.go":             "package main\n",
		"arch_arm64.go":             "package main\n",
		"combined_windows_amd64.go": "package main\n",
		"tag_windows.go":            "//go:build windows && amd64\n\npackage main\n",
		"tag_unix.go":               "//go:build unix\n\npackage main\n",
		"tag_legacy.go":             "// +build linux,arm64\n\npackage main\n",
		"tag_custom.go":             "//go:build customtag\n\npackage main\n",
		"tag_cgo.go":                "//go:build cgo\n\npackage main\n",
		"without_cgo.go":            "//go:build !cgo\n\npackage main\n",
		"import_c.go":               "package main\nimport \"C\"\n",
		"ignored_test.go":           "package main\n",
		".hidden.go":                "package main\n",
		"_ignored.go":               "package main\n",
	}
	for name, source := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, test := range []struct {
		goos   string
		goarch string
		want   []string
	}{
		{"windows", "amd64", []string{"arch_amd64.go", "build.go", "combined_windows_amd64.go", "platform_windows.go", "tag_windows.go", "without_cgo.go"}},
		{"linux", "arm64", []string{"arch_arm64.go", "build.go", "platform_linux.go", "tag_legacy.go", "tag_unix.go", "without_cgo.go"}},
	} {
		t.Run(test.goos+"/"+test.goarch, func(t *testing.T) {
			ctx := build.Default
			ctx.GOOS, ctx.GOARCH, ctx.CgoEnabled = test.goos, test.goarch, false
			got, err := listGoFiles(dir, ctx)
			if err != nil {
				t.Fatal(err)
			}
			for i := range got {
				got[i] = filepath.Base(got[i])
			}
			sort.Strings(test.want)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("files = %v, want %v", got, test.want)
			}
		})
	}
}

func TestListGoFilesUsesHostPlatform(t *testing.T) {
	t.Setenv("GOOS", "plan9")
	t.Setenv("GOARCH", "wasm")
	t.Setenv("CGO_ENABLED", "1")
	dir := t.TempDir()
	name := "host_" + runtime.GOOS + "_" + runtime.GOARCH + ".go"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("//go:build !cgo\n\npackage main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := ListGoFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || filepath.Base(got[0]) != name {
		t.Fatalf("host selection = %v, want %s", got, name)
	}
}
