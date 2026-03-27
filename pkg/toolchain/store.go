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
			CFlags:   []string{"-O2", "-Wall", "-Wstrict-prototypes", "-fno-strict-aliasing", "-fno-common", "-fPIC"},
			CxxFlags: []string{"-O2", "-Wall", "-Wextra", "-fno-strict-aliasing", "-fno-common", "-fPIC"},
			LdFlags:  []string{"-Wl,--as-needed"},
		},
	}
}
