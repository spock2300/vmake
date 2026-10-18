package build

import (
	"testing"
)

func TestActionEnvironmentIgnoresUnrelatedVariables(t *testing.T) {
	base, err := environmentSignature(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"TERM", "MAKEFLAGS", "PWD", "VMAKE_CACHE", "VMAKE_TRUST_ALL", "CI_JOB_ID"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "changed-by-test")
			got, err := environmentSignature(nil)
			if err != nil {
				t.Fatal(err)
			}
			if got != base {
				t.Fatalf("%s changed the action environment fingerprint", key)
			}
		})
	}
}

func TestActionEnvironmentTracksCompilerInputs(t *testing.T) {
	base, err := environmentSignature(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"CPATH", "CFLAGS", "CXXFLAGS", "LDFLAGS", "SDKROOT", "SOURCE_DATE_EPOCH"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "changed-by-test")
			got, err := environmentSignature(nil)
			if err != nil {
				t.Fatal(err)
			}
			if got == base {
				t.Fatalf("%s did not change the action environment fingerprint", key)
			}
		})
	}
}

func TestActionEnvironmentExtraEscapeHatch(t *testing.T) {
	t.Setenv("MY_TOOLCHAIN_VAR", "one")
	base, err := environmentSignature(nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MY_TOOLCHAIN_VAR", "two")
	same, err := environmentSignature(nil)
	if err != nil {
		t.Fatal(err)
	}
	if same != base {
		t.Fatal("unlisted variable changed the fingerprint")
	}
	t.Setenv("VMAKE_ENV_EXTRA", "MY_TOOLCHAIN_VAR")
	extra, err := environmentSignature(nil)
	if err != nil {
		t.Fatal(err)
	}
	if extra == same {
		t.Fatal("VMAKE_ENV_EXTRA did not include the variable")
	}
}
