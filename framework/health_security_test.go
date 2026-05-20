package framework

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ----- ReadinessRegistrar probe actually fires ------------------------------

type readinessBattery struct {
	called bool
}

func (b *readinessBattery) Name() string             { return "readiness-test" }
func (b *readinessBattery) Init(_ *App) error        { return nil }
func (b *readinessBattery) RegisterReadinessChecks(app *App) {
	b.called = true
	app.RegisterReadiness("from-battery", func(_ context.Context) error { return nil })
}

func TestApp_ProbesBatteryReadinessRegistrar(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	b := &readinessBattery{}
	app.RegisterBattery(b)

	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	if !b.called {
		t.Fatalf("battery's RegisterReadinessChecks was never invoked")
	}
	// The check it added must be visible to /readyz.
	app.registerHealthEndpoints()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rr := httptest.NewRecorder()
	app.Router().ServeHTTP(rr, req)
	var resp ReadinessResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	var sawBattery bool
	for _, c := range resp.Checks {
		if c.Name == "from-battery" {
			sawBattery = true
		}
	}
	if !sawBattery {
		t.Fatalf("battery-registered check missing from /readyz: %+v", resp.Checks)
	}
}

type readinessPlugin struct {
	called bool
}

func (p *readinessPlugin) Name() string      { return "readiness-plugin" }
func (p *readinessPlugin) Init(_ *App) error { return nil }
func (p *readinessPlugin) RegisterReadinessChecks(app *App) {
	p.called = true
	app.RegisterReadiness("from-plugin", func(_ context.Context) error { return nil })
}

func TestApp_ProbesPluginReadinessRegistrar(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	p := &readinessPlugin{}
	app.RegisterPlugin(p)

	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	if !p.called {
		t.Fatalf("plugin's RegisterReadinessChecks was never invoked")
	}
}

// ----- panic recovery + nil-check on the check fn ---------------------------

func TestReadiness_RecoversFromPanickingCheck(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	app.RegisterReadiness("panicker", func(_ context.Context) error {
		panic("boom from the check")
	})
	app.RegisterReadiness("happy", func(_ context.Context) error { return nil })
	app.registerHealthEndpoints()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicking check propagated out of /readyz: %v", r)
		}
	}()

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rr := httptest.NewRecorder()
	app.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("panicking check should mark /readyz as 503, got %d", rr.Code)
	}
	var resp ReadinessResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	var sawPanic bool
	for _, c := range resp.Checks {
		if c.Name == "panicker" && c.Status == "error" {
			sawPanic = true
		}
	}
	if !sawPanic {
		t.Fatalf("expected panicker row with status=error; got %+v", resp.Checks)
	}
}

func TestReadiness_NilCheckFnTreatedAsError(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	app.RegisterReadiness("nil-fn", nil)
	app.registerHealthEndpoints()

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rr := httptest.NewRecorder()
	app.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil check fn must mark /readyz as 503, got %d", rr.Code)
	}
}

// ----- error.Error() not leaked to public /readyz ---------------------------

func TestReadiness_DoesNotLeakInternalErrorTextByDefault(t *testing.T) {
	const leak = "dial tcp 10.0.3.17:5432: connect: connection refused"
	app := NewApp(WithoutDefaultMiddleware())
	app.RegisterReadiness("db", func(_ context.Context) error {
		return errors.New(leak)
	})
	app.registerHealthEndpoints()

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rr := httptest.NewRecorder()
	app.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d want 503", rr.Code)
	}
	if strings.Contains(rr.Body.String(), leak) {
		t.Fatalf("/readyz must not leak verbose error text by default; body=%q", rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "10.0.3.17") {
		t.Fatalf("/readyz must not leak the internal IP; body=%q", rr.Body.String())
	}
}

func TestReadiness_ExposesErrorTextWhenOptedIn(t *testing.T) {
	const leak = "specific reason"
	app := NewApp(WithoutDefaultMiddleware(), WithVerboseReadiness())
	app.RegisterReadiness("x", func(_ context.Context) error { return errors.New(leak) })
	app.registerHealthEndpoints()

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rr := httptest.NewRecorder()
	app.Router().ServeHTTP(rr, req)

	if !strings.Contains(rr.Body.String(), leak) {
		t.Fatalf("with WithVerboseReadiness, error text should appear; body=%q", rr.Body.String())
	}
}

// ----- /readyz survives a check that ignores ctx and hangs ------------------

func TestReadiness_DoesNotHangPastDeadline(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware(), WithReadinessTimeout(80*time.Millisecond))
	app.RegisterReadiness("slow", func(_ context.Context) error {
		// Ignore ctx entirely.
		time.Sleep(2 * time.Second)
		return nil
	})
	app.registerHealthEndpoints()

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rr := httptest.NewRecorder()
	start := time.Now()
	app.Router().ServeHTTP(rr, req)
	took := time.Since(start)
	if took > 300*time.Millisecond {
		t.Fatalf("/readyz waited past the deadline for a ctx-ignoring check; took %v", took)
	}
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("over-deadline check should mark /readyz 503, got %d", rr.Code)
	}
}
