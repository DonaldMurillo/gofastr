package framework

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// mockSetupRunnerSwappable is a richer mock for integration tests: it
// captures the swap callback so tests can trigger it via HTTP.
type mockSetupRunnerSwappable struct {
	incomplete     bool
	canHeadless    bool
	runStepsErr    error
	runStepsCalled atomic.Int32
	setupURLStr    string

	mu     sync.Mutex
	swapFn func()
}

func (m *mockSetupRunnerSwappable) Incomplete(_ context.Context) (bool, error) {
	return m.incomplete, nil
}

func (m *mockSetupRunnerSwappable) CanRunHeadless(_ context.Context) (bool, error) {
	return m.canHeadless, nil
}

func (m *mockSetupRunnerSwappable) RunSteps(_ context.Context) error {
	m.runStepsCalled.Add(1)
	return m.runStepsErr
}

func (m *mockSetupRunnerSwappable) Handler(swap func(), healthz, readyz http.HandlerFunc) http.Handler {
	m.mu.Lock()
	m.swapFn = swap
	m.mu.Unlock()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Health endpoints.
		if r.URL.Path == "/healthz" && healthz != nil {
			healthz(w, r)
			return
		}
		if r.URL.Path == "/readyz" && readyz != nil {
			readyz(w, r)
			return
		}
		// Non-setup paths get 503.
		if r.URL.Path != "/setup" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		// POST /setup triggers swap (simulates wizard completion).
		if r.Method == http.MethodPost {
			m.mu.Lock()
			fn := m.swapFn
			m.mu.Unlock()
			if fn != nil {
				fn()
			}
		}
		w.WriteHeader(http.StatusOK)
	})
}

func (m *mockSetupRunnerSwappable) SetupURL(addr string) string {
	if m.setupURLStr != "" {
		return m.setupURLStr
	}
	return "http://" + addr + "/setup"
}

// ─── Tests ───────────────────────────────────────────────────────────

// TestSetup_WorkerRoleIncompleteFails: worker role + incomplete setup →
// Start returns a descriptive error.
func TestSetup_WorkerRoleIncompleteFails(t *testing.T) {
	t.Setenv("GOFASTR_SETUP", "")
	t.Setenv("GOFASTR_ROLE", "")
	mock := &mockSetupRunnerSwappable{incomplete: true}
	app := NewApp(
		WithoutDefaultMiddleware(),
		WithRole(RoleWorker),
		WithSetup(mock),
	)
	err := app.Start("127.0.0.1:0")
	if err == nil {
		t.Fatal("expected Start to fail for worker + incomplete setup")
	}
	if !strings.Contains(err.Error(), "serve/all") {
		t.Fatalf("error must mention serve/all process, got: %s", err.Error())
	}
}

// TestSetup_OffSkipsSetupCheck: GOFASTR_SETUP=off bypasses setup even
// when Incomplete returns true.
func TestSetup_OffSkipsSetupCheck(t *testing.T) {
	t.Setenv("GOFASTR_SETUP", "off")
	t.Setenv("GOFASTR_ROLE", "")
	mock := &mockSetupRunnerSwappable{incomplete: true}
	app := NewApp(
		WithoutDefaultMiddleware(),
		WithSetup(mock),
	)
	_, stop := startOnRandomPort(t, app)
	defer stop()
	// If setup was not skipped, Start would have entered interactive mode
	// and the test would hang (setup handler would be served). Reaching
	// this assertion means Start proceeded normally.
}

