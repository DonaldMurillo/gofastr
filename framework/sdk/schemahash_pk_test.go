package sdk

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// The primary-key columns, and their order, are part of the schema: a
// generated client takes the key as positional arguments in PrimaryKey
// order, so an SDK built for (region, code) passes its arguments the
// wrong way round once the key reads (code, region). Fields are sorted
// by name in the projection, so the key order has to be hashed on its
// own (2026-10-27 bug bash G4).
func TestPrimaryKeyOrderMovesSchemaHash(t *testing.T) {
	hash := func(pk ...string) string {
		return SchemaHash([]NamedConfig{{Name: "depots", Config: entity.EntityConfig{
			PrimaryKey: pk,
			Fields: []schema.Field{
				{Name: "region", Type: schema.String, Required: true},
				{Name: "code", Type: schema.String, Required: true},
				{Name: "label", Type: schema.String},
			},
		}}})
	}
	base := hash("region", "code")
	for name, pk := range map[string][]string{
		"reordered":  {"code", "region"},
		"narrowed":   {"code"},
		"retargeted": {"region", "label"},
	} {
		if hash(pk...) == base {
			t.Errorf("%s primary key did not move the schema hash", name)
		}
	}
	if hash("region", "code") != base {
		t.Error("the same primary key hashed differently")
	}
}
