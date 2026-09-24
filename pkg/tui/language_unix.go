//go:build !windows

package tui

import "os"

func defaultLanguage() language {
	return languageFromEnv(os.Getenv)
}