// TestSetup_ConsumersDeferredUntilSwap: interactive setup defers
// consumer startup — the queue must not start until the wizard completes.
func TestSetup_ConsumersDeferredUntilSwap(t *testing.T) {
	t.Setenv("GOFASTR_SETUP", "")
	t.Setenv("GOFASTR_ROLE", "")

	f := &fakeStartStop{}
	mock := &mockSetupRunnerSwappable{incomplete: true, canHeadless: false}
	app := NewApp(
		WithoutDefaultMiddleware(),
		WithSetup(mock),
	)
	app.AddQueue(f)

	addr, stop := startOnRandomPort(t, app)
	defer stop()

	// Consumer must NOT have started during interactive setup.
	if got := f.starts.Load(); got != 0 {
		t.Fatalf("queue must not start during setup, got %d starts", got)
	}

	// Trigger swap via POST to /setup (the mock's handler calls swap on POST).
	resp, err := http.Post("http://"+addr+"/setup", "application/x-www-form-urlencoded", nil)
	if err != nil {
		t.Fatalf("POST /setup: %v", err)
	}
	resp.Body.Close()

	// Give the swap goroutine time to run.
	time.Sleep(200 * time.Millisecond)

	// Now the consumer should have started.
	if got := f.starts.Load(); got != 1 {
		t.Fatalf("expected queue to start after swap, got %d", got)
	}
}

// TestSetup_HeadlessRunsStepsBeforePort: headless setup runs steps
// inline before the port binds.
func TestSetup_HeadlessRunsStepsBeforePort(t *testing.T) {
	t.Setenv("GOFASTR_SETUP", "")
	t.Setenv("GOFASTR_ROLE", "")

	mock := &mockSetupRunnerSwappable{incomplete: true, canHeadless: true}
	app := NewApp(
		WithoutDefaultMiddleware(),
		WithSetup(mock),
	)
	_, stop := startOnRandomPort(t, app)
	defer stop()

	if got := mock.runStepsCalled.Load(); got != 1 {
		t.Fatalf("expected RunSteps called once during headless boot, got %d", got)
	}
}

// TestSetup_PreSetupNonSetupPath503: during interactive setup, a
// non-setup path returns 503.
func TestSetup_PreSetupNonSetupPath503(t *testing.T) {
	t.Setenv("GOFASTR_SETUP", "")
	t.Setenv("GOFASTR_ROLE", "")

	mock := &mockSetupRunnerSwappable{incomplete: true, canHeadless: false}
	app := NewApp(
		WithoutDefaultMiddleware(),
		WithSetup(mock),
	)
	addr, stop := startOnRandomPort(t, app)
	defer stop()

	resp, err := http.Get("http://" + addr + "/some/random/path")
	if err != nil {
		t.Fatalf("GET /some/random/path: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 for non-setup path, got %d", resp.StatusCode)
	}
}

// TestSetup_HealthzDuringSetup: /healthz returns 200 during interactive setup.
func TestSetup_HealthzDuringSetup(t *testing.T) {
	t.Setenv("GOFASTR_SETUP", "")
	t.Setenv("GOFASTR_ROLE", "")

	mock := &mockSetupRunnerSwappable{incomplete: true, canHeadless: false}
	app := NewApp(
		WithoutDefaultMiddleware(),
		WithSetup(mock),
	)
	addr, stop := startOnRandomPort(t, app)
	defer stop()

	resp, err := http.Get("http://" + addr + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for /healthz during setup, got %d", resp.StatusCode)
	}
}

