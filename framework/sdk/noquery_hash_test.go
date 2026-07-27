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
