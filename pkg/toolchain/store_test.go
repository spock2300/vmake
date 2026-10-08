package toolchain

import "testing"

func TestBuiltinHostPerPlatform(t *testing.T) {
	darwin := builtinHostFor("darwin")
	if darwin.Tools.CC != "cc" || darwin.Tools.CXX != "c++" {
		t.Fatalf("darwin host compilers = %q/%q, want cc/c++", darwin.Tools.CC, darwin.Tools.CXX)
	}
	if darwin.Tools.OBJCOPY != "" || darwin.Tools.OBJDUMP != "" {
		t.Fatalf("darwin host must not require objcopy/objdump, got %q/%q", darwin.Tools.OBJCOPY, darwin.Tools.OBJDUMP)
	}
	for name, got := range map[string]string{
		"ar": darwin.Tools.AR, "ld": darwin.Tools.LD, "strip": darwin.Tools.STRIP,
		"ranlib": darwin.Tools.RANLIB, "nm": darwin.Tools.NM, "size": darwin.Tools.SIZE,
	} {
		if got == "" {
			t.Errorf("darwin host %s is not configured", name)
		}
	}

	linux := builtinHostFor("linux")
	if linux.Tools.CC != "gcc" || linux.Tools.CXX != "g++" || linux.Tools.OBJCOPY != "objcopy" {
		t.Fatalf("linux host changed: CC=%q CXX=%q OBJCOPY=%q", linux.Tools.CC, linux.Tools.CXX, linux.Tools.OBJCOPY)
	}

	windows := builtinHostFor("windows")
	if windows.Tools.CC != "gcc" || windows.Tools.OBJDUMP != "objdump" {
		t.Fatalf("windows host changed: CC=%q OBJDUMP=%q", windows.Tools.CC, windows.Tools.OBJDUMP)
	}
}
