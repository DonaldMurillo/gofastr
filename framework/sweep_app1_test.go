package framework

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// WithoutAutoMigrate skips the DDL but not the entity seeds, so a seeded
// entity whose table nobody migrated fails Start instead of serving.
func TestSweepApp1WithoutAutoMigrateSeededEntityFailsStart(t *testing.T) {
	db := openTestDB(t, "sqlite3")
	app := NewApp(WithDB(db), WithoutAutoMigrate(), WithoutDefaultMiddleware())
	seeded := false
	app.Entity("gadgets", entity.EntityConfig{
		Table:  "gadgets",
		Fields: []schema.Field{{Name: "name", Type: schema.String}},
		Seed: func(ctx context.Context, db *sql.DB) error {
			seeded = true
			_, err := db.ExecContext(ctx, "INSERT INTO gadgets (name) VALUES ('a')")
			return err
		},
	}.WithTimestamps(false))

	err := startAndStop(t, app)
	if !seeded {
		t.Fatal("WithoutAutoMigrate skipped the entity seed; it must still run")
	}
	if err == nil || !strings.Contains(err.Error(), "run seeds") {
		t.Fatalf("Start err = %v, want a run seeds error for the missing table", err)
	}
}

// The claim check reads the enforced budget: with RequestTimeout unset that
// is the default chain's 30s, so a 30s claim is refused.
func TestSweepApp1DefaultBudgetChecksIdempotencyClaim(t *testing.T) {
	app := NewApp(WithIdempotency(middleware.IdempotencyConfig{Principal: idemPrincipal, Store: shortClaimStore{}}))
	err := app.checkIdempotencyInFlightTTL()
	if err == nil || !strings.Contains(err.Error(), "in-flight TTL (30s)") {
		t.Fatalf("checkIdempotencyInFlightTTL = %v, want a refusal of the 30s claim against the 30s default budget", err)
	}
}

// A write deadline equal to the request budget is refused: a response
// finished at the budget would never be delivered.
func TestSweepApp1WriteTimeoutEqualToBudgetRefused(t *testing.T) {
	for name, cfg := range map[string]AppConfig{
		"explicit budget": {RequestTimeout: 2 * time.Second, HTTPServerTimeouts: HTTPServerTimeoutsConfig{WriteTimeout: new(2 * time.Second)}},
		"default budget":  {HTTPServerTimeouts: HTTPServerTimeoutsConfig{WriteTimeout: new(defaultRequestTimeout)}},
	} {
		err := NewApp(WithConfig(cfg)).checkRequestWriteTimeouts()
		if err == nil || !strings.Contains(err.Error(), "HTTPServerTimeouts.WriteTimeout") {
			t.Errorf("%s: checkRequestWriteTimeouts = %v, want a refusal naming HTTPServerTimeouts.WriteTimeout", name, err)
		}
	}
}

// A RequestTimeout raised past the default write deadline moves an unset
// WriteTimeout strictly past the budget, so the timeout middleware's own
// 504 at the budget is still written.
func TestSweepApp1RaisedWriteTimeoutOutlivesBudget(t *testing.T) {
	app := NewApp(WithConfig(AppConfig{RequestTimeout: 90 * time.Second}))
	_, stop := startOnRandomPort(t, app)
	defer stop()
	app.serverMu.Lock()
	w := app.server.WriteTimeout
	app.serverMu.Unlock()
	if w <= 90*time.Second {
		t.Fatalf("WriteTimeout = %s under a 90s RequestTimeout, want it past the budget", w)
	}
}

// The App-sized default idempotency store outlives the request budget, so
// it passes the claim check Start runs, with the budget set or defaulted.
func TestSweepApp1DefaultIdempotencyStorePassesItsOwnCheck(t *testing.T) {
	for name, cfg := range map[string]AppConfig{"explicit budget": {RequestTimeout: 90 * time.Second}, "default budget": {}} {
		app := NewApp(WithConfig(cfg), WithIdempotency(middleware.IdempotencyConfig{Principal: idemPrincipal}))
		if err := app.checkIdempotencyInFlightTTL(); err != nil {
			t.Errorf("%s: the default store fails its own claim check: %v", name, err)
		}
	}
}

// The /api/llm.md index links an entity where its routes were recorded as
// mounted, group prefix included, not at the bare /<table>.
func TestSweepApp1LLMIndexLinksGroupMount(t *testing.T) {
	db := openTestDB(t, "sqlite3")
	app := NewApp(WithDB(db), WithPublicOpenAPI())
	g := app.Group("/v1")
	app.GroupEntity(g, "widgets", entity.EntityConfig{
		Table:  "widgets",
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	}.WithTimestamps(false))
	stop := covStartAndStop(t, app)
	defer stop()
	md := TestHarness(t, app).Get("/api/llm.md").Body()
	if !strings.Contains(md, "/v1/widgets/llm.md") {
		t.Fatalf("index should link the group-mounted page /v1/widgets/llm.md:\n%s", md)
	}
}

// With ShutdownTimeout unset the signal handler drains for 15s, inside the
// 30s a Kubernetes pod gets between SIGTERM and SIGKILL.
func TestSweepApp1SignalDrainDefaultsTo15s(t *testing.T) {
	app := NewApp(WithReadinessDrain(0))
	if got := app.signalShutdownTimeout(); got != 15*time.Second {
		t.Fatalf("signalShutdownTimeout = %s with ShutdownTimeout unset, want 15s", got)
	}
}

// WithOutbox without WithDB is refused by name at NewApp, not by whatever
// outbox.New does with a nil pool.
func TestSweepApp1OutboxWithoutDBNamesWithDB(t *testing.T) {
	defer func() {
		r := recover()
		if s, _ := r.(string); !strings.Contains(s, "WithOutbox requires WithDB") {
			t.Fatalf("NewApp(WithOutbox()) panic = %v, want one naming WithDB", r)
		}
	}()
	NewApp(WithOutbox())
}

// InitPlugins runs once per App: the call inside Start after a manual one
// is a no-op, so registrars and route mounts do not run twice.
func TestSweepApp1InitPluginsRunsRegistrarsOnce(t *testing.T) {
	calls := 0
	app := NewApp(WithMCPTools(func(*mcp.Server) error { calls++; return nil }))
	for i := 0; i < 2; i++ {
		if err := app.InitPlugins(); err != nil {
			t.Fatalf("InitPlugins call %d: %v", i+1, err)
		}
	}
	if calls != 1 {
		t.Fatalf("registrar ran %d times across two InitPlugins calls, want 1", calls)
	}
}
