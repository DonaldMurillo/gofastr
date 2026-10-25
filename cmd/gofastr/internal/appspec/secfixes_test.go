package appspec

import (
	"os"
	"testing"
)

// F1: app spec YAML decoder preserves owner_field.
func TestAppSpecOwnerField(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/gofastr.yml"
	yml := "app:\n  name: D\n  module: ex.com/d\nentities:\n" +
		"  - name: note\n    owner_field: user_id\n    fields:\n      - name: body\n        type: string\n"
	if err := os.WriteFile(path, []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}
	bp, err := LoadAppSpec(path)
	if err != nil {
		t.Fatalf("loadAppSpec: %v", err)
	}
	if len(bp.Entities) != 1 || bp.Entities[0].Scope.OwnerField != "user_id" {
		t.Fatalf("owner_field dropped: %#v", bp.Entities)
	}
}
