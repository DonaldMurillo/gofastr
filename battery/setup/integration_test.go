package setup

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	_ "github.com/mattn/go-sqlite3"
)

// setupSQLite builds an in-memory SQLite DB with the auth users table.
// Uses the canonical "auth_users" table name (matching the convention
// in blueprint.go and all examples) — NOT "users". The table name is
// host-configurable via auth.NewEntityUserStore, so tests must exercise
// the real convention to catch the exact bug that was masked by a
// hardcoded "users" default.
func setupSQLite(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	_, err = db.Exec(`CREATE TABLE auth_users (
		id TEXT PRIMARY KEY,
		email TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL DEFAULT '',
		roles TEXT DEFAULT '[]',
		password_set BOOLEAN NOT NULL DEFAULT FALSE
	)`)
	if err != nil {
		t.Fatalf("create auth_users table: %v", err)
	}
	return db
}

// setupAuthManager builds a real AuthManager against an in-memory SQLite DB.
func setupAuthManager(t *testing.T) (*auth.AuthManager, *sql.DB) {
	t.Helper()
	db := setupSQLite(t)
	mgr := auth.New(auth.AuthConfig{
		JWTSecret: "test-secret",
		UserStore: auth.NewEntityUserStore(db, "auth_users"),
		DevMode:   true,
	})
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("auth Init: %v", err)
	}
	return mgr, db
}

// ─── Wizard swap + crash re-entry ────────────────────────────────────

// TestSwap_PreSetupReturns503 verifies that a non-setup path returns 503
// while setup is incomplete.
func TestSwap_PreSetupReturns503(t *testing.T) {
	done := false
	r := buildTestRunner(t, true, &done)
	h := r.Handler(func() {}, nil, nil)

	w := doGet(h, "/some/path")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 for non-setup path, got %d", w.Code)
	}
}

// TestSwap_HealthzReturns200 verifies /healthz works during setup.
func TestSwap_HealthzReturns200(t *testing.T) {
	done := false
	r := buildTestRunner(t, true, &done)
	healthz := func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}
	h := r.Handler(func() {}, healthz, nil)

	w := doGet(h, "/healthz")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for /healthz, got %d", w.Code)
	}
}

// TestSwap_WizardCompletesAndSwaps verifies the full flow:
// pre-setup GET / → 503, POST /setup completes the step, swap is called,
// and the Complete predicate flips.
func TestSwap_WizardCompletesAndSwaps(t *testing.T) {
	var swapped int32
	done := false
	r := New(Config{
		DisableToken: true,
		Complete: func(_ context.Context) (bool, error) {
			return done, nil
		},
		Steps: []Step{
			{Name: "create", Fields: []Field{
				{Name: "NAME", Label: "Name"},
			}, Run: func(_ context.Context, vals map[string]string) error {
				done = true
				return nil
			}},
		},
	})
	h := r.Handler(func() {
		atomic.StoreInt32(&swapped, 1)
	}, nil, nil)

	// Pre-setup: non-setup path is 503.
	w := doGet(h, "/")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("pre-setup GET / expected 503, got %d", w.Code)
	}

	// GET /setup renders the form.
	w = doGet(h, "/setup")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /setup expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "create") {
		t.Fatal("wizard must render the step name")
	}

	// POST /setup submits the form → step runs → swap fires.
	w = doPost(h, "/setup", "NAME=alice", nil)
	if w.Code != http.StatusOK {
		// After final step, completion page is rendered (200), not a redirect.
		t.Fatalf("POST /setup (final step) expected 200 (completion), got %d", w.Code)
	}

	if atomic.LoadInt32(&swapped) != 1 {
		t.Fatal("swap must be called after final step completes")
	}
	if !done {
		t.Fatal("step.Run must have set done=true")
	}
}

