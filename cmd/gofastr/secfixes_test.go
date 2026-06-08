package main

import (
	"encoding/base64"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// F1: OwnerField must be emitted by the codegen text emitter.
func TestEmitOwnerField(t *testing.T) {
	reg, err := renderEntityRegistration(framework.EntityDeclaration{
		Name:       "notes",
		OwnerField: "user_id",
		Fields: []framework.FieldDeclaration{
			{Name: "title", Type: "string"},
		},
	})
	if err != nil {
		t.Fatalf("renderEntityRegistration: %v", err)
	}
	if !strings.Contains(reg, `OwnerField: "user_id"`) {
		t.Fatalf("generated registration missing OwnerField:\n%s", reg)
	}
}

// F1: blueprint YAML decoder preserves owner_field.
func TestBlueprintOwnerField(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/gofastr.yml"
	yml := "app:\n  name: D\n  module: ex.com/d\nentities:\n" +
		"  - name: note\n    owner_field: user_id\n    fields:\n      - name: body\n        type: string\n"
	if err := os.WriteFile(path, []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}
	bp, err := loadBlueprint(path)
	if err != nil {
		t.Fatalf("loadBlueprint: %v", err)
	}
	if len(bp.Entities) != 1 || bp.Entities[0].OwnerField != "user_id" {
		t.Fatalf("owner_field dropped: %#v", bp.Entities)
	}
}

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
