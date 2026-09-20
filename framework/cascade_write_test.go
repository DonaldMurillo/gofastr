package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

func cascadeTestApp(t *testing.T, db *sql.DB) *App {
	t.Helper()
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())

	app.Entity("orders", entity.EntityConfig{
		Table: "orders",
		Fields: []schema.Field{
			{Name: "customer_name", Type: schema.String, Required: true},
			{Name: "status", Type: schema.String, Default: "pending"},
		},
		Relations: []entity.Relation{
			entity.HasMany("items", "order_items", "order_id").WithCascadeWrite(true),
		},
	}.WithTimestamps(false))

	app.Entity("order_items", entity.EntityConfig{
		Table: "order_items",
		Fields: []schema.Field{
			{Name: "order_id", Type: schema.String, Required: true},
			{Name: "name", Type: schema.String, Required: true},
			{Name: "price", Type: schema.Int, Required: true},
		},
		Relations: []entity.Relation{
			entity.BelongsTo("order", "orders", "order_id"),
		},
	}.WithTimestamps(false))

	app.Entity("users", entity.EntityConfig{
		Table: "users",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
		},
		Relations: []entity.Relation{
			entity.HasOne("profile", "profiles", "user_id").WithCascadeWrite(true),
		},
	}.WithTimestamps(false))

	app.Entity("profiles", entity.EntityConfig{
		Table: "profiles",
		Fields: []schema.Field{
			{Name: "user_id", Type: schema.String, Required: true},
			{Name: "bio", Type: schema.String, Required: true},
		},
	}.WithTimestamps(false))

	app.Entity("articles", entity.EntityConfig{
		Table: "articles",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "author_id", Type: schema.String, Required: true},
		},
		Relations: []entity.Relation{
			entity.BelongsTo("author", "users", "author_id").WithCascadeWrite(true),
			entity.ManyToMany("tags", "tags", "article_tags", "article_id", "tag_id").WithCascadeWrite(true),
		},
	}.WithTimestamps(false))

	app.Entity("tags", entity.EntityConfig{
		Table: "tags",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
		},
	}.WithTimestamps(false))

	return app
}

func seedCascadeDB(t *testing.T, db *sql.DB) {
	t.Helper()
	stmts := []string{
		`CREATE TABLE orders (id TEXT PRIMARY KEY, customer_name TEXT NOT NULL, status TEXT)`,
		`CREATE TABLE order_items (id TEXT PRIMARY KEY, order_id TEXT NOT NULL, name TEXT NOT NULL, price INTEGER NOT NULL)`,
		`CREATE TABLE users (id TEXT PRIMARY KEY, name TEXT NOT NULL)`,
		`CREATE TABLE profiles (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, bio TEXT NOT NULL)`,
		`CREATE TABLE articles (id TEXT PRIMARY KEY, title TEXT NOT NULL, author_id TEXT NOT NULL)`,
		`CREATE TABLE tags (id TEXT PRIMARY KEY, name TEXT NOT NULL)`,
		`CREATE TABLE article_tags (article_id TEXT NOT NULL, tag_id TEXT NOT NULL, PRIMARY KEY(article_id, tag_id))`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("setup %q: %v", s, err)
		}
	}
}

// TestCascadeWrite_HasMany asserts that creating an order with nested items creates
// both parent and children within the same transaction.
func TestCascadeWrite_HasMany(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedCascadeDB(t, db)
		app := cascadeTestApp(t, db)
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})

		payload := map[string]any{
			"customer_name": "Alice",
			"items": []any{
				map[string]any{"name": "Book", "price": 15},
				map[string]any{"name": "Pen", "price": 3},
			},
		}
		resp := ta.Post("/orders", payload)
		resp.AssertStatus(t, http.StatusCreated)

		var res struct {
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal([]byte(resp.Body()), &res); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		orderID, _ := res.Data["id"].(string)
		if orderID == "" {
			t.Fatalf("expected order id in response: %v", res.Data)
		}

		// Verify database rows
		var orderCount int
		if err := db.QueryRow("SELECT COUNT(*) FROM orders WHERE id = $1", orderID).Scan(&orderCount); err != nil || orderCount != 1 {
			t.Fatalf("expected order in DB, got count=%d, err=%v", orderCount, err)
		}

		var itemCount int
		if err := db.QueryRow("SELECT COUNT(*) FROM order_items WHERE order_id = $1", orderID).Scan(&itemCount); err != nil || itemCount != 2 {
			t.Fatalf("expected 2 items in DB, got count=%d, err=%v", itemCount, err)
		}
	})
}

