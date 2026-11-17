package processmoduletest

// Local copies of framework-root test helpers that wrap root types, so they
// cannot move to framework/internal/pmtestkit. Keep these in sync with:
//
//	newTestStore / newStoreDB   framework/processmodule_test.go
//	broker* helpers             framework/processmodule_broker_test.go

import (
	"context"
	"database/sql"
	"fmt"
	"sync/atomic"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// ---- store (framework/processmodule_test.go) ----

// storeDBCounter makes each in-memory DSN unique so concurrent tests don't
// collide on the shared-cache DB name.
var storeDBCounter atomic.Uint64

// newStoreDB opens a SQLite DB suitable for supervisor tests: shared-cache
// in-memory so every connection from the *sql.DB pool sees the same schema
// and rows (the default ":memory:" gives each connection its own private
// DB, which breaks the supervisor's pooled reads).
func newStoreDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:pmstore%d?mode=memory&cache=shared", storeDBCounter.Add(1))
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Skipf("sqlite3 driver not available: %v", err)
	}
	// Single shared connection: required for cache=shared to be observable
	// across the pool.
	db.SetMaxOpenConns(1)
	return db
}

// newTestStore constructs a SQLite-backed store with EnsureSchema applied.
func newTestStore(t *testing.T) *framework.SQLProcessModuleStore {
	t.Helper()
	db := newStoreDB(t)
	store, err := framework.NewSQLProcessModuleStore(db)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return store
}

// ---- capability-broker helpers (framework/processmodule_broker_test.go) ----

func brokerSetupDB(t *testing.T, ddl string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(ddl); err != nil {
		t.Fatal(err)
	}
	return db
}

func brokerSeedRow(t *testing.T, db *sql.DB, table, id, ownerID, subject string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO `+table+` (id, user_id, subject) VALUES (?, ?, ?)`,
		id, ownerID, subject); err != nil {
		t.Fatal(err)
	}
}

func brokerEntity(name, table string, configure func(*entity.EntityConfig)) *entity.Entity {
	cfg := entity.EntityConfig{Table: table,
		Fields: []schema.Field{{Name: "user_id", Type: schema.String}, {Name: "subject", Type: schema.String}},
		// All three groups materialized so configure callbacks can write
		// through them before Define normalizes.
		Scope:      &entity.ScopeConfig{OwnerField: "user_id"},
		Pagination: &entity.PaginationConfig{},
		Exposure:   &entity.ExposureConfig{},
	}
	if configure != nil {
		configure(&cfg)
	}
	return entity.Define(name, cfg.WithTimestamps(false))
}

func brokerRegistry(ents ...*entity.Entity) *framework.Registry {
	r := framework.NewRegistry()
	for _, e := range ents {
		if err := r.Register(e); err != nil {
			panic(err)
		}
	}
	return r
}
