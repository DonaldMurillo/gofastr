package framework

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHealth_LivenessAlwaysOK(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	app.registerHealthEndpoints()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	app.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("/healthz: got %d want 200", rr.Code)
	}
	if rr.Body.String() != "ok" {
		t.Fatalf("/healthz body: got %q want %q", rr.Body.String(), "ok")
	}
	if rr.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("/healthz must set Cache-Control: no-store")
	}
}

func TestReadiness_NoChecksIsReady(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	app.registerHealthEndpoints()

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rr := httptest.NewRecorder()
	app.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("/readyz: got %d want 200", rr.Code)
	}
	var resp ReadinessResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "ready" {
		t.Fatalf("status: got %q want %q", resp.Status, "ready")
	}
}

func TestReadiness_FailingCheckReturns503(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	app.RegisterReadiness("ok-thing", func(ctx context.Context) error { return nil })
	app.RegisterReadiness("broken-thing", func(ctx context.Context) error {
		return errors.New("connection refused")
	})
	app.registerHealthEndpoints()

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rr := httptest.NewRecorder()
	app.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz with failing check: got %d want 503", rr.Code)
	}
	var resp ReadinessResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "not_ready" {
		t.Fatalf("status: got %q want %q", resp.Status, "not_ready")
	}
	if len(resp.Checks) != 2 {
		t.Fatalf("checks: got %d want 2", len(resp.Checks))
	}
	var brokenSeen, okSeen bool
	for _, c := range resp.Checks {
		switch c.Name {
		case "broken-thing":
			brokenSeen = true
			if c.Status != "error" {
				t.Fatalf("broken-thing status: got %q want error", c.Status)
			}
			if c.Error == "" {
				t.Fatalf("broken-thing missing error message")
			}
		case "ok-thing":
			okSeen = true
			if c.Status != "ok" {
				t.Fatalf("ok-thing status: got %q want ok", c.Status)
			}
		}
	}
	if !brokenSeen || !okSeen {
		t.Fatalf("missing check rows: %+v", resp.Checks)
	}
}

func TestReadiness_ChecksRunInParallel(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	// Two slow checks that overlap — total duration should be ~one
	// check, not two, if they truly run in parallel.
	app.RegisterReadiness("slow-a", func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(80 * time.Millisecond):
			return nil
		}
	})
	app.RegisterReadiness("slow-b", func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(80 * time.Millisecond):
			return nil
		}
	})
	app.registerHealthEndpoints()

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rr := httptest.NewRecorder()
	start := time.Now()
	app.Router().ServeHTTP(rr, req)
	took := time.Since(start)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	// Two 80ms checks: ~80ms in parallel, ~160ms serial. The threshold must
	// sit between the two so the test actually distinguishes them (a 200ms
	// bar passed even when serial). 140ms leaves ~60ms of scheduling slack
	// over the parallel floor while still failing on serial execution.
	if took > 140*time.Millisecond {
		t.Fatalf("parallel checks should finish in ~80ms; took %v (serial?)", took)
	}
}

// TestRunReadinessChecks_InProcess pins the exported in-process API that
// battery/setup's HealthStep uses: same checks as /readyz, no HTTP, and
// error text redacted by default so callers can't leak internals.
func TestRunReadinessChecks_InProcess(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	app.RegisterReadiness("ok-thing", func(ctx context.Context) error { return nil })
	app.RegisterReadiness("broken-thing", func(ctx context.Context) error {
		return errors.New("dial 10.0.0.5: connection refused")
	})

	resp := app.RunReadinessChecks(context.Background())
	if len(resp.Checks) != 2 {
		t.Fatalf("checks: got %d want 2", len(resp.Checks))
	}
	byName := map[string]ReadinessResult{}
	for _, c := range resp.Checks {
		byName[c.Name] = c
	}
	if byName["ok-thing"].Status != "ok" {
		t.Errorf("ok-thing status: got %q want ok", byName["ok-thing"].Status)
	}
	if byName["broken-thing"].Status != "error" {
		t.Errorf("broken-thing status: got %q want error", byName["broken-thing"].Status)
	}
	if got := byName["broken-thing"].Error; got != "check failed" {
		t.Errorf("error must be redacted by default: got %q", got)
	}
}

// TestRunReadinessChecks_Verbose pins the verbose opt-in: original error
// text passes through for operator-side debugging.
func TestRunReadinessChecks_Verbose(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware(), WithVerboseReadiness())
	app.RegisterReadiness("broken-thing", func(ctx context.Context) error {
		return errors.New("dial 10.0.0.5: connection refused")
	})

	resp := app.RunReadinessChecks(context.Background())
	if len(resp.Checks) != 1 {
		t.Fatalf("checks: got %d want 1", len(resp.Checks))
	}
	if got := resp.Checks[0].Error; got != "dial 10.0.0.5: connection refused" {
		t.Errorf("verbose error passthrough: got %q", got)
	}
}
