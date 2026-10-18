//go:build linux

package build

import (
	"os"
	"syscall"
	"time"
)

func fileChangeTime(_ string, info os.FileInfo) (time.Time, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return time.Time{}, false
	}
	return time.Unix(stat.Ctim.Sec, stat.Ctim.Nsec), true
}
