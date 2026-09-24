//go:build windows

package tui

import "golang.org/x/sys/windows"

func defaultLanguage() language {
	languages, err := windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_NAME)
	if err != nil {
		return languageEnglish
	}
	return preferredLanguage(languages)
}
