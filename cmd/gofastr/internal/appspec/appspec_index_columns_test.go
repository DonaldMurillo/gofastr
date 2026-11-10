package appspec

import (
	"strings"
	"testing"
)

// An index over a column the entity does not have loaded clean, and the
// migration `migrate generate` wrote from it failed its first `migrate up`
// with "no such column" (the generated app's boot failed the same way).
// The load refuses it, naming the index and the column.
func TestIndexRefusesUndeclaredColumn(t *testing.T) {
	for name, tc := range map[string]struct{ index, want string }{
		"undeclared": {
			index: "      - name: idx_posts_nope\n        columns: [title, nope]\n",
			want:  `names column "nope"`,
		},
		"no columns": {
			index: "      - name: idx_posts_empty\n",
			want:  "names no columns",
		},
		"repeated": {
			index: "      - name: idx_posts_twice\n        columns: [title, title]\n",
			want:  `lists column "title" twice`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := loadBPWithEntity(t, "  - name: posts\n    indices:\n"+tc.index+
				"    fields:\n      - name: title\n        type: string\n")
			if err == nil {
				t.Fatalf("load accepted the index; want an error holding %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to hold %q", err, tc.want)
			}
		})
	}
}

// The columns an index may cover: declared fields, a relation's key
// column, and the columns the framework adds.
func TestIndexAcceptsEntityColumns(t *testing.T) {
	_, err := loadBPWithEntity(t, `  - name: authors
    fields:
      - name: name
        type: string
  - name: posts
    soft_delete: true
    indices:
      - name: idx_posts_feed
        columns: [author_id, created_at]
      - name: idx_posts_live
        columns: [deleted_at, updated_at, id]
        unique: true
      - columns: [title]
    fields:
      - name: title
        type: string
      - name: author_id
        type: relation
        to: authors
`)
	if err != nil {
		t.Fatalf("load refused indices over the entity's own columns: %v", err)
	}
}

// Under a natural key the table has no id column, so an index cannot name it.
func TestIndexRefusesIDUnderNaturalKey(t *testing.T) {
	_, err := loadBPWithEntity(t, `  - name: currencies
    primary_key: [code]
    indices:
      - name: idx_currencies_id
        columns: [id]
    fields:
      - name: code
        type: string
        required: true
`)
	if err == nil || !strings.Contains(err.Error(), `names column "id"`) {
		t.Fatalf("error = %v, want the id column refused", err)
	}
}