// TestSwap_PRGRedirectBetweenSteps verifies that after a non-final step,
// the wizard redirects (PRG pattern) rather than rendering inline.
func TestSwap_PRGRedirectBetweenSteps(t *testing.T) {
	done := false
	r := New(Config{
		DisableToken: true,
		Complete: func(_ context.Context) (bool, error) {
			return done, nil
		},
		Steps: []Step{
			{Name: "first", Fields: []Field{{Name: "A", Label: "A"}}, Run: func(_ context.Context, _ map[string]string) error { return nil }},
			{Name: "second", Fields: []Field{{Name: "B", Label: "B"}}, Run: func(_ context.Context, _ map[string]string) error {
				done = true
				return nil
			}},
		},
	})
	h := r.Handler(func() {}, nil, nil)

	// POST step 1 → redirect to /setup (PRG).
	w := doPost(h, "/setup", "A=value", nil)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 PRG redirect, got %d", w.Code)
	}
}

// TestSwap_CrashMidSetupReEnters simulates a crash after step 1 of 2:
// a new Runner on the same DB re-enters setup because Complete is still
// false.
func TestSwap_CrashMidSetupReEnters(t *testing.T) {
	// Simulate: two steps, first modifies a flag but Complete is only
	// true after BOTH.
	step1Done := false
	done := false
	r := New(Config{
		DisableToken: true,
		Complete: func(_ context.Context) (bool, error) {
			return done, nil
		},
		Steps: []Step{
			{Name: "first", Fields: []Field{{Name: "A"}}, Run: func(_ context.Context, _ map[string]string) error {
				step1Done = true
				return nil
			}},
			{Name: "second", Fields: []Field{{Name: "B"}}, Run: func(_ context.Context, _ map[string]string) error {
				done = true
				return nil
			}},
		},
	})
	ctx := context.Background()

	// Simulate running only step 1 (crash before step 2).
	r.currentStep = 1 // step 1 ran
	if !step1Done {
		t.Log("step1 flag set only for realism; the crash point is currentStep=1")
	}
	incomplete, err := r.Incomplete(ctx)
	if err != nil {
		t.Fatalf("Incomplete: %v", err)
	}
	if !incomplete {
		t.Fatal("after crash mid-setup, Incomplete must be true")
	}

	// New Runner (simulating reboot) should also report incomplete.
	r2 := New(Config{
		DisableToken: true,
		Complete: func(_ context.Context) (bool, error) {
			return done, nil
		},
		Steps: r.cfg.Steps,
	})
	incomplete2, _ := r2.Incomplete(ctx)
	if !incomplete2 {
		t.Fatal("new Runner after crash must report incomplete")
	}
}

// ─── Headless boot ───────────────────────────────────────────────────

// TestHeadless_AllEnvPresent runs steps inline when all env is present.
func TestHeadless_AllEnvPresent(t *testing.T) {
	t.Setenv("HL_NAME", "alice")
	var stepRan bool
	done := false
	r := New(Config{
		Complete: func(_ context.Context) (bool, error) { return done, nil },
		Steps: []Step{
			{Name: "s1", Fields: []Field{{Name: "NAME", EnvVar: "HL_NAME"}}, Run: func(_ context.Context, vals map[string]string) error {
				if vals["NAME"] != "alice" {
					t.Fatalf("expected NAME=alice from env, got %q", vals["NAME"])
				}
				stepRan = true
				return nil
			}},
		},
	})

	can, err := r.CanRunHeadless(context.Background())
	if err != nil {
		t.Fatalf("CanRunHeadless: %v", err)
	}
	if !can {
		t.Fatal("expected headless=true")
	}
	if err := r.RunSteps(context.Background()); err != nil {
		t.Fatalf("RunSteps: %v", err)
	}
	if !stepRan {
		t.Fatal("step must have run")
	}
}

