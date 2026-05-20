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
	if took > 200*time.Millisecond {
		t.Fatalf("parallel checks should finish in ~80ms; took %v", took)
	}
}
