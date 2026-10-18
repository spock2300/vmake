package envsig

import "testing"

func TestAllowedIncludesLinkerEnvironment(t *testing.T) {
	for _, key := range []string{"CC", "LD_RUN_PATH", "VCToolsInstallDir", "PKG_CONFIG_PATH"} {
		if !Allowed(key) {
			t.Errorf("%s is not in the shared environment signature", key)
		}
	}
	if Allowed("TERM") {
		t.Fatal("unrelated environment variable accepted")
	}
}

func TestAllowedExtra(t *testing.T) {
	t.Setenv("VMAKE_ENV_EXTRA", " MY_FLAG , OTHER ")
	for _, key := range []string{"MY_FLAG", "my_flag", "OTHER"} {
		if !Allowed(key) {
			t.Errorf("VMAKE_ENV_EXTRA key %s rejected", key)
		}
	}
}
