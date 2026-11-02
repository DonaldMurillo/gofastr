package apiversions_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/experimental/apiversions"
)

// TestApplyToEntityConfigRefusesUnknownFieldNames pins that a
// projection naming a field the entity does not declare fails loudly at
// startup instead of being ignored. Exclude is how a version hides a
// column (an internal note, a secret) from its wire output; a name that
// matched nothing, a typo or the column's camelCase wire key instead of
// its field name, left the column visible on that version with no sign
// anything was wrong.
func TestApplyToEntityConfigRefusesUnknownFieldNames(t *testing.T) {
	base := entity.EntityConfig{Fields: []schema.Field{
		{Name: "id", Type: schema.Int},
		{Name: "title", Type: schema.String},
		{Name: "internal_notes", Type: schema.String},
	}}
	cases := map[string]*apiversions.Projection{
		"exclude typo":      {Version: "v1", Exclude: []string{"internal_note"}},
		"exclude wire name": {Version: "v1", Exclude: []string{"internalNotes"}},
		"include typo":      {Version: "v1", Include: []string{"id", "titel"}},
		"rename typo":       {Version: "v1", Rename: map[string]string{"titel": "headline"}},
	}
	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			ps := apiversions.NewProjectionSet(p)
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("ApplyToEntityConfig accepted a projection naming an undeclared field")
				}
				if !strings.Contains(r.(string), "apiversions:") {
					t.Fatalf("panic = %v, want an apiversions message", r)
				}
			}()
			_ = apiversions.ApplyToEntityConfig(base, ps, "v1")
		})
	}

	// A projection naming declared fields still applies.
	ps := apiversions.NewProjectionSet(&apiversions.Projection{Version: "v1", Exclude: []string{"internal_notes"}})
	cfg := apiversions.ApplyToEntityConfig(base, ps, "v1")
	for _, f := range cfg.Fields {
		if f.Hidden != (f.Name == "internal_notes") {
			t.Errorf("field %s Hidden = %v", f.Name, f.Hidden)
		}
	}
}
