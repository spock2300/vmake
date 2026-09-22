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
	}
}
