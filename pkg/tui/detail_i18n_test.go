package tui

import (
	"strings"
	"testing"
)

func TestDetailOverlayModifiedStatusTranslations(t *testing.T) {
	for _, tt := range []struct {
		name     string
		language language
		modified bool
		want     string
	}{
		{"English unchanged", languageEnglish, false, "modified : no"},
		{"English modified", languageEnglish, true, "modified : yes"},
		{"Chinese unchanged", languageChinese, false, "已修改 : 否"},
		{"Chinese modified", languageChinese, true, "已修改 : 是"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := newLanguageTestModel()
			m.language = tt.language
			m.setValue("enabled", tt.modified)
			m.openDetailOverlay(OptionItem{Name: "enabled"})

			view := stripAnsi(m.renderDetailOverlay())
			if strings.Contains(view, "%d") {
				t.Fatalf("detail overlay contains an unformatted count template:\n%s", view)
			}
			if !strings.Contains(strings.Join(strings.Fields(view), " "), tt.want) {
				t.Fatalf("detail overlay missing modification status %q:\n%s", tt.want, view)
			}
		})
	}
}
