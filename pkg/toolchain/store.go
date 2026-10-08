package toolchain

import "runtime"

func GetBuiltinHost() *Toolchain {
	return builtinHostFor(runtime.GOOS)
}

func builtinHostFor(goos string) *Toolchain {
	tc := &Toolchain{
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
	if goos == "darwin" {
		tc.Tools.CC = "cc"
		tc.Tools.CXX = "c++"
		tc.Tools.OBJCOPY = ""
		tc.Tools.OBJDUMP = ""
	}
	return tc
}
