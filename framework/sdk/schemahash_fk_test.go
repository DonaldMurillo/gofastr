package sdk

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// The columns of a multi-column foreign key, and their order, are part of
// the schema: an SDK built against (order_store, order_no) is stale once
// the key reads (order_no, order_store).
func TestForeignKeysMoveSchemaHash(t *testing.T) {
	hash := func(fks ...string) string {
		return SchemaHash([]NamedConfig{{Name: "lines", Config: entity.EntityConfig{
			Fields: []schema.Field{
				{Name: "order_store", Type: schema.String},
				{Name: "order_no", Type: schema.Int},
				{Name: "line_store", Type: schema.String},
			},
			Relations: []entity.Relation{{Type: entity.RelManyToOne, Name: "order", Entity: "orders", ForeignKey: fks}},
		}}})
	}
	base := hash("order_store", "order_no")
	for name, fks := range map[string][]string{
		"reordered":  {"order_no", "order_store"},
		"retargeted": {"line_store", "order_no"},
	} {
		if hash(fks...) == base {
			t.Errorf("%s foreign key did not move the schema hash", name)
		}
	}
}
