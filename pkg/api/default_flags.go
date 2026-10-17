package api

type DefaultFlags struct {
	CFlags   []string
	CxxFlags []string
	LdFlags  []string
}

func DefaultBuildFlags(toolchainName, targetOS string) DefaultFlags {
	if toolchainName != "host" {
		return DefaultFlags{}
	}
	return DefaultFlags{
		CFlags:   cFlagsFor(targetOS),
		CxxFlags: cxxFlagsFor(targetOS),
		LdFlags:  ldFlagsFor(targetOS),
	}
}

func baseFlags(targetOS string, languageFlags ...string) []string {
	flags := []string{"-Wall", "-Wextra", "-Werror"}
	flags = append(flags, languageFlags...)
	flags = append(flags,
		"-Wformat=2", "-Wshadow",
		"-ffunction-sections", "-fdata-sections",
		"-fstack-protector-strong",
	)
	if targetOS != "windows" && targetOS != "darwin" {
		flags = append(flags, "-D_FORTIFY_SOURCE=2")
	}
	return append(flags,
		"-fno-strict-aliasing", "-fno-common", "-fPIC",
	)
}

func cFlagsFor(targetOS string) []string {
	return baseFlags(targetOS,
		"-Wstrict-prototypes", "-Wmissing-prototypes", "-Wmissing-declarations",
		"-Wold-style-definition", "-Wundef", "-Werror-implicit-function-declaration",
	)
}

func cxxFlagsFor(targetOS string) []string {
	return baseFlags(targetOS,
		"-Wnon-virtual-dtor", "-Woverloaded-virtual", "-Wundef",
	)
}

func ldFlagsFor(targetOS string) []string {
	switch targetOS {
	case "windows":
		return []string{"-Wl,--gc-sections"}
	case "darwin":
		return []string{"-Wl,-dead_strip"}
	default:
		return []string{"-pie", "-Wl,--as-needed", "-Wl,--gc-sections", "-Wl,-z,relro,-z,now"}
	}
}
