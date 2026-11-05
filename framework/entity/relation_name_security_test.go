package entity

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

func TestRelationNamesDoNotShadowScalars(t *testing.T) {
	for _, field := range []schema.Field{
		{Type: schema.String},
		{Type: schema.Text},
		{Type: schema.Int},
		{Type: schema.JSON},
		{Type: schema.JSON, Hidden: true},
		{Type: schema.UUID, AutoGenerate: schema.AutoUUID, Hidden: true},
	} {
		field.Name = "profile"
		for _, relation := range []Relation{
			BelongsTo("profile", "profiles", "profile_id").Cascade(),
			HasOne("profile", "profiles", "record_id"),
			HasMany("profile", "profiles", "record_id"),
			ManyToMany("profile", "profiles", "record_profiles", "record_id", "profile_id"),
		} {
			config := EntityConfig{
				Fields: []schema.Field{
					field,
					{Name: "profile_id", Type: schema.String},
				},
				Relations: []Relation{relation},
			}
			if err := Define("records", config).Validate(); err == nil || !strings.Contains(err.Error(), "profile") {
				t.Errorf("scalar field %v relation type %v: collision accepted, err=%v", field, relation.Type, err)
			}
		}
	}
	for _, config := range []EntityConfig{
		{Fields: []schema.Field{{Name: "profile", Type: schema.Relation, To: "profiles"}}},
		{
			Fields:    []schema.Field{{Name: "profile", Type: schema.Relation, To: "profiles"}},
			Relations: []Relation{BelongsTo("profile", "profiles", "profile")},
		},
		{
			Fields:    []schema.Field{{Name: "profile_id", Type: schema.String}},
			Relations: []Relation{BelongsTo("profile", "profiles", "profile_id")},
		},
	} {
		if err := Define("records", config).Validate(); err != nil {
			t.Errorf("valid relation refused: %v", err)
		}
	}
}