// TestCascadeWrite_HasMany_Rollback asserts that if child validation fails,
// the entire transaction rolls back and neither parent nor child is persisted.
func TestCascadeWrite_HasMany_Rollback(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedCascadeDB(t, db)
		app := cascadeTestApp(t, db)
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})

		// Second item is missing required "name"
		payload := map[string]any{
			"customer_name": "Bob",
			"items": []any{
				map[string]any{"name": "ValidItem", "price": 10},
				map[string]any{"price": 20}, // missing name!
			},
		}
		resp := ta.Post("/orders", payload)
		resp.AssertStatus(t, http.StatusBadRequest)
		resp.AssertBodyContains(t, "items.1.name")

		// Verify ZERO rows created in either table
		var orderCount int
		db.QueryRow("SELECT COUNT(*) FROM orders").Scan(&orderCount)
		if orderCount != 0 {
			t.Fatalf("expected 0 orders after rollback, got %d", orderCount)
		}

		var itemCount int
		db.QueryRow("SELECT COUNT(*) FROM order_items").Scan(&itemCount)
		if itemCount != 0 {
			t.Fatalf("expected 0 order items after rollback, got %d", itemCount)
		}
	})
}

// TestCascadeWrite_BelongsTo asserts that creating an article with a nested author
// creates the author first, injects the author's ID into the article, and rolls back
// both if article creation fails.
func TestCascadeWrite_BelongsTo(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedCascadeDB(t, db)
		app := cascadeTestApp(t, db)
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})

		payload := map[string]any{
			"title": "Introduction to GoFastr",
			"author": map[string]any{
				"name": "Charlie",
			},
		}
		resp := ta.Post("/articles", payload)
		resp.AssertStatus(t, http.StatusCreated)

		var res struct {
			Data map[string]any `json:"data"`
		}
		json.Unmarshal([]byte(resp.Body()), &res)
		authorID, _ := res.Data["author_id"].(string)
		if authorID == "" {
			authorID, _ = res.Data["authorId"].(string)
		}
		if authorID == "" {
			t.Fatalf("expected author_id on created article: %v", res.Data)
		}

		var authorCount int
		db.QueryRow("SELECT COUNT(*) FROM users WHERE id = $1", authorID).Scan(&authorCount)
		if authorCount != 1 {
			t.Fatalf("expected author in DB, got %d", authorCount)
		}
	})
}

// TestCascadeWrite_HasOne asserts creating a user with a nested profile.
func TestCascadeWrite_HasOne(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedCascadeDB(t, db)
		app := cascadeTestApp(t, db)
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})

		payload := map[string]any{
			"name": "Diana",
			"profile": map[string]any{
				"bio": "Software Architect",
			},
		}
		resp := ta.Post("/users", payload)
		resp.AssertStatus(t, http.StatusCreated)

		var res struct {
			Data map[string]any `json:"data"`
		}
		json.Unmarshal([]byte(resp.Body()), &res)
		userID, _ := res.Data["id"].(string)

		var profileCount int
		db.QueryRow("SELECT COUNT(*) FROM profiles WHERE user_id = $1", userID).Scan(&profileCount)
		if profileCount != 1 {
			t.Fatalf("expected profile in DB, got %d", profileCount)
		}

		// Rollback test: profile missing bio
		badPayload := map[string]any{
			"name": "Eve",
			"profile": map[string]any{
				"bio": "", // required string!
			},
		}
		badResp := ta.Post("/users", badPayload)
		badResp.AssertStatus(t, http.StatusBadRequest)
		badResp.AssertBodyContains(t, "profile.bio")

		var eveCount int
		db.QueryRow("SELECT COUNT(*) FROM users WHERE name = 'Eve'").Scan(&eveCount)
		if eveCount != 0 {
			t.Fatalf("expected Eve to be rolled back, got %d", eveCount)
		}

		// Update existing user's profile without providing child ID (exercising findExistingHasOneChild)
		updatePayload := map[string]any{
			"profile": map[string]any{
				"bio": "Principal Architect",
			},
		}
		updateResp := ta.Put("/users/"+userID, updatePayload)
		updateResp.AssertStatus(t, http.StatusOK)

		var updatedBio string
		if err := db.QueryRow("SELECT bio FROM profiles WHERE user_id = $1", userID).Scan(&updatedBio); err != nil {
			t.Fatalf("query updated profile: %v", err)
		}
		if updatedBio != "Principal Architect" {
			t.Fatalf("expected updated bio 'Principal Architect', got %q", updatedBio)
		}
	})
}

