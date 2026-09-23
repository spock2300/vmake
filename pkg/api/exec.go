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
		fatalScript("", "working directory", "%v", err)
	}
	if err := os.Chdir(dir); err != nil {
		fatalScript("", "working directory", "%v", err)
	}
	defer func() {
		if err := os.Chdir(origDir); err != nil {
			fatalScript("", "restore working directory", "%v", err)
		}
	}()
	fn()
}
