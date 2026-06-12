package entities

import (
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
)

func boolPtr(v bool) *bool        { return &v }
func floatPtr(v float64) *float64 { return &v }

// RegisterAll registers every generated entity declaration with app.
func RegisterAll(app *framework.App) {
	app.Entity("categories", framework.EntityConfig{
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true, Max: floatPtr(100)},
			{Name: "slug", Type: schema.String, Required: true, Unique: true, Pattern: "^[a-z0-9-]+$"},
			{Name: "description", Type: schema.Text},
			{Name: "image", Type: schema.Image},
			{Name: "sort_order", Type: schema.Int, Default: 0, Min: floatPtr(0)},
			{Name: "active", Type: schema.Bool, Default: true},
		},
		Relations: []framework.Relation{
			{Type: framework.RelHasMany, Name: "products", Entity: "products", ForeignKey: "category_id"},
		},
		CRUD:       boolPtr(true),
		MCP:        true,
		Properties: map[string]any{"icon": "folder", "label": "Categories"},
	})
	_ = Categories{}
	app.Entity("order_items", framework.EntityConfig{
		Fields: []schema.Field{
			{Name: "user_id", Type: schema.String},
			{Name: "order_id", Type: schema.Relation, Required: true, To: "orders"},
			{Name: "product_id", Type: schema.Relation, Required: true, To: "products"},
			{Name: "product_name", Type: schema.String, Required: true, Max: floatPtr(200)},
			{Name: "quantity", Type: schema.Int, Required: true, Min: floatPtr(1)},
			{Name: "unit_price", Type: schema.Decimal, Required: true, Min: floatPtr(0)},
			{Name: "total_price", Type: schema.Decimal, Required: true, Min: floatPtr(0)},
		},
		Relations: []framework.Relation{
			{Type: framework.RelManyToOne, Name: "order", Entity: "orders", ForeignKey: "order_id"},
			{Type: framework.RelManyToOne, Name: "product", Entity: "products", ForeignKey: "product_id"},
		},
		OwnerField: "user_id",
		CRUD:       boolPtr(true),
		MCP:        true,
	})
	_ = OrderItems{}
	app.Entity("orders", framework.EntityConfig{
		Fields: []schema.Field{
			{Name: "user_id", Type: schema.String},
			{Name: "order_number", Type: schema.String, Required: true, Unique: true, ReadOnly: true, AutoGenerate: schema.AutoUUID},
			{Name: "status", Type: schema.Enum, Default: "pending", Values: []string{"pending", "confirmed", "processing", "shipped", "delivered", "cancelled", "refunded"}},
			{Name: "customer_name", Type: schema.String, Required: true, Max: floatPtr(200)},
			{Name: "customer_email", Type: schema.String, Required: true, Pattern: "^[^@]+@[^@]+\\.[^@]+$"},
			{Name: "customer_phone", Type: schema.String, Max: floatPtr(30)},
			{Name: "shipping_address", Type: schema.JSON},
			{Name: "billing_address", Type: schema.JSON},
			{Name: "subtotal", Type: schema.Decimal, Required: true, Min: floatPtr(0)},
			{Name: "tax", Type: schema.Decimal, Default: 0, Min: floatPtr(0)},
			{Name: "shipping_cost", Type: schema.Decimal, Default: 0, Min: floatPtr(0)},
			{Name: "total", Type: schema.Decimal, Required: true, Min: floatPtr(0)},
			{Name: "notes", Type: schema.Text},
			{Name: "shipped_at", Type: schema.Timestamp},
			{Name: "delivered_at", Type: schema.Timestamp},
		},
		Relations: []framework.Relation{
			{Type: framework.RelHasMany, Name: "items", Entity: "order_items", ForeignKey: "order_id"},
		},
		OwnerField:   "user_id",
		CRUD:         boolPtr(true),
		MCP:          true,
		CursorField:  "id",
		CursorFields: []string{"created_at", "id"},
		Indices: []framework.Index{
			{Name: "idx_orders_status", Columns: []string{"status"}},
			{Name: "idx_orders_user", Columns: []string{"user_id"}},
		},
		Properties: map[string]any{"icon": "shopping-cart", "label": "Orders"},
	})
	_ = Orders{}
	app.Entity("products", framework.EntityConfig{
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true, Max: floatPtr(200)},
			{Name: "slug", Type: schema.String, Required: true, Unique: true, Pattern: "^[a-z0-9-]+$"},
			{Name: "sku", Type: schema.String, Unique: true, Max: floatPtr(50)},
			{Name: "description", Type: schema.Text},
			{Name: "price", Type: schema.Decimal, Required: true, Min: floatPtr(0)},
			{Name: "compare_at_price", Type: schema.Decimal, Min: floatPtr(0)},
			{Name: "cost", Type: schema.Decimal, Hidden: true, Min: floatPtr(0)},
			{Name: "stock", Type: schema.Int, Required: true, Default: 0, Min: floatPtr(0)},
			{Name: "category_id", Type: schema.Relation, To: "categories"},
			{Name: "status", Type: schema.Enum, Default: "draft", Values: []string{"draft", "active", "archived"}},
			{Name: "featured", Type: schema.Bool, Default: false},
			{Name: "weight", Type: schema.Float, Min: floatPtr(0)},
			{Name: "image", Type: schema.Image},
			{Name: "tags", Type: schema.JSON},
		},
		Relations: []framework.Relation{
			{Type: framework.RelManyToOne, Name: "category", Entity: "categories", ForeignKey: "category_id"},
			{Type: framework.RelHasMany, Name: "reviews", Entity: "reviews", ForeignKey: "product_id"},
			{Type: framework.RelHasMany, Name: "order_items", Entity: "order_items", ForeignKey: "product_id"},
		},
		SoftDelete:   true,
		CRUD:         boolPtr(true),
		MCP:          true,
		CursorField:  "id",
		CursorFields: []string{"created_at", "id"},
		Indices: []framework.Index{
			{Name: "idx_products_category", Columns: []string{"category_id"}},
			{Name: "idx_products_status", Columns: []string{"status"}},
		},
		Properties: map[string]any{"icon": "package", "label": "Products"},
	})
	_ = Products{}
	app.Entity("reviews", framework.EntityConfig{
		Fields: []schema.Field{
			{Name: "product_id", Type: schema.Relation, Required: true, To: "products"},
			{Name: "author_name", Type: schema.String, Required: true, Max: floatPtr(100)},
			{Name: "rating", Type: schema.Int, Required: true, Max: floatPtr(5), Min: floatPtr(1)},
			{Name: "title", Type: schema.String, Max: floatPtr(200)},
			{Name: "body", Type: schema.Text},
			{Name: "verified", Type: schema.Bool, Default: false},
		},
		Relations: []framework.Relation{
			{Type: framework.RelManyToOne, Name: "product", Entity: "products", ForeignKey: "product_id"},
		},
		CRUD: boolPtr(true),
		MCP:  true,
		Indices: []framework.Index{
			{Name: "idx_reviews_product", Columns: []string{"product_id"}},
		},
	})
	_ = Reviews{}
}
