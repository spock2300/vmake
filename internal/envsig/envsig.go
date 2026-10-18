// Package envsig defines the environment variables that can affect compiler,
// linker or CMake output. Action records use the full list; the CMake configure
// fingerprint uses the same list minus variables vmake passes explicitly.
package envsig

import (
	"os"
	"strings"
)

var keys = map[string]bool{
	"PATH": true, "CC": true, "CXX": true, "AR": true,
	"CFLAGS": true, "CXXFLAGS": true, "CPPFLAGS": true, "LDFLAGS": true,
	"CPATH": true, "C_INCLUDE_PATH": true, "CPLUS_INCLUDE_PATH": true, "OBJC_INCLUDE_PATH": true,
	"LIBRARY_PATH": true, "COMPILER_PATH": true, "GCC_EXEC_PREFIX": true,
	"SDKROOT": true, "MACOSX_DEPLOYMENT_TARGET": true, "MACOSX_SDK_VERSION": true,
	"SOURCE_DATE_EPOCH": true, "ZERO_AR_DATE": true,
	"INCLUDE": true, "LIB": true, "LIBPATH": true, "VCTOOLSINSTALLDIR": true,
	"PKG_CONFIG_PATH": true, "PKG_CONFIG_LIBDIR": true, "VCPKG_ROOT": true,
	"LD_RUN_PATH": true,
}

// Allowed reports whether key affects build output. VMAKE_ENV_EXTRA is an
// additive, comma-separated escape hatch for unusual toolchains.
func Allowed(key string) bool {
	if keys[strings.ToUpper(key)] {
		return true
	}
	return Extra(key)
}

// Extra reports whether key was added through VMAKE_ENV_EXTRA.
func Extra(key string) bool {
	for _, extra := range strings.Split(os.Getenv("VMAKE_ENV_EXTRA"), ",") {
		if name := strings.TrimSpace(extra); name != "" && strings.EqualFold(name, key) {
			return true
		}
	}
	return false
}