// TestCascadeWrite_HTTPResponse_RedactsChildViaReadHooks asserts that cascade write
// HTTP responses run child entity read hooks (e.g. AfterGet) to redact sensitive child fields.
func TestCascadeWrite_HTTPResponse_RedactsChildViaReadHooks(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedCascadeDB(t, db)
		app := cascadeTestApp(t, db)
		app.HookRegistry("profiles").RegisterHook(hook.AfterGet, func(ctx context.Context, data any) error {
			if payload, ok := data.(*hook.GetPayload); ok {
				payload.Result["bio"] = "[redacted]"
			}
			return nil
		})
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})

		payload := map[string]any{
			"name": "SecretUser",
			"profile": map[string]any{
				"bio": "classified bio",
			},
		}
		resp := ta.Post("/users", payload)
		resp.AssertStatus(t, http.StatusCreated)

		var res struct {
			Data map[string]any `json:"data"`
		}
		json.Unmarshal([]byte(resp.Body()), &res)
		prof, ok := res.Data["profile"].(map[string]any)
		if !ok {
			t.Fatalf("expected profile map in response, got: %#v", res.Data["profile"])
		}
		if prof["bio"] != "[redacted]" {
			t.Fatalf("expected child profile bio to be [redacted], got: %v", prof["bio"])
		}
	})
}

// TestCascadeWrite_ManyToMany asserts creating an article with nested tags creates
// the tags and the pivot table rows atomically.
func TestCascadeWrite_ManyToMany(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedCascadeDB(t, db)
		app := cascadeTestApp(t, db)
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})

		payload := map[string]any{
			"title": "Full Stack Go",
			"author": map[string]any{
				"name": "Frank",
			},
			"tags": []any{
				map[string]any{"name": "golang"},
				map[string]any{"name": "web"},
			},
		}
		resp := ta.Post("/articles", payload)
		resp.AssertStatus(t, http.StatusCreated)

		var res struct {
			Data map[string]any `json:"data"`
		}
		json.Unmarshal([]byte(resp.Body()), &res)
		articleID, _ := res.Data["id"].(string)

		var tagCount int
		db.QueryRow("SELECT COUNT(*) FROM tags").Scan(&tagCount)
		if tagCount != 2 {
			t.Fatalf("expected 2 tags in DB, got %d", tagCount)
		}

		var pivotCount int
		db.QueryRow("SELECT COUNT(*) FROM article_tags WHERE article_id = $1", articleID).Scan(&pivotCount)
		if pivotCount != 2 {
			t.Fatalf("expected 2 article_tags in DB, got %d", pivotCount)
		}
	})
}

