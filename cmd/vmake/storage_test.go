package main

import (
	"testing"
)

func TestRequiresExclusiveStorage(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{nil, false},
		{[]string{"build"}, false},
		{[]string{"test"}, false},
		{[]string{"query", "targets"}, false},
		{[]string{"manifest", "checkout", "manifest.json"}, false},
		{[]string{"clean"}, true},
		{[]string{"distclean"}, true},
		{[]string{"rebuild"}, true},
		{[]string{"deps", "status"}, false},
		{[]string{"deps", "sync"}, true},
		{[]string{"deps", "update"}, true},
		{[]string{"deps", "pin", "official/zlib@1.0.0"}, true},
		{[]string{"deps", "unpin", "official/zlib"}, true},
		{[]string{"deps", "fetch"}, true},
		{[]string{"pkg", "clean", "official/zlib"}, true},
	}
	for _, tc := range cases {
		cmd, _, err := RootCmd.Find(tc.args)
		if err != nil {
			t.Fatalf("find %v: %v", tc.args, err)
		}
		if got := requiresExclusiveStorage(cmd); got != tc.want {
			t.Errorf("requiresExclusiveStorage(%v) = %v, want %v", tc.args, got, tc.want)
		}
	}
}