// TestHeadless_BadPasswordFails verifies that a validation error aborts.
func TestHeadless_BadPasswordFails(t *testing.T) {
	t.Setenv("HL_PW", "short") // too short for ValidatePasswordStrength
	done := false
	r := New(Config{
		Complete: func(_ context.Context) (bool, error) { return done, nil },
		Steps: []Step{
			{Name: "s1", Fields: []Field{{
				Name:   "PW",
				EnvVar: "HL_PW",
				Validate: func(v string) error {
					if len(v) < 8 {
						return io.ErrUnexpectedEOF // sentinel
					}
					return nil
				},
			}}, Run: func(_ context.Context, _ map[string]string) error { return nil }},
		},
	})
	err := r.RunSteps(context.Background())
	if err == nil {
		t.Fatal("expected validation error for short password")
	}
}

// ─── GOFASTR_SETUP env tests are in framework/setup_test.go ────────

// ─── Consumer gating ─────────────────────────────────────────────────

// TestConsumerGating_DeferredUntilSwap verifies that a consumer (fake
// queue) is NOT started during interactive setup, only after the swap.
func TestConsumerGating_DeferredUntilSwap(t *testing.T) {
	var consumerStarted int32
	done := false
	r := New(Config{
		DisableToken: true,
		Complete:     func(_ context.Context) (bool, error) { return done, nil },
		Steps: []Step{
			{Name: "s1", Fields: []Field{{Name: "X"}}, Run: func(_ context.Context, _ map[string]string) error {
				done = true
				return nil
			}},
		},
	})
	h := r.Handler(func() {
		atomic.StoreInt32(&consumerStarted, 1)
	}, nil, nil)

	// Before setup: consumer not started.
	if atomic.LoadInt32(&consumerStarted) != 0 {
		t.Fatal("consumer must not start before setup completes")
	}

	// Complete setup via POST.
	w := doPost(h, "/setup", "X=val", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 completion, got %d", w.Code)
	}

	// After swap: consumer started.
	if atomic.LoadInt32(&consumerStarted) != 1 {
		t.Fatal("consumer must start after swap")
	}
}

// ─── AdminStep against real auth battery ─────────────────────────────

// TestAdminStep_CreatesAdminWithRole verifies AdminStep creates a user
// with role "admin" and that login works via the auth manager.
func TestAdminStep_CreatesAdminWithRole(t *testing.T) {
	mgr, db := setupAuthManager(t)
	ctx := context.Background()

	step, completeFn := AdminStep(mgr, db, "auth_users")

	// Before: no users.
	done, err := completeFn(ctx)
	if err != nil {
		t.Fatalf("completeFn: %v", err)
	}
	if done {
		t.Fatal("expected no users before AdminStep runs")
	}

	// Run the step with valid values.
	err = step.Run(ctx, map[string]string{
		"ADMIN_EMAIL":    "admin@test.com",
		"ADMIN_PASSWORD": "securepass123",
	})
	if err != nil {
		t.Fatalf("AdminStep.Run: %v", err)
	}

	// After: Complete flips.
	done, err = completeFn(ctx)
	if err != nil {
		t.Fatalf("completeFn after: %v", err)
	}
	if !done {
		t.Fatal("expected Complete=true after admin creation")
	}

	// Verify the user exists with role admin and login works.
	user, hash, err := mgr.UserStore().FindByEmail(ctx, "admin@test.com")
	if err != nil {
		t.Fatalf("FindByEmail: %v", err)
	}
	roles := user.GetRoles()
	found := false
	for _, r := range roles {
		if r == "admin" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected role 'admin', got %v", roles)
	}
	// Password is hashed (bcrypt) and verifies.
	if !auth.CheckPassword("securepass123", hash) {
		t.Fatal("password hash must verify via CheckPassword")
	}
}

// TestAdminStep_WeakPasswordFails verifies password policy enforcement.
func TestAdminStep_WeakPasswordFails(t *testing.T) {
	mgr, db := setupAuthManager(t)
	step, _ := AdminStep(mgr, db, "auth_users")

	// Validate should reject short password.
	pwField := step.Fields[1] // ADMIN_PASSWORD
	if err := pwField.Validate("short"); err == nil {
		t.Fatal("expected validation error for short password")
	}
}

