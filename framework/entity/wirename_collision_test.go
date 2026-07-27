package entity

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// A WireName aliases a field's JSON key without changing its DB column. Two
// fields resolving to the SAME wire key is unrepresentable on the wire: one
// JSON key cannot address two columns.
//
// Left unchecked the CRUD layer keeps whichever field it saw first, so writes
// land on that column while filters — which resolve the wire key independently
// — target the other. Reads and writes then disagree with no error anywhere.
//
// crud.go's refreshFieldCache documented this as "a config error that
// ValidateWireNames catches at Define". No such function existed; this is that
// guard.
func TestValidate_RejectsWireKeyCollision(t *testing.T) {
	cases := []struct {
		name   string
		fields []schema.Field
		want   string // substring the error must name
	}{
		{
			name: "WireName collides with another field's literal Name",
			fields: []schema.Field{
				{Name: "author_id", Type: schema.String, WireName: "writer"},
				{Name: "writer", Type: schema.String},
			},
			want: "writer",
		},
		{
			name: "two WireNames collide with each other",
			fields: []schema.Field{
				{Name: "a_col", Type: schema.String, WireName: "shared"},
				{Name: "b_col", Type: schema.String, WireName: "shared"},
			},
			want: "shared",
		},
		{
			name: "WireName collides with a hidden field's name",
			fields: []schema.Field{
				{Name: "secret", Type: schema.String, Hidden: true},
				{Name: "public_col", Type: schema.String, WireName: "secret"},
			},
			want: "secret",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := Define("posts", EntityConfig{Table: "posts", Fields: tc.fields}.WithTimestamps(false))
			err := e.Validate()
			if err == nil {
				t.Fatalf("expected a wire-key collision error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error should name the colliding wire key %q; got: %v", tc.want, err)
			}
		})
	}
}

// The guard must not fire on legitimate configurations, or it breaks every
// existing entity.
func TestValidate_AllowsDistinctWireKeys(t *testing.T) {
	cases := []struct {
		name   string
		fields []schema.Field
	}{
		{
			name: "no WireNames at all",
			fields: []schema.Field{
				{Name: "title", Type: schema.String},
				{Name: "body", Type: schema.String},
			},
		},
		{
			name: "distinct WireNames",
			fields: []schema.Field{
				{Name: "author_id", Type: schema.String, WireName: "writer"},
				{Name: "editor_id", Type: schema.String, WireName: "editor"},
			},
		},
		{
			name: "a field may alias its own name",
			fields: []schema.Field{
				{Name: "title", Type: schema.String, WireName: "title"},
				{Name: "body", Type: schema.String},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := Define("posts", EntityConfig{Table: "posts", Fields: tc.fields}.WithTimestamps(false))
			if err := e.Validate(); err != nil {
				t.Fatalf("legitimate config rejected: %v", err)
			}
		})
	}
}
