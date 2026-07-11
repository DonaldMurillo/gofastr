package entity

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

func expectDefinePanic(t *testing.T, wantSubstr string, config EntityConfig) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("Define did not panic (want %q)", wantSubstr)
		}
		if msg, ok := r.(string); !ok || !strings.Contains(msg, wantSubstr) {
			t.Fatalf("panic = %v, want substring %q", r, wantSubstr)
		}
	}()
	Define("bad", config)
}

func TestCrossOwnerReadRequiresOwnerField(t *testing.T) {
	expectDefinePanic(t, "CrossOwnerRead", EntityConfig{
		Fields:         []schema.Field{{Name: "title", Type: schema.String}},
		CrossOwnerRead: "things:read:all",
	})
}

func TestCrossOwnerReadWithOwnerFieldOK(t *testing.T) {
	e := Define("tickets", EntityConfig{
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
			{Name: "user_id", Type: schema.String, Hidden: true},
		},
		OwnerField:     "user_id",
		CrossOwnerRead: "tickets:read:all",
	})
	if e.Config.CrossOwnerRead != "tickets:read:all" {
		t.Fatalf("CrossOwnerRead = %q", e.Config.CrossOwnerRead)
	}
}

func TestSearchFieldsUnknownColumnPanics(t *testing.T) {
	expectDefinePanic(t, "not a declared field", EntityConfig{
		Fields:       []schema.Field{{Name: "title", Type: schema.String}},
		SearchFields: []string{"body"},
	})
}

func TestSearchFieldsHiddenColumnPanics(t *testing.T) {
	expectDefinePanic(t, "Hidden", EntityConfig{
		Fields:       []schema.Field{{Name: "secret", Type: schema.String, Hidden: true}},
		SearchFields: []string{"secret"},
	})
}

func TestSearchFieldsNonTextColumnPanics(t *testing.T) {
	expectDefinePanic(t, "must be String or Text", EntityConfig{
		Fields:       []schema.Field{{Name: "weight", Type: schema.Float}},
		SearchFields: []string{"weight"},
	})
}

func TestSearchFieldsValidColumnsOK(t *testing.T) {
	e := Define("posts", EntityConfig{
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
			{Name: "body", Type: schema.Text},
		},
		SearchFields: []string{"title", "body"},
	})
	if len(e.Config.SearchFields) != 2 {
		t.Fatalf("SearchFields = %v", e.Config.SearchFields)
	}
}
