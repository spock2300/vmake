package api

import (
	"os"
)

func execInDir(dir string, fn func()) {
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	if dir != "" {
		os.Chdir(dir)
	}
	fn()
}
