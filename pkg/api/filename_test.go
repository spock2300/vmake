package api

import "testing"

func TestTargetFilenamePerOS(t *testing.T) {
	cases := []struct {
		kind     TargetKind
		name     string
		targetOS string
		want     string
	}{
		{TargetBinary, "app", "linux", "app"},
		{TargetBinary, "app", "windows", "app.exe"},
		{TargetStatic, "foo", "linux", "libfoo.a"},
		{TargetStatic, "foo", "windows", "libfoo.a"},
		{TargetShared, "foo", "linux", "libfoo.so"},
		{TargetShared, "foo", "windows", "libfoo.dll"},
		{TargetObject, "foo", "windows", "foo.o"},
	}

	for _, tc := range cases {
		if got := TargetFilename(tc.kind, tc.name, tc.targetOS); got != tc.want {
			t.Errorf("TargetFilename(%s, %s, %s) = %q, want %q", tc.kind, tc.name, tc.targetOS, got, tc.want)
		}
	}
}
