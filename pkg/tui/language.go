package tui

import (
	"strings"
	"unicode"
)

type language uint8

const (
	languageEnglish language = iota
	languageChinese
)

func parseLocale(locale string) language {
	locale = strings.TrimSpace(strings.ToLower(locale))
	if locale == "" {
		return languageEnglish
	}
	for i, r := range locale {
		if r == '_' || r == '-' || r == '.' || r == '@' || unicode.IsSpace(r) {
			locale = locale[:i]
			break
		}
	}
	if locale == "zh" {
		return languageChinese
	}
	return languageEnglish
}

func preferredLanguage(locales []string) language {
	for _, locale := range locales {
		if strings.TrimSpace(locale) != "" {
			return parseLocale(locale)
		}
	}
	return languageEnglish
}

func languageFromEnv(getenv func(string) string) language {
	for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if locale := strings.TrimSpace(getenv(name)); locale != "" {
			return parseLocale(locale)
		}
	}
	return languageEnglish
}

func systemLanguage() language {
	return defaultLanguage()
}
