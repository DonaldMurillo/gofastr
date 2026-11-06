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

func TestScalarFKRelationNames(t *testing.T) {
	for _, field := range []schema.Field{
		{Name: "author_id", Type: schema.String},
		{Name: "account_ref", Type: schema.UUID},
		{Name: "tenant_no", Type: schema.Int},
	} {
		t.Run(field.Name, func(t *testing.T) {
			for _, relation := range []Relation{
				BelongsTo(field.Name, "owners", field.Name),
				BelongsTo(field.Name, "owners", field.Name).Cascade(),
				BelongsTo(field.Name, "owners", field.Name, "region"),
				BelongsTo(field.Name, "owners", "other_key"),
				HasOne(field.Name, "owners", field.Name),
				HasMany(field.Name, "owners", field.Name),
				ManyToMany(field.Name, "owners", "record_owners", "record_id", field.Name),
			} {
				config := EntityConfig{
					Fields: []schema.Field{
						field,
						{Name: "region", Type: schema.String},
						{Name: "other_key", Type: schema.String},
					},
					Relations: []Relation{relation},
				}
				err := Define("records", config).Validate()
				valid := relation.Type == RelManyToOne && !relation.CascadeWrite &&
					len(relation.ForeignKey) == 1 && relation.ForeignKey[0] == field.Name
				if valid && err != nil {
					t.Errorf("own foreign-key relation refused: %v", err)
				}
				if !valid && (err == nil || !strings.Contains(err.Error(), "conflicts with scalar field")) {
					t.Errorf("ambiguous relation %+v accepted, err=%v", relation, err)
				}
			}
		})
	}
}
