package plugin

import "reflect"

func YaegiSymbols() map[string]reflect.Value {
	return map[string]reflect.Value{
		"Context":      reflect.ValueOf((*Context)(nil)),
		"Info":         reflect.ValueOf((*Info)(nil)),
		"MainFunc":     reflect.ValueOf((*MainFunc)(nil)),
		"RunGitLFS":    reflect.ValueOf(RunGitLFS),
		"DownloadFile": reflect.ValueOf(DownloadFile),
		"ExtractToDir": reflect.ValueOf(ExtractToDir),
	}
}
