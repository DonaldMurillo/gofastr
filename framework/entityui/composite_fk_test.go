package entityui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A cart keyed on (store, no) lists the items whose two-column foreign
// key names it: the Related tab pins each column to its part of the key,
// counts only those items, and its New pre-fills both columns.
func TestRelatedByCompositeForeignKey(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{
			"carts": {
				PrimaryKey: []string{"store", "no"},
				Fields: fields(
					schema.Field{Name: "store", Type: schema.String, Required: true},
					schema.Field{Name: "no", Type: schema.Int, Required: true},
				),
				Exposure: &entity.ExposureConfig{Public: true},
			},
			"items": {
				Fields: fields(
					schema.Field{Name: "order_store", Type: schema.String},
					schema.Field{Name: "order_no", Type: schema.Int},
					schema.Field{Name: "sku", Type: schema.String},
				),
				Relations: []entity.Relation{entity.BelongsTo("cart", "carts", "order_store", "order_no")},
				Exposure:  &entity.ExposureConfig{Public: true},
			},
		},
		map[string][]map[string]any{
			"carts": {{"store": "s1", "no": 7}, {"store": "s1", "no": 8}, {"store": "s2", "no": 7}},
			"items": {
				{"id": "l1", "order_store": "s1", "order_no": 7, "sku": "sku-a"},
				{"id": "l2", "order_store": "s1", "order_no": 7, "sku": "sku-b"},
				{"id": "l3", "order_store": "s1", "order_no": 8, "sku": "sku-c"},
				{"id": "l4", "order_store": "s2", "order_no": 7, "sku": "sku-d"},
			},
		},
		withAPI(map[string]string{"carts": "/api/carts", "items": "/api/items"}),
	)
	id := entity.JoinKey([]string{"s1", "7"})
	body := string(x.ui.Record("carts", id).Base("/rec/carts").Related("items").
		RenderCtx(x.userCtx("/rec/carts/s1/7", "?tab=related", "u1")))

	if got := tabBadge(t, body, "related"); got != "2" {
		t.Errorf("Related counts %q, want 2", got)
	}
	for _, sku := range []string{"sku-a", "sku-b"} {
		if !strings.Contains(body, sku) {
			t.Errorf("the Related list is missing %s:\n%s", sku, body)
		}
	}
	for _, sku := range []string{"sku-c", "sku-d"} {
		if strings.Contains(body, sku) {
			t.Errorf("the Related list draws %s, whose cart matches only one key column:\n%s", sku, body)
		}
	}
	for _, want := range []string{"prefill_order_store=s1", "prefill_order_no=7"} {
		if !strings.Contains(body, want) {
			t.Errorf("the Related tab's New does not pre-fill %s:\n%s", want, body)
		}
	}
}