// TestAdminStep_DuplicateEmailFails verifies CreateUser error propagates.
func TestAdminStep_DuplicateEmailFails(t *testing.T) {
	mgr, db := setupAuthManager(t)
	ctx := context.Background()

	step, _ := AdminStep(mgr, db, "auth_users")

	// First creation succeeds.
	err := step.Run(ctx, map[string]string{
		"ADMIN_EMAIL":    "admin@test.com",
		"ADMIN_PASSWORD": "securepass123",
	})
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}

	// Second creation with same email fails.
	err = step.Run(ctx, map[string]string{
		"ADMIN_EMAIL":    "admin@test.com",
		"ADMIN_PASSWORD": "anotherpass123",
	})
	if err == nil {
		t.Fatal("expected error for duplicate email")
	}
}

// TestAdminStep_EmailRequired verifies the email field validation.
func TestAdminStep_EmailRequired(t *testing.T) {
	mgr, db := setupAuthManager(t)
	step, _ := AdminStep(mgr, db, "auth_users")

	emailField := step.Fields[0] // ADMIN_EMAIL
	if err := emailField.Validate(""); err == nil {
		t.Fatal("expected error for empty email")
	}
	if err := emailField.Validate("valid@test.com"); err != nil {
		t.Fatalf("valid email should pass: %v", err)
	}
}

// TestAdminStep_EnvVars verifies the field EnvVars match the spec.
func TestAdminStep_EnvVars(t *testing.T) {
	mgr, db := setupAuthManager(t)
	step, _ := AdminStep(mgr, db, "auth_users")

	if step.Fields[0].EnvVar != "GOFASTR_ADMIN_EMAIL" {
		t.Fatalf("expected GOFASTR_ADMIN_EMAIL, got %s", step.Fields[0].EnvVar)
	}
	if step.Fields[1].EnvVar != "GOFASTR_ADMIN_PASSWORD" {
		t.Fatalf("expected GOFASTR_ADMIN_PASSWORD, got %s", step.Fields[1].EnvVar)
	}
	if !step.Fields[1].Secret {
		t.Fatal("password field must be Secret")
	}
}

// TestRender_PageContainsComponents verifies the wizard renders
// framework/ui components (AuthCard, ProgressSteps, Form) with their
// data-fui-comp markers — no bespoke CSS.
func TestRender_PageContainsComponents(t *testing.T) {
	done := false
	r := New(Config{
		DisableToken: true,
		Title:        "Test Setup",
		Complete:     func(_ context.Context) (bool, error) { return done, nil },
		Steps: []Step{
			{Name: "step1", Fields: []Field{{Name: "X", Label: "X"}}},
		},
	})
	h := r.Handler(func() {}, nil, nil)

	w := doGet(h, "/setup")
	body := w.Body.String()
	for _, marker := range []string{
		`data-fui-comp="ui-auth-card"`,
		`data-fui-comp="ui-progress-steps"`,
		`data-fui-comp="ui-form"`,
	} {
		if !strings.Contains(body, marker) {
			t.Errorf("wizard body must contain %s", marker)
		}
	}
	// Must NOT contain inline <style> tags.
	if strings.Contains(body, "<style") {
		t.Error("wizard must not contain inline <style> tags")
	}
}

