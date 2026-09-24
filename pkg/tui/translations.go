package tui

var translations = map[language]map[textID]string{
	languageEnglish: {
		textDetailScrollHint:   "↑↓/Wheel scroll",
		textVMakeConfiguration: "VMake Configuration", textSwitchToChinese: "中文", textSwitchToEnglish: "English", textLanguage: "Language", textModified: "● %d modified", textModifiedLabel: "modified", textPackages: "Packages", textFilter: "filter: %s (%d)", textNoMatches: "No matches",
		textOptions: "Options", textSelectPackage: "Select a package", textRunningMenuconfig: "Running menuconfig for %s...", textNoOptions: "No options", textGeneral: "General", textGlobal: "Global", textPreset: "preset", textRunMenuconfig: "run menuconfig",
		textUnsavedChanges: "Unsaved Changes", textSaveBeforeExit: "Save before exiting?", textSave: "Save", textDiscard: "Discard", textConfirmHint: "Tab: switch │ Enter: confirm │ Esc: cancel", textChoiceHint: "↑↓ navigate │ Enter select │ Esc cancel", textNoDetails: "No details available",
		textName: "name", textType: "type", textDefault: "default", textCurrent: "current", textYes: "yes", textNo: "no", textChoices: "choices: ", textCloseDetails: "Esc/? to close", textNavigate: "navigate", textCollapseExpand: "collapse/expand", textSearch: "search", textCollapseExpandAll: "collapse/expand all", textHideEmpty: "hide empty",
		textCycleValue: "cycle value", textEdit: "edit", textReset: "reset", textDetail: "detail", textBack: "back", textConfirm: "confirm", textDelete: "delete", textCancel: "cancel", textConfirmMatch: "confirm/1 match", textPickMatch: "pick match", textJump: "jump", textRefine: "refine", textClear: "clear", textMoveCursor: "move cursor",
		textTerminalTooSmall: "Terminal too small. Please resize to at least 50x8.", textToolchainErrors: "%d unavailable toolchain definitions; run vmake toolchain list for details", textSelectedToolchain: "Selected toolchain %q: %v",
	},
	languageChinese: {
		textDetailScrollHint:   "↑↓/滚轮滚动",
		textVMakeConfiguration: "VMake 配置", textSwitchToChinese: "中文", textSwitchToEnglish: "English", textLanguage: "语言", textModified: "● 已修改 %d 项", textModifiedLabel: "已修改", textPackages: "软件包", textFilter: "筛选：%s（%d）", textNoMatches: "没有匹配项",
		textOptions: "选项", textSelectPackage: "请选择软件包", textRunningMenuconfig: "正在为 %s 运行 menuconfig...", textNoOptions: "没有可用选项", textGeneral: "常规", textGlobal: "全局", textPreset: "预设", textRunMenuconfig: "运行 menuconfig",
		textUnsavedChanges: "有未保存的修改", textSaveBeforeExit: "退出前保存吗？", textSave: "保存", textDiscard: "放弃", textConfirmHint: "Tab：切换 │ Enter：确认 │ Esc：取消", textChoiceHint: "↑↓ 导航 │ Enter 选择 │ Esc 取消", textNoDetails: "没有可用详情",
		textName: "名称", textType: "类型", textDefault: "默认值", textCurrent: "当前值", textYes: "是", textNo: "否", textChoices: "可选值：", textCloseDetails: "Esc/? 关闭", textNavigate: "导航", textCollapseExpand: "折叠/展开", textSearch: "搜索", textCollapseExpandAll: "全部折叠/展开", textHideEmpty: "隐藏空项",
		textCycleValue: "切换值", textEdit: "编辑", textReset: "重置", textDetail: "详情", textBack: "返回", textConfirm: "确认", textDelete: "删除", textCancel: "取消", textConfirmMatch: "确认/单个匹配", textPickMatch: "选择匹配", textJump: "跳转", textRefine: "细化", textClear: "清除", textMoveCursor: "移动光标",
		textTerminalTooSmall: "终端窗口太小，请至少调整到 50x8。", textToolchainErrors: "%d 个工具链定义不可用；运行 vmake toolchain list 查看详情", textSelectedToolchain: "当前工具链 %q：%v",
	},
}
