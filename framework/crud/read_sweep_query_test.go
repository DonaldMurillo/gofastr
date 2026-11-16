package crud

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A repeated ?q= is the host's only when the framework reads no q at all:
// a filterable column named q or declared SearchFields keep it a control.
func TestReadQueryRepeatedQOwnership(t *testing.T) {
	cases := []struct {
		name    string
		cfg     func(*entity.EntityConfig)
		refused bool
	}{
		{"host owns q", func(c *entity.EntityConfig) { c.AllowedFilterParams = []string{"q"} }, false},
		{"search fields", func(c *entity.EntityConfig) {
			c.AllowedFilterParams = []string{"q"}
			c.SearchFields = []string{"name"}
		}, true},
		{"column named q", func(c *entity.EntityConfig) {
			c.AllowedFilterParams = []string{"q"}
			c.Fields = append(c.Fields, schema.Field{Name: "q", Type: schema.String})
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ch, _ := covItems(t, tc.cfg, 0)
			_, err := ch.readQuery(httptest.NewRequest("GET", "/items?q=a&q=b", nil))
			if (err != nil) != tc.refused {
				t.Fatalf("repeated q: err = %v, want refused %v", err, tc.refused)
			}
		})
	}
}

// The exported soft-delete helpers cannot answer an error, so a query they
// cannot read strictly shows no trashed row.
func TestSoftDeleteHelpersFailClosed(t *testing.T) {
	ch, _ := covItems(t, func(c *entity.EntityConfig) { c.Scope = &entity.ScopeConfig{SoftDelete: true} }, 0)
	hidesTrashed := func(raw string) bool {
		qb := query.Select("id").From("items")
		ch.ApplySoftDeleteFilter(qb, withTestUser(httptest.NewRequest("GET", "/items?"+raw, nil), "u1"))
		sql, _ := qb.Build()
		return strings.Contains(sql, "deleted_at IS NULL")
	}
	if hidesTrashed("trashed=true") {
		t.Fatal("control: an authenticated ?trashed=true should show trashed rows")
	}
	for _, raw := range []string{"trashed=true&trashed=true", "trashed=true&x=%ZZ"} {
		if !hidesTrashed(raw) {
			t.Errorf("?%s showed trashed rows", raw)
		}
	}
}
