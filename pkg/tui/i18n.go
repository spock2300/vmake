package tui

import "fmt"

type textID string

const (
	textVMakeConfiguration textID = "vmake_configuration"
	textSwitchToChinese    textID = "switch_to_chinese"
	textSwitchToEnglish    textID = "switch_to_english"
	textLanguage           textID = "language"
	textModified           textID = "modified"
	textModifiedLabel      textID = "modified_label"
	textPackages           textID = "packages"
	textFilter             textID = "filter"
	textNoMatches          textID = "no_matches"
	textOptions            textID = "options"
	textSelectPackage      textID = "select_package"
	textRunningMenuconfig  textID = "running_menuconfig"
	textNoOptions          textID = "no_options"
	textGeneral            textID = "general"
	textGlobal             textID = "global"
	textPreset             textID = "preset"
	textRunMenuconfig      textID = "run_menuconfig"
	textUnsavedChanges     textID = "unsaved_changes"
	textSaveBeforeExit     textID = "save_before_exit"
	textSave               textID = "save"
	textDiscard            textID = "discard"
	textConfirmHint        textID = "confirm_hint"
	textChoiceHint         textID = "choice_hint"
	textNoDetails          textID = "no_details"
	textName               textID = "name"
	textType               textID = "type"
	textDefault            textID = "default"
	textCurrent            textID = "current"
	textYes                textID = "yes"
	textNo                 textID = "no"
	textChoices            textID = "choices"
	textCloseDetails       textID = "close_details"
	textNavigate           textID = "navigate"
	textCollapseExpand     textID = "collapse_expand"
	textSearch             textID = "search"
	textCollapseExpandAll  textID = "collapse_expand_all"
	textHideEmpty          textID = "hide_empty"
	textCycleValue         textID = "cycle_value"
	textEdit               textID = "edit"
	textReset              textID = "reset"
	textDetail             textID = "detail"
	textBack               textID = "back"
	textConfirm            textID = "confirm"
	textDelete             textID = "delete"
	textCancel             textID = "cancel"
	textConfirmMatch       textID = "confirm_match"
	textPickMatch          textID = "pick_match"
	textJump               textID = "jump"
	textRefine             textID = "refine"
	textClear              textID = "clear"
	textMoveCursor         textID = "move_cursor"
	textTerminalTooSmall   textID = "terminal_too_small"
	textToolchainErrors    textID = "toolchain_errors"
	textSelectedToolchain  textID = "selected_toolchain"
)

func (m *Model) text(id textID) string {
	if table := translations[m.language]; table != nil {
		if value, ok := table[id]; ok {
			return value
		}
	}
	return translations[languageEnglish][id]
}

func (m *Model) textf(id textID, args ...any) string {
	return fmt.Sprintf(m.text(id), args...)
}

func (m *Model) toggleLanguage() {
	if m.language == languageChinese {
		m.language = languageEnglish
	} else {
		m.language = languageChinese
	}
}

func (m *Model) languageLabel() string {
	if m.language == languageChinese {
		return m.text(textSwitchToChinese)
	}
	return m.text(textSwitchToEnglish)
}

func (m *Model) packageLabel(name string) string {
	if name == GlobalPkgName {
		return m.text(textGlobal)
	}
	return name
}

func (m *Model) openLanguageSelector() {
	m.overlay = overlayLanguage
	m.choiceOpt = ""
	m.choiceValues = []string{m.text(textSwitchToEnglish), m.text(textSwitchToChinese)}
	if m.language == languageChinese {
		m.choiceCursor = 1
	} else {
		m.choiceCursor = 0
	}
}