// TestSetup_SwapSwitchesToRealRouter: after the wizard completes and
// swap fires, the real app router serves instead of the setup handler.
func TestSetup_SwapSwitchesToRealRouter(t *testing.T) {
	t.Setenv("GOFASTR_SETUP", "")
	t.Setenv("GOFASTR_ROLE", "")

	// Register a route on the real router that the test will hit after swap.
	app := NewApp(WithoutDefaultMiddleware())
	app.Router().Get("/real-route", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("real"))
	}))

	mock := &mockSetupRunnerSwappable{incomplete: true, canHeadless: false}
	app.setup = mock // set after NewApp so routes are registered first

	addr, stop := startOnRandomPort(t, app)
	defer stop()

	// Before swap: /real-route is 503 (setup surface intercepts).
	resp, err := http.Get("http://" + addr + "/real-route")
	if err != nil {
		t.Fatalf("GET /real-route pre-swap: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("pre-swap expected 503, got %d", resp.StatusCode)
	}

	// Trigger swap via POST to /setup.
	resp2, err := http.Post("http://"+addr+"/setup", "application/x-www-form-urlencoded", nil)
	if err != nil {
		t.Fatalf("POST /setup: %v", err)
	}
	resp2.Body.Close()

	time.Sleep(200 * time.Millisecond)

	// After swap: /real-route hits the real router → 200.
	resp3, err := http.Get("http://" + addr + "/real-route")
	if err != nil {
		t.Fatalf("GET /real-route post-swap: %v", err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusOK {
		t.Fatalf("post-swap expected 200, got %d", resp3.StatusCode)
	}
}

// errSetupRunner errors from the method named by failAt, so Start's setup
// error branches (Incomplete / CanRunHeadless / RunSteps) each abort.
type errSetupRunner struct {
	mockSetupRunnerSwappable
	failAt string
}

func (e *errSetupRunner) Incomplete(ctx context.Context) (bool, error) {
	if e.failAt == "incomplete" {
		return false, errors.New("boom incomplete")
	}
	return e.mockSetupRunnerSwappable.Incomplete(ctx)
}

func (e *errSetupRunner) CanRunHeadless(ctx context.Context) (bool, error) {
	if e.failAt == "headless" {
		return false, errors.New("boom headless")
	}
	return e.mockSetupRunnerSwappable.CanRunHeadless(ctx)
}

func (e *errSetupRunner) RunSteps(ctx context.Context) error {
	if e.failAt == "runsteps" {
		return errors.New("boom runsteps")
	}
	return e.mockSetupRunnerSwappable.RunSteps(ctx)
}

// Every setup-runner error path aborts Start with a wrapped error.
func TestSetup_RunnerErrorsAbortStart(t *testing.T) {
	for _, failAt := range []string{"incomplete", "headless", "runsteps"} {
		t.Run(failAt, func(t *testing.T) {
			t.Setenv("GOFASTR_SETUP", "")
			r := &errSetupRunner{failAt: failAt}
			r.incomplete = true
			r.canHeadless = true
			app := NewApp(WithoutDefaultMiddleware(), WithSetup(r))
			app.Config.DisableSignalHandling = true
			err := app.Start("127.0.0.1:0")
			if err == nil || !strings.Contains(err.Error(), "setup") {
				t.Fatalf("Start with failing %s should abort with a setup error, got %v", failAt, err)
			}
		})
	}
}

// An invalid GOFASTR_SETUP value fails Start loudly (mirrors GOFASTR_ROLE).
func TestSetup_InvalidEnvAbortsStart(t *testing.T) {
	t.Setenv("GOFASTR_SETUP", "bananas")
	r := &mockSetupRunnerSwappable{incomplete: true}
	app := NewApp(WithoutDefaultMiddleware(), WithSetup(r))
	app.Config.DisableSignalHandling = true
	err := app.Start("127.0.0.1:0")
	if err == nil || !strings.Contains(err.Error(), "GOFASTR_SETUP") {
		t.Fatalf("Start with invalid GOFASTR_SETUP should fail naming the var, got %v", err)
	}
}

// A deferred start hook failing at swap time must get boot-parity
// fail-loud semantics: the real handler serves (setup IS complete), the
// failure is logged, and the process shuts down — never a silent
// half-up server, never a setup surface serving 503s forever.
func TestSetup_SwapHookFailureShutsDown(t *testing.T) {
	t.Setenv("GOFASTR_SETUP", "")
	r := &mockSetupRunnerSwappable{incomplete: true}
	app := NewApp(WithoutDefaultMiddleware(), WithSetup(r))
	app.Config.DisableSignalHandling = true
	app.OnStart(func(context.Context) error {
		return errors.New("boom consumer")
	})

	ready := make(chan string, 1)
	app.OnReady(func(a string) { ready <- a })
	startDone := make(chan error, 1)
	go func() { startDone <- app.Start("127.0.0.1:0") }()

	var addr string
	select {
	case addr = <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("server never became ready")
	}

	// Complete the wizard: the mock's POST /setup fires the swap, whose
	// deferred hooks fail → shutdown.
	resp, err := http.Post("http://"+addr+"/setup", "application/x-www-form-urlencoded", nil)
	if err != nil {
		t.Fatalf("POST /setup: %v", err)
	}
	resp.Body.Close()

	select {
	case <-startDone:
		// Start returned — the process shut itself down. Correct.
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after a swap-time hook failure — half-up server")
	}
}
