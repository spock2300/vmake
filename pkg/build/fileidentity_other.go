//go:build !linux && !darwin && !dragonfly && !freebsd && !netbsd && !openbsd

package build

import (
	"os"
	"time"
)

func fileChangeTime(string, os.FileInfo) (time.Time, bool) {
	return time.Time{}, false
}
