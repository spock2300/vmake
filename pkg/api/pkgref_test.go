package api

import "testing"

func TestCanonicalizePackageRef(t *testing.T) {
	tests := []struct {
		container string
		ref       string
		want      string
	}{
		{"//", "//", "//"},
		{"//", "//firmware", "//firmware"},
		{"//", "//board/evb", "//board/evb"},
		{"//", "hl/hl-mcu-sdk/system", "hl/hl-mcu-sdk/system"},
		{"hl/hl-mcu-sdk", "//", "hl/hl-mcu-sdk"},
		{"hl/hl-mcu-sdk", "//system/uniface", "hl/hl-mcu-sdk/system/uniface"},
		{"hl/hl-mcu-sdk", "official/tinyexpr", "official/tinyexpr"},
		{"official", "//zlib", "official/zlib"},
		{"", "//app", "//app"},
		{"//", "", ""},
	}
	for _, tt := range tests {
		if got := CanonicalizePackageRef(tt.container, tt.ref); got != tt.want {
			t.Errorf("CanonicalizePackageRef(%q, %q) = %q, want %q", tt.container, tt.ref, got, tt.want)
		}
	}
}

func TestValidatePackageRef(t *testing.T) {
	valid := []string{
		"//",
		"//app",
		"//board/evb",
		"official/tinyexpr",
		"hl/hl-mcu-sdk",
		"hl/hl-mcu-sdk/system/freertos/lvgl",
	}
	for _, ref := range valid {
		if err := ValidatePackageRef(ref); err != nil {
			t.Errorf("ValidatePackageRef(%q) unexpected error: %v", ref, err)
		}
	}

	invalid := []string{
		"",
		"bare",
		"/leading",
		"//trailing/",
		"//a//b",
		"//../escape",
		"//./dot",
		"official/tinyexpr >=1.0.0",
		"official/tinyexpr=1.0.0",
		"hl/hl-mcu-sdk/system:target",
		"repo/",
		"repo//pkg",
	}
	for _, ref := range invalid {
		if err := ValidatePackageRef(ref); err == nil {
			t.Errorf("ValidatePackageRef(%q) = nil, want error", ref)
		}
	}
}

func TestSplitPackageRefAndLocalRelPath(t *testing.T) {
	if repo, name, ok := SplitPackageRef("//firmware"); ok || repo != "" || name != "//firmware" {
		t.Fatalf("SplitPackageRef(//firmware) = (%q, %q, %v)", repo, name, ok)
	}
	if repo, name, ok := SplitPackageRef("hl/hl-mcu-sdk/system"); !ok || repo != "hl" || name != "hl-mcu-sdk/system" {
		t.Fatalf("SplitPackageRef(hl/hl-mcu-sdk/system) = (%q, %q, %v)", repo, name, ok)
	}
	if rel, ok := LocalRelPath("//"); !ok || rel != "" {
		t.Fatalf("LocalRelPath(//) = (%q, %v)", rel, ok)
	}
	if rel, ok := LocalRelPath("//board/evb"); !ok || rel != "board/evb" {
		t.Fatalf("LocalRelPath(//board/evb) = (%q, %v)", rel, ok)
	}
	if _, ok := LocalRelPath("hl/hl-mcu-sdk"); ok {
		t.Fatal("LocalRelPath(hl/hl-mcu-sdk) should be false")
	}
}

func TestParseDepRef(t *testing.T) {
	valid := []struct {
		raw       string
		pkg       string
		target    string
		qualified bool
		wildcard  bool
	}{
		{"helper", "helper", "", false, false},
		{"//", "//", "", false, false},
		{"//board", "//board", "", false, false},
		{"//:app", "//", "app", true, false},
		{"//board:lib", "//board", "lib", true, false},
		{"//board:*", "//board", "*", true, true},
		{"hl/hl-mcu-sdk/system", "hl/hl-mcu-sdk/system", "", false, false},
		{"hl/hl-mcu-sdk/system:sys", "hl/hl-mcu-sdk/system", "sys", true, false},
		{"hl/hl-mcu-sdk/system:*", "hl/hl-mcu-sdk/system", "*", true, true},
	}
	for _, tt := range valid {
		got, err := ParseDepRef(tt.raw)
		if err != nil {
			t.Errorf("ParseDepRef(%q) error: %v", tt.raw, err)
			continue
		}
		if got.Pkg != tt.pkg || got.Target != tt.target || got.Qualified != tt.qualified || got.Wildcard != tt.wildcard {
			t.Errorf("ParseDepRef(%q) = %+v, want %+v", tt.raw, got, tt)
		}
	}

	invalid := []string{"", "a b", ":target", "//:", "bare:target", "a:b:c", "//a//b", "/bad"}
	for _, raw := range invalid {
		if _, err := ParseDepRef(raw); err == nil {
			t.Errorf("ParseDepRef(%q) = nil error, want error", raw)
		}
	}
}
