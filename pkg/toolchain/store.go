package toolchain

func GetBuiltinHost() *Toolchain {
	return &Toolchain{
		Name:        "host",
		DisplayName: "Host",
		Tools: Tools{
			CC:      "gcc",
			CXX:     "g++",
			AR:      "ar",
			LD:      "ld",
			STRIP:   "strip",
			RANLIB:  "ranlib",
			OBJCOPY: "objcopy",
			SIZE:    "size",
			OBJDUMP: "objdump",
			NM:      "nm",
		},
		DefaultFlags: DefaultFlags{
			CFlags: []string{
				"-Wall", "-Wextra", "-Werror",
				"-Wstrict-prototypes", "-Wmissing-prototypes", "-Wmissing-declarations",
				"-Wold-style-definition", "-Wundef", "-Werror-implicit-function-declaration",
				"-Wformat=2", "-Wshadow",
				"-ffunction-sections", "-fdata-sections",
				"-fstack-protector-strong", "-D_FORTIFY_SOURCE=2",
				"-fno-strict-aliasing", "-fno-common", "-fPIC",
			},
			CxxFlags: []string{
				"-Wall", "-Wextra", "-Werror",
				"-Wnon-virtual-dtor", "-Woverloaded-virtual", "-Wundef",
				"-Wformat=2", "-Wshadow",
				"-ffunction-sections", "-fdata-sections",
				"-fstack-protector-strong", "-D_FORTIFY_SOURCE=2",
				"-fno-strict-aliasing", "-fno-common", "-fPIC",
			},
			LdFlags: []string{
				"-pie", "-Wl,--as-needed", "-Wl,--gc-sections", "-Wl,-z,relro,-z,now",
			},
		},
	}
}
