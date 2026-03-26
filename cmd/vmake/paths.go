package main

import (
	"path/filepath"
)

func getReposDir() string {
	return filepath.Join(vmakeDir, "repos")
}

func getPackagesDir() string {
	return filepath.Join(vmakeDir, "packages")
}

func getCacheDir() string {
	return filepath.Join(vmakeDir, "cache")
}

func getExtensionsDir() string {
	return filepath.Join(vmakeDir, "extensions")
}

func getToolchainsDir() string {
	return filepath.Join(vmakeDir, "toolchains")
}
