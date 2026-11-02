package crud

import (
	"context"
	"database/sql"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// The in-process CRUD paths name a keyword column the way the migrator's
// DDL does (query.QuoteKeyword): a state transition on a column named
// order, the aggregates, an upsert (column list, ON CONFLICT target,
// EXCLUDED sets, the owner guard on a column named user), batch writes,
// restore and purge. framework's TestKeywordColumnsCRUD covers the HTTP
// routes.
func TestKeywordColumnsInProcess(t *testing.T) {
	run := func(t *testing.T, conn *sql.DB) {
		ent := entity.Define("kwtickets", entity.EntityConfig{
			Name:       "kwtickets",
			Table:      "kwtickets",
			PrimaryKey: []string{"select"},
			Fields: []schema.Field{
				{Name: "select", Type: schema.String},
				{Name: "order", Type: schema.Enum, Default: "open", Values: []string{"open", "closed"}},
				{Name: "group", Type: schema.String},
				{Name: "limit", Type: schema.Int},
			},
			States: &entity.StatesConfig{
				Field:       "order",
				Initial:     []string{"open"},
				Transitions: []entity.Transition{{Key: "close", From: []string{"open"}, To: "closed"}},
			},
			Scope:    &entity.ScopeConfig{SoftDelete: true},
			Exposure: &entity.ExposureConfig{Public: true},
		}.WithTimestamps(false))
		if err := migrate.MigrateEntity(conn, ent); err != nil {
			t.Fatalf("MigrateEntity: %v", err)
		}
		ent.SetDB(conn)
		ch := NewCrudHandler(ent, conn).WithJSONCase(CaseSnake)
		ctx := WithReadHooks(context.Background())

		for _, b := range []map[string]any{
			{"select": "a", "group": "g1", "limit": 2},
			{"select": "b", "group": "g1", "limit": 3},
		} {
			if _, err := ch.CreateOne(ctx, b); err != nil {
				t.Fatalf("CreateOne %v: %v", b, err)
			}
		}
		if _, err := ch.BatchCreateMany(ctx, []map[string]any{{"select": "c", "group": "g2", "limit": 4}}); err != nil {
			t.Fatalf("BatchCreateMany: %v", err)
		}
		if _, err := ch.BatchUpdateMany(ctx, []string{"c"}, []map[string]any{{"limit": 5}}); err != nil {
			t.Fatalf("BatchUpdateMany: %v", err)
		}
		got, err := ch.RunTransition(ctx, "a", "close")
		if err != nil || got["order"] != "closed" {
			t.Fatalf("RunTransition on a state column named order = %v, %v", got, err)
		}
		if sum, err := ch.SumAll(ctx, "limit", ListOptions{}); err != nil || sum != "10" {
			t.Fatalf("SumAll(limit) = %q, %v; want 10", sum, err)
		}
		groups, err := ch.GroupCountAll(ctx, "group", ListOptions{}, 0)
		if want := []GroupCount{{Value: "g1", Count: 2}, {Value: "g2", Count: 1}}; err != nil || !slices.Equal(groups, want) {
			t.Fatalf("GroupCountAll(group) = %v, %v; want %v", groups, err, want)
		}

		// An upsert that hits the key "b" updates it.
		up, err := ch.UpsertOne(ctx, map[string]any{"select": "b", "group": "g9", "limit": 9})
		if err != nil || up["group"] != "g9" {
			t.Fatalf("UpsertOne on the keyword key = %v, %v", up, err)
		}
		if n, err := ch.CountAll(ctx, ListOptions{}); err != nil || n != 3 {
			t.Fatalf("CountAll = %d, %v; want 3", n, err)
		}

		if err := ch.DeleteOne(ctx, "c"); err != nil {
			t.Fatalf("DeleteOne: %v", err)
		}
		if err := ch.RestoreOne(ctx, "c"); err != nil {
			t.Fatalf("RestoreOne: %v", err)
		}
		if err := ch.DeleteOne(ctx, "c"); err != nil {
			t.Fatalf("DeleteOne: %v", err)
		}
		if err := ch.PurgeOne(ctx, "c"); err != nil {
			t.Fatalf("PurgeOne: %v", err)
		}
		if _, err := ch.BatchDeleteMany(ctx, []string{"a"}); err != nil {
			t.Fatalf("BatchDeleteMany: %v", err)
		}
		rows, err := ch.ListAll(ctx, ListOptions{})
		if err != nil || len(rows) != 1 || rows[0]["select"] != "b" {
			t.Fatalf("ListAll after deletes = %v, %v; want only b", rows, err)
		}
	}
	t.Run("sqlite", func(t *testing.T) { run(t, setupDB(t)) })
	t.Run("postgres", func(t *testing.T) { run(t, pgtest.DB(t)) })
}
