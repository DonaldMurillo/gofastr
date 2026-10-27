package crud

import (
	"context"
	"database/sql"
	"net/http"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/db"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// AutoMigrate writes its DDL unquoted, so Postgres stores a declared
// table "Tickets" and column "Status" folded (tickets, status). A
// statement that quotes the DECLARED spelling names an object that does
// not exist there. Each test below declares mixed-case names, creates the
// table the way AutoMigrate does, and drives a public path that reaches a
// quoted site, on SQLite and on Postgres.

// mixedTickets declares the mixed-case entity the tests share.
func mixedTickets() *entity.Entity {
	return entity.Define("Tickets", entity.EntityConfig{
		Name:  "Tickets",
		Table: "Tickets",
		Fields: []schema.Field{
			{Name: "Title", Type: schema.String},
			{Name: "Status", Type: schema.Enum, Default: "open", Values: []string{"open", "closed"}},
			{Name: "Amount", Type: schema.Int},
		},
		States: &entity.StatesConfig{
			Field:   "Status",
			Initial: []string{"open"},
			Transitions: []entity.Transition{
				{Key: "close", From: []string{"open"}, To: "closed"},
			},
		},
		Exposure: &entity.ExposureConfig{Public: true},
	}.WithTimestamps(false))
}

// forMixedCase runs fn with a handler over the migrated mixed-case table,
// once per dialect.
func forMixedCase(t *testing.T, fn func(t *testing.T, ch *CrudHandler, conn *sql.DB)) {
	run := func(t *testing.T, conn *sql.DB) {
		ent := mixedTickets()
		if err := migrate.MigrateEntity(conn, ent); err != nil {
			t.Fatalf("MigrateEntity: %v", err)
		}
		ent.SetDB(conn)
		fn(t, NewCrudHandler(ent, conn), conn)
	}
	t.Run("sqlite", func(t *testing.T) { run(t, setupDB(t)) })
	t.Run("postgres", func(t *testing.T) { run(t, pgtest.DB(t)) })
}

func TestMixedCaseTransition(t *testing.T) {
	forMixedCase(t, func(t *testing.T, ch *CrudHandler, _ *sql.DB) {
		ctx := context.Background()
		row, err := ch.CreateOne(ctx, map[string]any{"Title": "a", "Amount": 1})
		if err != nil {
			t.Fatalf("CreateOne: %v", err)
		}
		id, _ := row["id"].(string)
		got, err := ch.RunTransition(ctx, id, "close")
		if err != nil {
			t.Fatalf("RunTransition on a mixed-case table: %v", err)
		}
		if got[ch.convertKey("Status")] != "closed" {
			t.Fatalf("status after close = %v, want closed (row %v)", got[ch.convertKey("Status")], got)
		}
	})
}

func TestMixedCaseAggregates(t *testing.T) {
	forMixedCase(t, func(t *testing.T, ch *CrudHandler, _ *sql.DB) {
		ctx := WithReadHooks(context.Background())
		for _, amount := range []int{2, 3} {
			if _, err := ch.CreateOne(ctx, map[string]any{"Title": "a", "Amount": amount}); err != nil {
				t.Fatalf("CreateOne: %v", err)
			}
		}
		if sum, err := ch.SumAll(ctx, "Amount", ListOptions{}); err != nil || sum != "5" {
			t.Fatalf("SumAll = %q, %v; want 5", sum, err)
		}
		groups, err := ch.GroupCountAll(ctx, "Status", ListOptions{}, 0)
		if want := []GroupCount{{Value: "open", Count: 2}}; err != nil || !slices.Equal(groups, want) {
			t.Fatalf("GroupCountAll = %v, %v; want %v", groups, err, want)
		}
	})
}

// A write joined to a caller-owned transaction confirms its event against
// the table once the transaction ends; that confirm query names the
// declared table and columns. Snake wire keys keep the declared column
// names, so the confirm query also matches on the updated Title.
func TestMixedCaseAmbientTxEvent(t *testing.T) {
	forMixedCase(t, func(t *testing.T, ch *CrudHandler, conn *sql.DB) {
		ch = ch.WithJSONCase(CaseSnake)
		seed, err := ch.CreateOne(context.Background(), map[string]any{"Title": "a", "Amount": 1})
		if err != nil {
			t.Fatalf("seed CreateOne: %v", err)
		}
		bus := event.NewEventBus()
		ch.Events = bus
		got := make(chan event.Event, 4)
		cancel := bus.Subscribe(event.EntityUpdated, func(_ context.Context, ev event.Event) error {
			got <- ev
			return nil
		})
		defer cancel()

		tx, err := conn.Begin()
		if err != nil {
			t.Fatal(err)
		}
		ctx := db.WithTx(context.Background(), tx)
		if _, err := ch.UpdateOne(ctx, seed["id"].(string), map[string]any{"Title": "b"}); err != nil {
			t.Fatalf("UpdateOne: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		select {
		case <-got:
		case <-time.After(3 * time.Second):
			t.Fatal("EntityUpdated not delivered after the ambient tx committed")
		}
	})
}

// The confirm query also names a declared mixed-case primary key.
func TestMixedCaseAmbientTxKey(t *testing.T) {
	run := func(t *testing.T, conn *sql.DB) {
		ent := entity.Define("Codes", entity.EntityConfig{
			Table:      "Codes",
			PrimaryKey: []string{"CodeKey"},
			Fields: []schema.Field{
				{Name: "CodeKey", Type: schema.String},
				{Name: "Label", Type: schema.String},
			},
		}.WithTimestamps(false))
		if err := migrate.MigrateEntity(conn, ent); err != nil {
			t.Fatalf("MigrateEntity: %v", err)
		}
		ent.SetDB(conn)
		ch := NewCrudHandler(ent, conn).WithJSONCase(CaseSnake)
		bus := event.NewEventBus()
		ch.Events = bus
		got := make(chan event.Event, 4)
		cancel := bus.Subscribe(event.EntityCreated, func(_ context.Context, ev event.Event) error {
			got <- ev
			return nil
		})
		defer cancel()
		tx, err := conn.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ch.CreateOne(db.WithTx(context.Background(), tx), map[string]any{"CodeKey": "k1", "Label": "a"}); err != nil {
			t.Fatalf("CreateOne: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		select {
		case <-got:
		case <-time.After(3 * time.Second):
			t.Fatal("EntityCreated not delivered after the ambient tx committed")
		}
	}
	t.Run("sqlite", func(t *testing.T) { run(t, setupDB(t)) })
	t.Run("postgres", func(t *testing.T) { run(t, pgtest.DB(t)) })
}

// Postgres reports a folded table's constraints in the folded spelling
// (members_email_key, column "teamid"), so the 409 still names the field
// the caller sent.
func TestMixedCaseConflictNamesField(t *testing.T) {
	run := func(t *testing.T, conn *sql.DB) {
		ent := entity.Define("Members", entity.EntityConfig{
			Name: "Members", Table: "Members",
			Fields:   []schema.Field{{Name: "Email", Type: schema.String, Unique: true}},
			Exposure: &entity.ExposureConfig{Public: true},
		}.WithTimestamps(false))
		if err := migrate.MigrateEntity(conn, ent); err != nil {
			t.Fatalf("MigrateEntity: %v", err)
		}
		ent.SetDB(conn)
		code, env := conflictCreate(t, NewCrudHandler(ent, conn).WithJSONCase(CaseSnake), `{"Email":"a@b.com"}`)
		if code != http.StatusConflict {
			t.Fatalf("dup create = %d, want 409", code)
		}
		if got := env["fields"]; !reflect.DeepEqual(got, map[string]any{"Email": []any{"is already in use"}}) {
			t.Errorf("fields = %v, want Email named", got)
		}
	}
	t.Run("sqlite", func(t *testing.T) { run(t, setupDB(t)) })
	t.Run("postgres", func(t *testing.T) { run(t, pgtest.DB(t)) })
}

func TestMixedCaseConstraintMessages(t *testing.T) {
	ent := entity.Define("Members", entity.EntityConfig{
		Name: "Members", Table: "Members",
		Fields: []schema.Field{
			{Name: "Email", Type: schema.String, Unique: true},
			{Name: "TeamID", Type: schema.Relation, To: "teams"},
		},
		Indices: []entity.Index{{Columns: []string{"Email", "TeamID"}, Unique: true}},
	}.WithTimestamps(false))
	for msg, want := range map[string][]string{
		`ERROR: duplicate key value violates unique constraint "members_email_key" (SQLSTATE 23505)`:                        {"Email"},
		`ERROR: duplicate key value violates unique constraint "idx_members_email_teamid" (SQLSTATE 23505)`:                 {"Email", "TeamID"},
		`ERROR: insert or update on table "members" violates foreign key constraint "members_teamid_fkey" (SQLSTATE 23503)`: {"TeamID"},
		`ERROR: insert or update on table "others" violates foreign key constraint "others_teamid_fkey" (SQLSTATE 23503)`:   nil,
	} {
		if got := conflictColumns(ent, msg); !reflect.DeepEqual(got, want) {
			t.Errorf("%s\n got %v, want %v", msg, got, want)
		}
	}
	if got := notNullColumn("Members", `ERROR: null value in column "email" of relation "members" violates not-null constraint (SQLSTATE 23502)`); got != "email" {
		t.Errorf("notNullColumn = %q, want email", got)
	}
	if f, ok := fieldNamed(ent, "email"); !ok || f.Name != "Email" {
		t.Errorf("fieldNamed(email) = %q, %v, want the declared Email", f.Name, ok)
	}
}

// Under camel wire keys a rolled-back update of a mixed-case column must
// not be announced: the confirm query matches the row on the announced
// Title, which the row no longer holds.
func TestMixedCaseCamelRollbackSilent(t *testing.T) {
	forMixedCase(t, func(t *testing.T, ch *CrudHandler, conn *sql.DB) {
		seed, err := ch.CreateOne(context.Background(), map[string]any{"Title": "a", "Amount": 1})
		if err != nil {
			t.Fatalf("seed CreateOne: %v", err)
		}
		bus := event.NewEventBus()
		ch.Events = bus
		got := make(chan event.Event, 4)
		cancel := bus.Subscribe(event.EntityUpdated, func(_ context.Context, ev event.Event) error {
			got <- ev
			return nil
		})
		defer cancel()

		tx, err := conn.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ch.UpdateOne(db.WithTx(context.Background(), tx), seed["id"].(string), map[string]any{"Title": "b"}); err != nil {
			t.Fatalf("UpdateOne: %v", err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		select {
		case ev := <-got:
			t.Fatalf("EntityUpdated delivered for a rolled-back update: %v", ev.Data)
		case <-time.After(time.Second):
		}
	})
}

// A WireName or camel-cased key maps back to its declared column.
func TestComparableColumnsByWireKey(t *testing.T) {
	ent := entity.Define("Tickets", entity.EntityConfig{
		Table: "Tickets",
		Fields: []schema.Field{
			{Name: "Title", Type: schema.String},
			{Name: "due_on", Type: schema.String, WireName: "deadline"},
		},
	}.WithTimestamps(false))
	ch := NewCrudHandler(ent, nil)
	got := ch.comparableColumns(map[string]any{ch.WireKey("Title"): "b", "deadline": "d"})
	if want := map[string]any{"Title": "b", "due_on": "d"}; !reflect.DeepEqual(got, want) {
		t.Errorf("comparableColumns = %v, want %v", got, want)
	}
}
