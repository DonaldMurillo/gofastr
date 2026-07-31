package dsl

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func noQueryDSLRegistry(t *testing.T) dslRegistry {
	t.Helper()
	ent := entity.Define("cards", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "label", Type: schema.String},
			{Name: "number", Type: schema.String, NoQuery: true},
		},
	})
	return dslRegistry{ents: map[string]*entity.Entity{"cards": ent}}
}

func TestBuildDSLRejectsNoQuerySurfaces(t *testing.T) {
	reg := noQueryDSLRegistry(t)
	ent, err := reg.Get("cards")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "filter", input: `cards.where(number="4111")`, want: "cannot be filtered"},
		{name: "sort", input: `cards.order(number ASC)`, want: "cannot be sorted"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := BuildDSLQuery(reg, tc.input)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("BuildDSLQuery(%q) error = %v, want %q", tc.input, err, tc.want)
			}
		})
	}

	// Define rejects a static NoQuery cursor. Set it after definition so this
	// test reaches the DSL's own after() guard.
	ent.Config.Pagination.CursorField = "number"
	_, err = BuildDSLQuery(reg, `cards.after("4111")`)
	if err == nil || !strings.Contains(err.Error(), "cannot be queried") {
		t.Fatalf("NoQuery after() error = %v, want cannot be queried", err)
	}
}
