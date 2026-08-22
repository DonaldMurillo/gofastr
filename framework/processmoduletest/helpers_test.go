package processmoduletest

// Local copies of framework-root test helpers. The suites in this package
// moved out of the framework root for the CI race gate (issue #208), but
// the root's helpers are unexported test-package functions that cannot be
// imported, and several of them (sha256OfFile, the sandbox probe dispatch)
// wrap unexported root API. Re-implementing the small ones here is cheaper
// than exporting test seams from the framework's public API. Keep these in
// sync with their originals:
//
//	newTestStore / newStoreDB   framework/processmodule_test.go
//	sha256OfFile                framework/processmodule_runner.go
//	testExecutablePath          framework/processmodule_test_exec_*_test.go
//	broker* helpers             framework/processmodule_broker_test.go

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/owner"
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

// ---- hashing + exec path (framework/processmodule_runner.go and the
// per-OS test-exec helpers). Runtime check instead of build tags: the
// behavior is two lines and only Windows differs. ----

func sha256OfFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func testExecutablePath(path string) string {
	if runtime.GOOS == "windows" && filepath.Ext(path) == "" {
		return path + ".exe"
	}
	return path
}

// ---- capability-broker helpers (framework/processmodule_broker_test.go) ----

type brokerTestUser struct{ id string }

func (u *brokerTestUser) GetID() string { return u.id }

func brokerInstallOwnerExtractor(t *testing.T) {
	t.Helper()
	prev := owner.GetExtractor()
	owner.SetExtractor(func(ctx context.Context) (any, bool) {
		raw, ok := handler.GetUser(ctx)
		if !ok || raw == nil {
			return nil, false
		}
		if u, ok := raw.(*brokerTestUser); ok {
			return u.GetID(), true
		}
		return nil, false
	})
	t.Cleanup(func() { owner.SetExtractor(prev) })
}

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

// brokerAuthMiddleware mirrors what battery/auth installs: policy always,
// plus the caller's user id + roles resolved from the re-injected session
// cookie. The broker re-injects Cookie via the delegation snapshot, so this
// is where the caller-authority half of module-grant ∩ caller-authority
// resolves.
func brokerAuthMiddleware(policy *access.RolePolicy, rolesFor func(string) []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			if policy != nil {
				ctx = access.WithPolicy(ctx, policy)
			}
			if c := r.Header.Get("Cookie"); strings.HasPrefix(c, "sid=") {
				sid := strings.TrimPrefix(c, "sid=")
				ctx = handler.SetUser(ctx, &brokerTestUser{id: sid})
				if rolesFor != nil {
					if roles := rolesFor(sid); len(roles) > 0 {
						ctx = access.WithRoles(ctx, roles)
					}
				}
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