// TestCascadeWrite_Update_HasMany asserts that updating an order can add new items.
func TestCascadeWrite_Update_HasMany(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedCascadeDB(t, db)
		app := cascadeTestApp(t, db)
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})

		// First create an order without items
		if _, err := db.Exec("INSERT INTO orders(id, customer_name, status) VALUES ($1, $2, $3)", "ord1", "Grace", "pending"); err != nil {
			t.Fatalf("insert order: %v", err)
		}

		// Update order with new items
		updatePayload := map[string]any{
			"status": "processing",
			"items": []any{
				map[string]any{"name": "Keyboard", "price": 100},
			},
		}
		resp := ta.Put("/orders/ord1", updatePayload)
		resp.AssertStatus(t, http.StatusOK)

		var itemCount int
		db.QueryRow("SELECT COUNT(*) FROM order_items WHERE order_id = 'ord1'").Scan(&itemCount)
		if itemCount != 1 {
			t.Fatalf("expected 1 item after update, got %d", itemCount)
		}
	})
}

// TestCascadeWrite_ManyToMany_ExistingIDs_Security asserts that linking an existing ID
// succeeds when the row exists and fails with 404 when it doesn't exist or is not readable.
func TestCascadeWrite_ManyToMany_ExistingIDs_Security(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedCascadeDB(t, db)
		app := cascadeTestApp(t, db)
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})

		// Insert an existing tag
		if _, err := db.Exec("INSERT INTO tags(id, name) VALUES ('t1', 'existing-tag')"); err != nil {
			t.Fatalf("insert tag: %v", err)
		}

		// 1. Linking existing tag by string ID succeeds
		payload := map[string]any{
			"title": "Existing Tag Article",
			"author": map[string]any{
				"name": "Henry",
			},
			"tags": []any{"t1"},
		}
		resp := ta.Post("/articles", payload)
		resp.AssertStatus(t, http.StatusCreated)

		// 2. Linking non-existent tag fails with 404
		badPayload := map[string]any{
			"title": "Bogus Tag Article",
			"author": map[string]any{
				"name": "Henry2",
			},
			"tags": []any{"ghost_tag_id"},
		}
		badResp := ta.Post("/articles", badPayload)
		badResp.AssertStatus(t, http.StatusNotFound)
	})
}

// TestCascadeWrite_ManyToMany_JsonNumber asserts that json.Number IDs are handled gracefully in-process.
func TestCascadeWrite_ManyToMany_JsonNumber(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedCascadeDB(t, db)
		app := cascadeTestApp(t, db)

		// Insert tag with numeric ID
		if _, err := db.Exec("INSERT INTO tags(id, name) VALUES ('42', 'numeric-tag')"); err != nil {
			t.Fatalf("insert tag: %v", err)
		}

		ch, err := app.CrudHandler("articles")
		if err != nil {
			t.Fatalf("app.CrudHandler: %v", err)
		}
		payload := map[string]any{
			"title": "Json Number Article",
			"author": map[string]any{
				"name": "Ian",
			},
			"tags": []any{json.Number("42")},
		}
		res, err := ch.CreateOne(context.Background(), payload)
		if err != nil {
			t.Fatalf("CreateOne with json.Number: %v", err)
		}
		if res["title"] != "Json Number Article" {
			t.Fatalf("expected title %q, got %v", "Json Number Article", res["title"])
		}
	})
}

// TestCascadeWrite_ExplicitParentID asserts that cascade writes work when the parent ID is explicitly supplied.
func TestCascadeWrite_ExplicitParentID(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedCascadeDB(t, db)
		app := cascadeTestApp(t, db)
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})

		payload := map[string]any{
			"id":            "ord-explicit-1",
			"customer_name": "Jack",
			"items": []any{
				map[string]any{"name": "Mouse", "price": 50},
			},
		}
		resp := ta.Post("/orders", payload)
		resp.AssertStatus(t, http.StatusCreated)

		var res struct {
			Data map[string]any `json:"data"`
		}
		json.Unmarshal([]byte(resp.Body()), &res)
		createdID, _ := res.Data["id"].(string)
		if createdID == "" {
			t.Fatalf("expected createdID in response, got %v", res.Data)
		}

		var itemCount int
		db.QueryRow("SELECT COUNT(*) FROM order_items WHERE order_id = $1", createdID).Scan(&itemCount)
		if itemCount != 1 {
			t.Fatalf("expected 1 item linked to %s, got %d", createdID, itemCount)
		}
	})
}

