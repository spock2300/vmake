package version

import (
	"runtime/debug"
	"strings"
)

var (
	Version   string
	GitCommit string
	BuildDate string
)

func init() {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		Version = "dev"
		GitCommit = "unknown"
		BuildDate = "unknown"
		return
	}

	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		Version = info.Main.Version
	} else {
		Version = "dev"
	}

	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			if len(s.Value) >= 8 {
				GitCommit = s.Value[:8]
			} else {
				GitCommit = s.Value
			}
		case "vcs.time":
			BuildDate = strings.Replace(s.Value, "T", " ", 1)
			BuildDate = strings.TrimSuffix(BuildDate, "Z")
		}
	}

	if GitCommit == "" {
		GitCommit = "unknown"
	}
	if BuildDate == "" {
		BuildDate = "unknown"
	}
}
