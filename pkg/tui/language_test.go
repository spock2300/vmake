package tui

import "testing"

func TestParseLocale(t *testing.T) {
	tests := []struct {
		locale string
		want   language
	}{
		{"zh_CN.UTF-8", languageChinese},
		{"zh-TW", languageChinese},
		{"ZH", languageChinese},
		{"en_US.UTF-8", languageEnglish},
		{"C", languageEnglish},
		{"POSIX", languageEnglish},
		{"", languageEnglish},
	}
	for _, test := range tests {
		if got := parseLocale(test.locale); got != test.want {
			t.Errorf("parseLocale(%q) = %d, want %d", test.locale, got, test.want)
		}
	}
}

func TestPreferredLanguage(t *testing.T) {
	if got := preferredLanguage([]string{"", "zh-CN", "en-US"}); got != languageChinese {
		t.Errorf("preferredLanguage should select first non-empty locale, got %d", got)
	}
	if got := preferredLanguage([]string{"", "en-US", "zh-CN"}); got != languageEnglish {
		t.Errorf("preferredLanguage should preserve order, got %d", got)
	}
	if got := preferredLanguage(nil); got != languageEnglish {
		t.Errorf("preferredLanguage(nil) = %d, want English", got)
	}
}

func TestLanguageFromEnvPriority(t *testing.T) {
	values := map[string]string{"LC_ALL": "en_US.UTF-8", "LC_MESSAGES": "zh_CN.UTF-8", "LANG": "zh_TW.UTF-8"}
	getenv := func(name string) string { return values[name] }
	if got := languageFromEnv(getenv); got != languageEnglish {
		t.Errorf("LC_ALL should have highest priority, got %d", got)
	}
	values["LC_ALL"] = ""
	if got := languageFromEnv(getenv); got != languageChinese {
		t.Errorf("LC_MESSAGES should be selected when LC_ALL is empty, got %d", got)
	}
	values["LC_MESSAGES"] = ""
	if got := languageFromEnv(getenv); got != languageChinese {
		t.Errorf("LANG should be selected when higher priority values are empty, got %d", got)
	}
}

func TestTranslationTablesAreComplete(t *testing.T) {
	english := translations[languageEnglish]
	chinese := translations[languageChinese]
	for id, value := range english {
		if value == "" {
			t.Errorf("English translation %q is empty", id)
		}
		if chinese[id] == "" {
			t.Errorf("Chinese translation %q is missing", id)
		}
	}
}
