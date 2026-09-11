package toolchain

// defaultFlagsFor returns the builtin host toolchain's default flags for a
// target OS. ELF-only linker flags and the glibc-only _FORTIFY_SOURCE are
// omitted for PE targets, where MinGW's linker rejects them.
func defaultFlagsFor(targetOS string) DefaultFlags {
	return DefaultFlags{
		CFlags:   cFlagsFor(targetOS),
		CxxFlags: cxxFlagsFor(targetOS),
		LdFlags:  ldFlagsFor(targetOS),
	}
}

func cFlagsFor(targetOS string) []string {
	flags := []string{
		"-Wall", "-Wextra", "-Werror",
		"-Wstrict-prototypes", "-Wmissing-prototypes", "-Wmissing-declarations",
		"-Wold-style-definition", "-Wundef", "-Werror-implicit-function-declaration",
		"-Wformat=2", "-Wshadow",
		"-ffunction-sections", "-fdata-sections",
		"-fstack-protector-strong",
	}
	if targetOS != "windows" {
		flags = append(flags, "-D_FORTIFY_SOURCE=2")
	}
	return append(flags,
		"-fno-strict-aliasing", "-fno-common", "-fPIC",
	)
}

func cxxFlagsFor(targetOS string) []string {
	flags := []string{
		"-Wall", "-Wextra", "-Werror",
		"-Wnon-virtual-dtor", "-Woverloaded-virtual", "-Wundef",
		"-Wformat=2", "-Wshadow",
		"-ffunction-sections", "-fdata-sections",
		"-fstack-protector-strong",
	}
	if targetOS != "windows" {
		flags = append(flags, "-D_FORTIFY_SOURCE=2")
	}
	return append(flags,
		"-fno-strict-aliasing", "-fno-common", "-fPIC",
	)
}

func ldFlagsFor(targetOS string) []string {
	if targetOS == "windows" {
		// -pie, --as-needed and -z relro/now are ELF-only.
		return []string{"-Wl,--gc-sections"}
	}
	return []string{"-pie", "-Wl,--as-needed", "-Wl,--gc-sections", "-Wl,-z,relro,-z,now"}
}
