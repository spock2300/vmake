package api

import (
	"os"
)

func execInDir(dir string, fn func()) {
	if dir == "" {
		fn()
		return
	}
	origDir, err := os.Getwd()
	if err != nil {
		fn()
		return
	}
	if err := os.Chdir(dir); err != nil {
		fn()
		return
	}
	defer os.Chdir(origDir)
	fn()
}