// TestAdminStep_CustomTableName verifies AdminStep works with a
// non-default users table name. This is the regression test for the
// bug where AdminStep hardcoded SELECT 1 FROM users — the canonical
// convention is auth.NewEntityUserStore(db, "auth_users") and the
// table name is host-configurable, so any custom name must work.
func TestAdminStep_CustomTableName(t *testing.T) {
	const customTable = "my_app_users"
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	_, err = db.Exec(`CREATE TABLE ` + customTable + ` (
		id TEXT PRIMARY KEY,
		email TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL DEFAULT '',
		roles TEXT DEFAULT '[]',
		password_set BOOLEAN NOT NULL DEFAULT FALSE
	)`)
	if err != nil {
		t.Fatalf("create custom table: %v", err)
	}
	mgr := auth.New(auth.AuthConfig{
		JWTSecret: "test-secret",
		UserStore: auth.NewEntityUserStore(db, customTable),
		DevMode:   true,
	})
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("auth Init: %v", err)
	}

	step, completeFn := AdminStep(mgr, db, customTable)
	ctx := context.Background()

	// Before: no users.
	done, err := completeFn(ctx)
	if err != nil {
		t.Fatalf("completeFn: %v", err)
	}
	if done {
		t.Fatal("expected no users before AdminStep runs")
	}

	// Run the step.
	err = step.Run(ctx, map[string]string{
		"ADMIN_EMAIL":    "admin@test.com",
		"ADMIN_PASSWORD": "securepass123",
	})
	if err != nil {
		t.Fatalf("AdminStep.Run: %v", err)
	}

	// After: Complete flips — the custom table was queried, not "users".
	done, err = completeFn(ctx)
	if err != nil {
		t.Fatalf("completeFn after: %v", err)
	}
	if !done {
		t.Fatal("expected Complete=true after admin creation with custom table name")
	}
}

// TestAdminStep_EmptyTablePanics verifies that an empty table name
// fails loudly (panic) rather than silently defaulting to "users".
func TestAdminStep_EmptyTablePanics(t *testing.T) {
	mgr, db := setupAuthManager(t)
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for empty usersTable")
		}
	}()
	AdminStep(mgr, db, "")
}

// A final step whose Run succeeds but whose Complete predicate stays false
// (e.g. AdminStep probing a different table than the store writes to) must
// NOT render the "Setup Complete" page and must NOT fire the swap — that
// would show "your application is ready" over a permanently-503 app.
func TestSwap_CompleteStuckFalseIsHonest(t *testing.T) {
	var swapped int32
	r := New(Config{
		DisableToken: true,
		Complete:     func(_ context.Context) (bool, error) { return false, nil },
		Steps: []Step{
			{Name: "create", Fields: []Field{{Name: "NAME", Label: "Name"}},
				Run: func(_ context.Context, _ map[string]string) error { return nil }},
		},
	})
	h := r.Handler(func() { atomic.StoreInt32(&swapped, 1) }, nil, nil)

	w := doPost(h, "/setup", "NAME=alice", nil)
	if atomic.LoadInt32(&swapped) != 0 {
		t.Fatal("swap must not fire while Complete reports false")
	}
	body := w.Body.String()
	if strings.Contains(body, "Setup Complete") {
		t.Fatalf("must not claim completion while Complete is false; body: %.200s", body)
	}
	if !strings.Contains(body, "incomplete") {
		t.Fatalf("body should explain setup still reports incomplete; got: %.200s", body)
	}
}

// A transient Complete ERROR at the final re-check must also be honest and
// recoverable: the error is shown, and a later request (predicate healed)
// completes and swaps — no restart required.
func TestSwap_CompleteErrorRecovers(t *testing.T) {
	var swapped int32
	var failing atomic.Bool
	failing.Store(true)
	ran := false
	r := New(Config{
		DisableToken: true,
		Complete: func(_ context.Context) (bool, error) {
			if failing.Load() {
				return false, errors.New("db hiccup")
			}
			return ran, nil
		},
		Steps: []Step{
			{Name: "create", Fields: []Field{{Name: "NAME", Label: "Name"}},
				Run: func(_ context.Context, _ map[string]string) error { ran = true; return nil }},
		},
	})
	h := r.Handler(func() { atomic.StoreInt32(&swapped, 1) }, nil, nil)

	w := doPost(h, "/setup", "NAME=alice", nil)
	if strings.Contains(w.Body.String(), "Setup Complete") || atomic.LoadInt32(&swapped) != 0 {
		t.Fatal("completion must not be claimed while the Complete check errors")
	}

	// Predicate heals; a plain GET /setup now completes and swaps.
	failing.Store(false)
	w = doGet(h, "/setup")
	if atomic.LoadInt32(&swapped) != 1 {
		t.Fatalf("healed Complete on a later request must fire the swap (code %d)", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Setup Complete") {
		t.Fatal("healed request should render the completion page")
	}
}
