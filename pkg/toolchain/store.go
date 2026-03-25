package toolchain

func GetBuiltinGCC() *Toolchain {
	return &Toolchain{
		Name:        "gcc",
		DisplayName: "System GCC",
		Tools: Tools{
			CC:     "gcc",
			CXX:    "g++",
			AR:     "ar",
			LD:     "ld",
			STRIP:  "strip",
			RANLIB: "ranlib",
		},
		DefaultFlags: DefaultFlags{
			CFlags:   []string{"-O2", "-Wall", "-Wstrict-prototypes", "-fno-strict-aliasing", "-fno-common", "-fPIC"},
			CxxFlags: []string{"-O2", "-Wall", "-Wextra", "-fno-strict-aliasing", "-fno-common", "-fPIC"},
			LdFlags:  []string{"-Wl,--as-needed"},
		},
	}
}
