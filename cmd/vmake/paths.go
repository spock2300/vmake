package main

import (
	"os"
	"path/filepath"
)

var cachedProjectDir string

func findProjectDir() string {
	if cachedProjectDir != "" {
		return cachedProjectDir
	}
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, ".vmake")); err == nil {
			cachedProjectDir = dir
			return dir
		}
		if _, err := os.Stat(filepath.Join(dir, "build.go")); err == nil {
			cachedProjectDir = dir
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			wd, _ := os.Getwd()
			cachedProjectDir = wd
			return wd
		}
		dir = parent
	}
}

func getDepsDir() string {
	return filepath.Join(findProjectDir(), "vmake_deps")
}

func getReposDir() string {
	return filepath.Join(vmakeDir, "repos")
}

func getExtensionsDir() string {
	return filepath.Join(vmakeDir, "extensions")
}

func getToolchainsDir() string {
	return filepath.Join(vmakeDir, "toolchains")
}

func getSourcesDir() string {
	return filepath.Join(vmakeDir, "sources")
}
