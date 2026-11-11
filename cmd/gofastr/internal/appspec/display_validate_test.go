package appspec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An entity's display: block was checked only for its key shape: a
// title field the entity does not declare, a facet on a string field,
// or a view filter on a missing column passed validate, and the
// generated app panicked at boot registering the entity.
func TestDisplayCheckedAtValidate(t *testing.T) {
	load := func(display string) error {
		path := filepath.Join(t.TempDir(), "gofastr.yml")
		spec := "entities:\n  - name: memos\n    crud: true\n    fields:\n      - name: title\n        type: string\n        required: true\n      - name: status\n        type: enum\n        values: [a, b]\n      - name: secret\n        type: string\n        hidden: true\n    display:\n" + display
		if err := os.WriteFile(path, []byte(spec), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := LoadAppSpec(path)
		return err
	}
	for _, c := range []struct{ display, want string }{
		{"      title_fields: [missing_field]\n", `"missing_field"`},
		{"      facets: [title]\n", "must be an Enum, Bool or Relation"},
		{"      columns: [secret]\n", "Hidden"},
		{"      form:\n        main:\n          - row: [title, secret]\n", "Hidden"},
		{"      views:\n        - key: open\n          where: 'nope = 1'\n", `view "open" where`},
	} {
		err := load(c.display)
		if err == nil || !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), `"memos"`) {
			t.Errorf("display\n%s: got %v, want an error naming %q", c.display, err, c.want)
		}
	}
	for _, display := range []string{
		"      title_fields: [title]\n      facets: [status]\n      columns: [title, created_at]\n",
		"      views:\n        - key: open\n          where: 'status = \"a\"'\n",
	} {
		if err := load(display); err != nil {
			t.Errorf("display\n%s: refused: %v", display, err)
		}
	}
}
