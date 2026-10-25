package harnesscmd

import (
	"encoding/base64"
	"encoding/hex"
	"os"
	"testing"
)

// F5: profiles load with CWD outside the source tree (installed binary).
func TestLoadProfileOutsideRepo(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	if _, err := loadProfile(true, ""); err != nil {
		t.Fatalf("loadProfile(framework) outside repo: %v", err)
	}
	if _, err := loadProfile(false, ""); err != nil {
		t.Fatalf("loadProfile(default) outside repo: %v", err)
	}
}

// F10: MachineKey env accepts raw-32, hex-64, base64; rejects bad values.
func TestMachineKeyEncodings(t *testing.T) {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte('a' + i%26)
	}
	cases := map[string]string{
		"raw":    string(raw),
		"hex":    hex.EncodeToString(raw),
		"base64": base64.StdEncoding.EncodeToString(raw),
	}
	for name, val := range cases {
		t.Setenv("GOFASTR_HARNESS_MACHINE_KEY", val)
		got, err := machineKeyFromEnv()
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", name, err)
		}
		if len(got) != 32 || string(got) != string(raw) {
			t.Fatalf("%s: decoded key mismatch", name)
		}
	}
	t.Setenv("GOFASTR_HARNESS_MACHINE_KEY", "not-a-valid-key")
	if _, err := machineKeyFromEnv(); err == nil {
		t.Fatalf("expected error for bad machine key")
	}
	t.Setenv("GOFASTR_HARNESS_MACHINE_KEY", "")
	got, err := machineKeyFromEnv()
	if err != nil {
		t.Fatalf("empty env should not error: %v", err)
	}
	if got != nil {
		t.Fatalf("empty env should yield nil key")
	}
}
