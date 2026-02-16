package api

type TargetKind string

const (
	TargetBinary TargetKind = "binary"
	TargetStatic TargetKind = "static"
	TargetShared TargetKind = "shared"
	TargetObject TargetKind = "object"
)

type OptionType int

const (
	OptionBool OptionType = iota
	OptionString
	OptionInt
	OptionChoice
)

func (t OptionType) String() string {
	switch t {
	case OptionBool:
		return "bool"
	case OptionString:
		return "string"
	case OptionInt:
		return "int"
	case OptionChoice:
		return "choice"
	default:
		return "unknown"
	}
}

type ConfigFunc func(ctx *ConfigContext)
type BuildFunc func(ctx *BuildContext)
type InstallFunc func(ctx *InstallContext)

type Builder struct {
	configFuncs  []ConfigFunc
	buildFuncs   []BuildFunc
	installFuncs []InstallFunc
	requireFuncs []RequireFunc
}

func (b *Builder) OnRequire(fn RequireFunc) {
	b.requireFuncs = append(b.requireFuncs, fn)
}

func (b *Builder) GetRequireFuncs() []RequireFunc {
	return b.requireFuncs
}

func (b *Builder) OnConfig(fn ConfigFunc) {
	b.configFuncs = append(b.configFuncs, fn)
}

func (b *Builder) OnBuild(fn BuildFunc) {
	b.buildFuncs = append(b.buildFuncs, fn)
}

func (b *Builder) GetConfigFuncs() []ConfigFunc {
	return b.configFuncs
}

func (b *Builder) GetBuildFuncs() []BuildFunc {
	return b.buildFuncs
}

func (b *Builder) OnInstall(fn InstallFunc) {
	b.installFuncs = append(b.installFuncs, fn)
}

func (b *Builder) GetInstallFuncs() []InstallFunc {
	return b.installFuncs
}
