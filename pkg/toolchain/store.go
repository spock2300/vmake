package toolchain

import "runtime"

func GetBuiltinHost() *Toolchain {
	targetOS := runtime.GOOS
	return &Toolchain{
		Name:        "host",
		DisplayName: "Host",
		TargetOS:    targetOS,
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
			MAKE:    "make",
		},
		DefaultFlags: defaultFlagsFor(targetOS),
	}
}
