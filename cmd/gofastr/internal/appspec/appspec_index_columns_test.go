package appspec

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
	fwentity "github.com/DonaldMurillo/gofastr/framework/entity"
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

// A multi-tenant entity's tenant column is the one tenant_field names.
// With a custom name the table has no tenant_id, so an index cannot name
// it. The load refuses every multi_tenant entity today (the generator
// cannot emit a tenant resolver), so this asks the column rule itself:
// it must already be right the day that refusal lifts.
func TestIndexTenantColumnFollowsTenantField(t *testing.T) {
	decl := func(tenantField string) framework.EntityDeclaration {
		return framework.EntityDeclaration{
			Name:   "posts",
			Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}},
			Scope:  &fwentity.ScopeDeclaration{MultiTenant: true, TenantField: tenantField},
		}
	}
	for _, tc := range []struct {
		tenantField, col string
		want             bool
	}{
		{"", "tenant_id", true},
		{"tenant_id", "tenant_id", true},
		{"account_id", "account_id", true},
		{"account_id", "tenant_id", false},
		{"account_id", "title", true},
	} {
		if got := appSpecIndexableColumn(decl(tc.tenantField), tc.col); got != tc.want {
			t.Errorf("tenant_field %q: index column %q indexable = %v, want %v", tc.tenantField, tc.col, got, tc.want)
		}
	}
	// A field the entity declares under that name is a column whatever
	// the tenant column is called.
	d := decl("account_id")
	d.Fields = append(d.Fields, framework.FieldDeclaration{Name: "tenant_id", Type: "string"})
	if !appSpecIndexableColumn(d, "tenant_id") {
		t.Error("a declared field named tenant_id is not indexable")
	}
}
