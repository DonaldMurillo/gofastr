package crud

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func lineEntity() *entity.Entity {
	return entity.Define("lines", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "order_store", Type: schema.String},
			{Name: "order_no", Type: schema.Int},
		},
		Relations: []entity.Relation{entity.BelongsTo("order", "orders", "order_store", "order_no")},
	})
}

// A partial composite reference is refused for a server write too: the
// host may skip the scope lookup, never the shape of the key.
func TestPartialForeignKeyServerWrite(t *testing.T) {
	ch := &CrudHandler{Entity: lineEntity()}
	ctx := WithServerWrites(context.Background())
	for name, body := range map[string]map[string]any{
		"one column":      {"order_no": 7},
		"one column null": {"order_store": nil, "order_no": 7},
	} {
		var ve *ValidationError
		if err := ch.checkBelongsToScope(ctx, nil, ch.createWrite(ctx, body)); !errors.As(err, &ve) || len(ve.Fields()["order_store"]) == 0 {
			t.Errorf("%s: err = %v; want a validation error on order_store", name, err)
		}
	}
	for name, body := range map[string]map[string]any{
		"neither":   {"sku": "a"},
		"both":      {"order_store": "s1", "order_no": 7},
		"both null": {"order_store": nil, "order_no": nil},
	} {
		if err := ch.checkBelongsToScope(ctx, nil, ch.createWrite(ctx, body)); err != nil {
			t.Errorf("%s: err = %v; want nil", name, err)
		}
	}
}

// The database assigns an AutoIncrement column after the check, so a
// create naming the rest of a reference through one is refused.
func TestAutoIncrementForeignKeyColumn(t *testing.T) {
	e := entity.Define("lines", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "order_store", Type: schema.Int, AutoGenerate: schema.AutoIncrement},
			{Name: "order_no", Type: schema.Int},
		},
		Relations: []entity.Relation{entity.BelongsTo("order", "orders", "order_store", "order_no")},
	})
	ch := &CrudHandler{Entity: e}
	var ve *ValidationError
	err := ch.checkBelongsToScope(context.Background(), nil, ch.createWrite(context.Background(), map[string]any{"order_no": 7}))
	if !errors.As(err, &ve) || len(ve.Fields()["order_no"]) == 0 {
		t.Errorf("err = %v; want order_no refused", err)
	}
}

// llm.md names every column of a multi-column foreign key.
func TestLLMMDListsForeignKeyColumns(t *testing.T) {
	if md := EntityLLMMD(lineEntity()); !strings.Contains(md, "`order_store, order_no`") {
		t.Errorf("llm.md does not name both foreign-key columns:\n%s", md)
	}
}
