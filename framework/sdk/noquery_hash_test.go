package sdk

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestSchemaHashChangesWhenNoQueryChanges(t *testing.T) {
	config := func(noQuery bool) entity.EntityConfig {
		return entity.EntityConfig{Fields: []schema.Field{
			{Name: "number", Type: schema.String, NoQuery: noQuery},
		}}.WithTimestamps(false)
	}
	plain := SchemaHash([]NamedConfig{{Name: "cards", Config: config(false)}})
	masked := SchemaHash([]NamedConfig{{Name: "cards", Config: config(true)}})
	if plain == masked {
		t.Fatalf("SchemaHash did not change when NoQuery changed: %s", plain)
	}
}

// TestSchemaHashSeesBigDefaults: two defaults past 2^53 that differ in
// the last digit hash differently, and an int and a float64 default of
// the same ordinary value still hash the same.
func TestSchemaHashSeesBigDefaults(t *testing.T) {
	hash := func(def any) string {
		return SchemaHash([]NamedConfig{{Name: "cards", Config: entity.EntityConfig{Fields: []schema.Field{
			{Name: "n", Type: schema.Int, Default: def},
		}}.WithTimestamps(false)}})
	}
	if hash(int64(9007199254740993)) == hash(int64(9007199254740992)) {
		t.Fatal("defaults 9007199254740993 and 9007199254740992 hashed the same")
	}
	if hash(5) != hash(float64(5)) {
		t.Fatal("int 5 and float64 5 defaults hashed differently")
	}
}