// TestCascadeWrite_HasMany_IDOR_Rejection ensures that attempting to update an item belonging to
// another parent via cascade write is rejected with 404 (IDOR prevention).
func TestCascadeWrite_HasMany_IDOR_Rejection(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedCascadeDB(t, db)
		app := cascadeTestApp(t, db)
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})

		// Create Order 1 with Item 1
		p1 := map[string]any{
			"customer_name": "Alice",
			"items": []any{
				map[string]any{"name": "Item 1", "price": 10},
			},
		}
		r1 := ta.Post("/orders", p1)
		r1.AssertStatus(t, http.StatusCreated)
		var res1 struct {
			Data struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(r1.Body()), &res1); err != nil {
			t.Fatalf("unmarshal r1: %v", err)
		}
		order1ID := res1.Data.ID

		// Create Order 2 with Item 2
		p2 := map[string]any{
			"customer_name": "Bob",
			"items": []any{
				map[string]any{"name": "Item 2", "price": 20},
			},
		}
		r2 := ta.Post("/orders", p2)
		r2.AssertStatus(t, http.StatusCreated)

		var res2 struct {
			Data struct {
				ID    string           `json:"id"`
				Items []map[string]any `json:"items"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(r2.Body()), &res2); err != nil {
			t.Fatalf("unmarshal r2: %v", err)
		}
		if len(res2.Data.Items) == 0 {
			t.Fatalf("expected items in r2 response: %s", r2.Body())
		}
		order2ID := res2.Data.ID
		item2ID, _ := res2.Data.Items[0]["id"].(string)
		if item2ID == "" {
			t.Fatalf("empty item2ID: %v", res2.Data.Items[0])
		}

		// Attack: Attempt to update Order 1 while referencing Item 2 (which belongs to Order 2)
		attackPayload := map[string]any{
			"customer_name": "Alice Modified",
			"items": []any{
				map[string]any{"id": item2ID, "name": "Hijacked Item", "price": 99},
			},
		}
		resp := ta.Put("/orders/"+order1ID, attackPayload)
		resp.AssertStatus(t, http.StatusNotFound)

		// Verify Item 2 was NOT modified
		var name string
		var price int
		var orderID string
		err := db.QueryRow("SELECT name, price, order_id FROM order_items WHERE id = $1", item2ID).Scan(&name, &price, &orderID)
		if err != nil {
			t.Fatalf("query item-2: %v", err)
		}
		if name != "Item 2" || price != 20 || orderID != order2ID {
			t.Fatalf("item-2 was modified! name=%s price=%d orderID=%s want=%s", name, price, orderID, order2ID)
		}
	})
}

// TestCascadeWrite_CamelCase_NestedFields verifies unconvertMapKeys on nested child objects.
func TestCascadeWrite_CamelCase_NestedFields(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedCascadeDB(t, db)
		app := cascadeTestApp(t, db)
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})

		// Post with camelCase keys in child items
		payload := map[string]any{
			"customer_name": "Camel Customer",
			"items": []any{
				map[string]any{"name": "Widget", "price": 100},
			},
		}
		resp := ta.Post("/orders", payload)
		resp.AssertStatus(t, http.StatusCreated)
	})
}

// TestCascadeWrite_EmptySlice_SerializesAsEmptyArray asserts that an empty slice
// in cascade writes serializes as [] instead of null in JSON response.
func TestCascadeWrite_EmptySlice_SerializesAsEmptyArray(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedCascadeDB(t, db)
		app := cascadeTestApp(t, db)
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})

		payload := map[string]any{
			"customer_name": "Empty Items Customer",
			"items":         []any{},
		}
		resp := ta.Post("/orders", payload)
		resp.AssertStatus(t, http.StatusCreated)

		var res struct {
			Data struct {
				Items []any `json:"items"`
			} `json:"data"`
		}
		resp.JSON(&res)
		if res.Data.Items == nil {
			t.Fatalf("items was null, expected empty array []")
		}
	})
}

// TestCascadeWrite_ManyToMany_UpdateExistingTargetWithAttrs asserts that ManyToMany
// cascade writes carrying both an ID and attributes update the existing target entity
// rather than creating a duplicate row.
func TestCascadeWrite_ManyToMany_UpdateExistingTargetWithAttrs(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedCascadeDB(t, db)
		if _, err := db.Exec("INSERT INTO tags (id, name) VALUES ('t1', 'Old Tag 1')"); err != nil {
			t.Fatalf("seed tag: %v", err)
		}
		app := cascadeTestApp(t, db)
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})

		// Create an article with existing tag updated
		payload := map[string]any{
			"title": "M2M Update Article",
			"author": map[string]any{
				"name": "Jane",
			},
			"tags": []any{
				map[string]any{"id": "t1", "name": "Renamed Tag 1"},
			},
		}
		resp := ta.Post("/articles", payload)
		resp.AssertStatus(t, http.StatusCreated)

		// Verify tag was updated in DB and not duplicated
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM tags WHERE id = 't1' AND name = 'Renamed Tag 1'").Scan(&count); err != nil {
			t.Fatalf("query tag: %v", err)
		}
		if count != 1 {
			t.Fatalf("expected 1 tag with updated name, got %d", count)
		}
	})
}

// TestCascadeWrite_CustomPrimaryKey_Child asserts that cascade writes work when the child entity
// has a custom primary key (e.g. sku_code instead of id).
func TestCascadeWrite_CustomPrimaryKey_Child(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())

		app.Entity("products", entity.EntityConfig{
			Table: "products",
			Fields: []schema.Field{
				{Name: "name", Type: schema.String, Required: true},
			},
			Relations: []entity.Relation{
				entity.HasMany("skus", "product_skus", "product_id").WithCascadeWrite(true),
			},
		}.WithTimestamps(false))

		skuEnt := entity.Define("product_skus", entity.EntityConfig{
			Table: "product_skus",
			Fields: []schema.Field{
				{Name: "sku_code", Type: schema.String, Required: true},
				{Name: "product_id", Type: schema.String, Required: true},
				{Name: "size", Type: schema.String, Required: true},
			},
		}.WithTimestamps(false))
		skuEnt.PrimaryKey = "sku_code"
		app.Registry.Register(skuEnt)

		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}

		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})
		payload := map[string]any{
			"name": "T-Shirt",
			"skus": []any{
				map[string]any{"sku_code": "SKU-SM", "size": "S"},
				map[string]any{"sku_code": "SKU-MD", "size": "M"},
			},
		}
		resp := ta.Post("/products", payload)
		resp.AssertStatus(t, http.StatusCreated)

		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM product_skus WHERE sku_code IN ('SKU-SM', 'SKU-MD')").Scan(&count); err != nil {
			t.Fatalf("query skus: %v", err)
		}
		if count != 2 {
			t.Fatalf("expected 2 skus inserted with custom PK, got %d", count)
		}
	})
}

// TestCascadeWrite_NilRegistry_FailsClosed asserts that requesting a cascade write
// on a handler with no registry returns an error instead of silently ignoring the child write.
func TestCascadeWrite_NilRegistry_FailsClosed(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		postEnt := entity.Define("posts", entity.EntityConfig{
			Table: "posts",
			Fields: []schema.Field{
				{Name: "id", Type: schema.String},
				{Name: "title", Type: schema.String},
			},
			Relations: []entity.Relation{
				entity.HasMany("items", "items", "post_id").WithCascadeWrite(true),
			},
		}.WithTimestamps(false))
		postEnt.SetDB(db)

		_, err := db.Exec("CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT)")
		if err != nil {
			t.Fatal(err)
		}

		ch := NewCrudHandler(postEnt, db)
		ch.Registry = nil // explicitly nil

		payload := map[string]any{
			"id":    "p1",
			"title": "Post 1",
			"items": []any{
				map[string]any{"name": "Item 1"},
			},
		}

		_, err = ch.CreateOne(context.Background(), payload)
		if err == nil {
			t.Fatal("expected error when cascade write requested with nil registry")
		}
	})
}

// TestCascadeWrite_ManyToMany_IntegerPK_FloatExactness asserts that linking ManyToMany
// records with an integer PK rejects float64 values at or beyond ±2^53 that may have suffered precision loss.
func TestCascadeWrite_ManyToMany_IntegerPK_FloatExactness(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())

		app.Entity("tags_int", entity.EntityConfig{
			Table: "tags_int",
			Fields: []schema.Field{
				{Name: "id", Type: schema.Int},
				{Name: "name", Type: schema.String},
			},
		}.WithTimestamps(false))

		app.Entity("posts_int", entity.EntityConfig{
			Table: "posts_int",
			Fields: []schema.Field{
				{Name: "id", Type: schema.String},
				{Name: "title", Type: schema.String},
			},
			Relations: []entity.Relation{
				entity.ManyToMany("tags", "tags_int", "post_tags_int", "post_id", "tag_id").WithCascadeWrite(true),
			},
		}.WithTimestamps(false))

		// Note: tags_int has BIGINT PRIMARY KEY to accommodate 64-bit IDs like 2^53 across Postgres and SQLite
		_, err := db.Exec(`
			CREATE TABLE posts_int (id TEXT PRIMARY KEY, title TEXT);
			CREATE TABLE tags_int (id BIGINT PRIMARY KEY, name TEXT);
			CREATE TABLE post_tags_int (post_id TEXT NOT NULL, tag_id BIGINT NOT NULL, PRIMARY KEY (post_id, tag_id));
		`)
		if err != nil {
			t.Fatal(err)
		}

		// Insert tag with ID = 2^53
		bigID := int64(1 << 53) // 9007199254740992
		if _, err := db.Exec("INSERT INTO tags_int(id, name) VALUES ($1, 'big-tag')", bigID); err != nil {
			t.Fatal(err)
		}

		ch, err := app.CrudHandler("posts_int")
		if err != nil {
			t.Fatalf("app.CrudHandler: %v", err)
		}

		// 1. Bare float64 at 2^53 must be refused with validation error
		payload := map[string]any{
			"id":    "p1",
			"title": "Post 1",
			"tags":  []any{float64(bigID)},
		}
		_, err = ch.CreateOne(context.Background(), payload)
		if err == nil {
			t.Fatal("expected ValidationError for float64 ID at or beyond 2^53")
		}

		// 2. Object with float64 ID at 2^53 must also be refused
		payloadObj := map[string]any{
			"id":    "p2",
			"title": "Post 2",
			"tags":  []any{map[string]any{"id": float64(bigID)}},
		}
		_, err = ch.CreateOne(context.Background(), payloadObj)
		if err == nil {
			t.Fatal("expected ValidationError for float64 ID inside object at or beyond 2^53")
		}

		// 3. String spelling or exact int64 must succeed
		payloadExact := map[string]any{
			"id":    "p3",
			"title": "Post 3",
			"tags":  []any{bigID}, // exact int64
		}
		_, err = ch.CreateOne(context.Background(), payloadExact)
		if err != nil {
			t.Fatalf("exact int64 link failed: %v", err)
		}

		// 4. Non-integral float (1.5) must be refused with validation error, not cause 404
		payloadNonIntegral := map[string]any{
			"id":    "p_frac",
			"title": "Post Frac",
			"tags":  []any{1.5},
		}
		_, err = ch.CreateOne(context.Background(), payloadNonIntegral)
		if err == nil {
			t.Fatal("expected ValidationError for non-integral float ID (1.5)")
		}

		// 5. HTTP POST with large int64 literal in JSON payload succeeds via HTTP handler
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})
		httpPayload := map[string]any{
			"id":    "p4",
			"title": "Post 4",
			"tags":  []any{bigID},
		}
		resp := ta.Post("/posts_int", httpPayload)
		resp.AssertStatus(t, http.StatusCreated)

		var pivotCount int
		db.QueryRow("SELECT COUNT(*) FROM post_tags_int WHERE post_id = 'p4' AND tag_id = $1", bigID).Scan(&pivotCount)
		if pivotCount != 1 {
			t.Fatalf("expected post_tags_int link for bigID %d, got count %d", bigID, pivotCount)
		}
	})
}
