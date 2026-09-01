package api

import (
	"os"

	vlog "github.com/spock2300/vmake/pkg/log"
)

func execInDir(dir string, fn func()) {
	if dir == "" {
		fn()
		return
	}
	origDir, err := os.Getwd()
	if err != nil {
		vlog.Error("get working directory for %s: %v (callbacks run in current dir)", dir, err)
		fn()
		return
	}
	if err := os.Chdir(dir); err != nil {
		vlog.Error("chdir to %s: %v (callbacks run in current dir)", dir, err)
		fn()
		return
	}
	defer os.Chdir(origDir)
	fn()
}
