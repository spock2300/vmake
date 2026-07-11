package toolchain

import "reflect"

func YaegiSymbols() map[string]reflect.Value {
	return map[string]reflect.Value{
		"Toolchain":          reflect.ValueOf((*Toolchain)(nil)),
		"ToolchainDef":       reflect.ValueOf((*ToolchainDef)(nil)),
		"Tools":              reflect.ValueOf((*Tools)(nil)),
		"DefaultFlags":       reflect.ValueOf((*DefaultFlags)(nil)),
		"InstallConfig":      reflect.ValueOf((*InstallConfig)(nil)),
		"Manager":            reflect.ValueOf((*Manager)(nil)),
		"OnMissingToolchain": reflect.ValueOf((*OnMissingToolchain)(nil)),

		"GetManager":         reflect.ValueOf(GetManager),
		"GetBuiltinHost":     reflect.ValueOf(GetBuiltinHost),
		"DetectFormat":       reflect.ValueOf(DetectFormat),
		"ResolveToolPath":    reflect.ValueOf(ResolveToolPath),
		"ValidateToolchain":  reflect.ValueOf(ValidateToolchain),
		"LoadToolchainDef":   reflect.ValueOf(LoadToolchainDef),
		"ScanRepoToolchains": reflect.ValueOf(ScanRepoToolchains),
	}
}
