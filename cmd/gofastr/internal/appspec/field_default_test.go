package appspec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A field default the type cannot hold passed validate, and the
// generated app panicked at boot registering the entity. A required
// field with default "" did the same ("is required").
func TestFieldDefaultCheckedAtValidate(t *testing.T) {
	load := func(field string) error {
		path := filepath.Join(t.TempDir(), "gofastr.yml")
		spec := "entities:\n  - name: things\n    crud: true\n    fields:\n      - name: title\n        type: string\n" + field
		if err := os.WriteFile(path, []byte(spec), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := LoadAppSpec(path)
		return err
	}
	for _, field := range []string{
		"      - name: status\n        type: enum\n        values: [draft, published]\n        default: shipped\n",
		"      - name: num\n        type: int\n        default: notanumber\n",
		"      - name: flag\n        type: bool\n        default: maybe\n",
		"      - name: label\n        type: string\n        required: true\n        default: \"\"\n",
	} {
		err := load(field)
		if err == nil || !strings.Contains(err.Error(), "invalid Default") || !strings.Contains(err.Error(), `entity "things"`) {
			t.Errorf("field\n%s: got %v, want the invalid Default refusal", field, err)
		}
	}
	for _, field := range []string{
		"      - name: status\n        type: enum\n        values: [draft, published]\n        default: draft\n",
		"      - name: num\n        type: int\n        default: 5\n",
		"      - name: price\n        type: decimal\n        default: 0\n",
		"      - name: flag\n        type: bool\n        default: true\n",
		"      - name: label\n        type: string\n        default: \"\"\n",
	} {
		if err := load(field); err != nil && strings.Contains(err.Error(), "Default") {
			t.Errorf("field\n%s: refused: %v", field, err)
		}
	}
}
