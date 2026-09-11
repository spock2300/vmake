package toolchain

import (
	"slices"
	"testing"
)

func TestDefaultFlagsLinuxUnchanged(t *testing.T) {
	flags := defaultFlagsFor("linux")

	wantC := []string{
		"-Wall", "-Wextra", "-Werror",
		"-Wstrict-prototypes", "-Wmissing-prototypes", "-Wmissing-declarations",
		"-Wold-style-definition", "-Wundef", "-Werror-implicit-function-declaration",
		"-Wformat=2", "-Wshadow",
		"-ffunction-sections", "-fdata-sections",
		"-fstack-protector-strong", "-D_FORTIFY_SOURCE=2",
		"-fno-strict-aliasing", "-fno-common", "-fPIC",
	}
	if !slices.Equal(flags.CFlags, wantC) {
		t.Errorf("CFlags = %v\nwant %v", flags.CFlags, wantC)
	}

	wantLd := []string{"-pie", "-Wl,--as-needed", "-Wl,--gc-sections", "-Wl,-z,relro,-z,now"}
	if !slices.Equal(flags.LdFlags, wantLd) {
		t.Errorf("LdFlags = %v\nwant %v", flags.LdFlags, wantLd)
	}
}

func TestDefaultFlagsWindowsDropsELFFlags(t *testing.T) {
	flags := defaultFlagsFor("windows")

	if slices.Contains(flags.CFlags, "-D_FORTIFY_SOURCE=2") {
		t.Error("CFlags must not carry the glibc-only -D_FORTIFY_SOURCE=2 for PE targets")
	}
	if slices.Contains(flags.CxxFlags, "-D_FORTIFY_SOURCE=2") {
		t.Error("CxxFlags must not carry the glibc-only -D_FORTIFY_SOURCE=2 for PE targets")
	}

	for _, elfOnly := range []string{"-pie", "-Wl,--as-needed", "-Wl,-z,relro,-z,now"} {
		if slices.Contains(flags.LdFlags, elfOnly) {
			t.Errorf("LdFlags must not contain the ELF-only %s for PE targets", elfOnly)
		}
	}
	if !slices.Contains(flags.LdFlags, "-Wl,--gc-sections") {
		t.Error("LdFlags should keep -Wl,--gc-sections, which MinGW's linker supports")
	}
}

func TestTargetOSOf(t *testing.T) {
	if got := TargetOSOf(nil); got == "" {
		t.Error("TargetOSOf(nil) must fall back to the host OS")
	}
	if got := TargetOSOf(&Toolchain{TargetOS: "windows"}); got != "windows" {
		t.Errorf("TargetOSOf = %q, want windows", got)
	}
}

func TestMakeTool(t *testing.T) {
	if got := (&Toolchain{}).MakeTool(); got != "make" {
		t.Errorf("MakeTool() = %q, want make", got)
	}
	if got := (&Toolchain{Tools: Tools{MAKE: "mingw32-make"}}).MakeTool(); got != "mingw32-make" {
		t.Errorf("MakeTool() = %q, want mingw32-make", got)
	}
}
