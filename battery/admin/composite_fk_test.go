package admin

import (
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A child that belongs to its parent through a two-column foreign key is
// on the parent's Related tab, and the parent's record is one the child's
// New opens over, as a one-column reference is.
func TestRelatedThroughCompositeForeignKey(t *testing.T) {
	orders := entity.Define("orders", entity.EntityConfig{
		PrimaryKey: []string{"store", "no"},
		Fields: []schema.Field{
			{Name: "store", Type: schema.String, Required: true},
			{Name: "no", Type: schema.Int, Required: true},
		},
	})
	lines := entity.Define("lines", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "order_store", Type: schema.String},
			{Name: "order_no", Type: schema.Int},
		},
		Relations: []entity.Relation{entity.BelongsTo("order", "orders", "order_store", "order_no")},
	})
	b := &Battery{ents: []*entity.Entity{orders, lines}}
	if got := b.relatedTo(orders); !slices.Equal(got, []string{"lines"}) {
		t.Errorf("relatedTo(orders) = %v, want [lines]", got)
	}
	if got := b.parentRecords(lines); !slices.Equal(got, []string{"/entities/orders/:store/:no"}) {
		t.Errorf("parentRecords(lines) = %v, want the order's record pattern", got)
	}
}
