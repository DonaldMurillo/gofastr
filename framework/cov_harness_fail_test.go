package framework

import (
	"net/http"
	"runtime"
	"testing"
)

// cov_fakeTB is a minimal testing.TB that records the first Fatalf and
// then Goexits exactly like the real *testing.T, so the harness
// assertion helpers take their failure branch without aborting the
// parent test. Only Helper/Fatalf are exercised by the assertions.
type cov_fakeTB struct {
	testing.TB
	failed bool
}

func (f *cov_fakeTB) Helper() {}
func (f *cov_fakeTB) Fatalf(string, ...any) {
	f.failed = true
	runtime.Goexit()
}

// cov_runFail runs fn (which is expected to call the fake's Fatalf) in a
// goroutine so the Goexit unwinds only that goroutine, then reports
// whether the failure branch fired.
func cov_runFail(fn func(tb testing.TB)) bool {
	f := &cov_fakeTB{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn(f)
	}()
	<-done
	return f.failed
}

// Drive every assertion-helper failure branch through the fake TB.
func TestCovHarnessAssertionFailures(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	app.Router().Get("/x", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"a":1}`))
	}))
	ta := TestHarness(t, app)

	// AssertStatus mismatch (line 153 branch).
	if !cov_runFail(func(tb testing.TB) { ta.Get("/x").AssertStatus(tb, http.StatusTeapot) }) {
		t.Error("AssertStatus mismatch branch did not fire")
	}
	// AssertStatus with a stored creation error (line 150 branch).
	if !cov_runFail(func(tb testing.TB) {
		(&TestResponse{err: errStored}).AssertStatus(tb, 200)
	}) {
		t.Error("AssertStatus err branch did not fire")
	}

	// AssertJSON mismatch (line 182 branch).
	if !cov_runFail(func(tb testing.TB) {
		ta.Get("/x").AssertJSON(tb, map[string]any{"a": 999})
	}) {
		t.Error("AssertJSON mismatch branch did not fire")
	}
	// AssertJSON stored creation error (line 165 branch).
	if !cov_runFail(func(tb testing.TB) {
		(&TestResponse{err: errStored}).AssertJSON(tb, nil)
	}) {
		t.Error("AssertJSON err branch did not fire")
	}
	// AssertJSON marshal-of-expected error (line 168/170 branch): a channel
	// can't be marshalled.
	if !cov_runFail(func(tb testing.TB) {
		ta.Get("/x").AssertJSON(tb, make(chan int))
	}) {
		t.Error("AssertJSON marshal-expected branch did not fire")
	}
	// AssertJSON unmarshal-body error (line 176 branch): body isn't JSON.
	app.Router().Get("/notjson", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not json at all`))
	}))
	if !cov_runFail(func(tb testing.TB) {
		ta.Get("/notjson").AssertJSON(tb, map[string]any{})
	}) {
		t.Error("AssertJSON body-unmarshal branch did not fire")
	}

	// AssertHeader mismatch (line 192 branch).
	if !cov_runFail(func(tb testing.TB) {
		ta.Get("/x").AssertHeader(tb, "X-Nope", "value")
	}) {
		t.Error("AssertHeader mismatch branch did not fire")
	}

	// AssertBodyContains mismatch (line 201 branch).
	if !cov_runFail(func(tb testing.TB) {
		ta.Get("/x").AssertBodyContains(tb, "definitely-absent")
	}) {
		t.Error("AssertBodyContains mismatch branch did not fire")
	}
}

var errStored = &covErr{}

type covErr struct{}

func (covErr) Error() string { return "stored creation error" }

// TestHarness aborts via t.Fatalf when InitPlugins fails. Drive that
// branch with a plugin whose Init errors, through the fake TB.
func TestCovTestHarnessInitError(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	app.RegisterPlugin(&covFailPlugin{})
	if !cov_runFail(func(tb testing.TB) {
		TestHarness(tb, app)
	}) {
		t.Error("expected TestHarness to fail when a plugin Init errors")
	}
}

type covFailPlugin struct{}

func (covFailPlugin) Name() string      { return "cov-fail-plugin" }
func (covFailPlugin) Init(_ *App) error { return errStored }

// WithBody returns early (no body set) when the value can't be marshalled.
func TestCovWithBodyMarshalError(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	app.Router().Post("/echo", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			_, _ = w.Write([]byte("had-body"))
		}
	}))
	ta := TestHarness(t, app)
	// A channel can't be marshalled → WithBody returns tr unchanged (no body).
	tr := ta.Request(http.MethodPost, "/echo", nil).WithBody(make(chan int))
	if tr.request.Header.Get("Content-Type") == "application/json" {
		t.Fatal("WithBody should not set Content-Type on marshal failure")
	}
}

// doRequest applies caller-supplied headers (the headers loop, line 82).
func TestCovDoRequestHeaders(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	app.Router().Get("/h", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.Header.Get("X-Cov")))
	}))
	ta := TestHarness(t, app)
	resp := ta.doRequest(http.MethodGet, "/h", nil, map[string]string{"X-Cov": "seen"})
	if resp.Body() != "seen" {
		t.Fatalf("header not applied, body=%q", resp.Body())
	}
}
