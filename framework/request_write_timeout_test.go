package framework

import (
	"strings"
	"testing"
	"time"
)

// AppConfig.RequestTimeout and the server's WriteTimeout are each valid
// alone. A request budget at or past the write deadline is dead config: a
// buffered response finished after the write deadline is never delivered
// (the client sees the connection drop, not the response and not the
// timeout middleware's 504), while the handler keeps running to the
// budget. Raising RequestTimeout past the 60s default write deadline was
// the common way in: the slow handler it was raised for still lost its
// response at 60s.

// An unset WriteTimeout follows a RequestTimeout raised past it.
func TestRaisedRequestTimeoutRaisesDefaultWriteTimeout(t *testing.T) {
	app := NewApp(WithConfig(AppConfig{RequestTimeout: 90 * time.Second}))
	_, stop := startOnRandomPort(t, app)
	defer stop()

	app.serverMu.Lock()
	srv := app.server
	app.serverMu.Unlock()
	if want := 90*time.Second + requestTimeoutWriteSlack; srv.WriteTimeout != want {
		t.Fatalf("WriteTimeout = %s under a 90s RequestTimeout, want %s: a handler inside its budget loses its response at the write deadline",
			srv.WriteTimeout, want)
	}
}

// Both set explicitly, the pair is refused at Start.
func TestRequestTimeoutPastExplicitWriteTimeoutRefused(t *testing.T) {
	app := NewApp(WithConfig(AppConfig{
		RequestTimeout:        2 * time.Second,
		DisableSignalHandling: true,
		HTTPServerTimeouts:    HTTPServerTimeoutsConfig{WriteTimeout: new(time.Second)},
	}))
	ready := make(chan struct{}, 1)
	app.OnReady(func(string) { ready <- struct{}{} })
	done := make(chan error, 1)
	go func() { done <- app.Start("127.0.0.1:0") }()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "WriteTimeout") {
			t.Fatalf("Start = %v, want a refusal naming WriteTimeout", err)
		}
	case <-ready:
		t.Cleanup(func() { _ = app.Shutdown(t.Context()) })
		t.Fatal("Start served a 2s RequestTimeout behind a 1s WriteTimeout")
	case <-time.After(10 * time.Second):
		t.Fatal("Start neither refused nor became ready")
	}
}

// The implicit 30s budget is enforced like an explicit one: a write cut
// under it is refused, because the timeout middleware's own 504 at 30s
// could never pass a 20s write deadline.
func TestImplicitDefaultBudgetPastExplicitWriteTimeoutRefused(t *testing.T) {
	app := NewApp(WithConfig(AppConfig{
		DisableSignalHandling: true,
		HTTPServerTimeouts:    HTTPServerTimeoutsConfig{WriteTimeout: new(20 * time.Second)},
	}))
	ready := make(chan struct{}, 1)
	app.OnReady(func(string) { ready <- struct{}{} })
	done := make(chan error, 1)
	go func() { done <- app.Start("127.0.0.1:0") }()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "WriteTimeout") {
			t.Fatalf("Start = %v, want a refusal naming WriteTimeout", err)
		}
	case <-ready:
		t.Cleanup(func() { _ = app.Shutdown(t.Context()) })
		t.Fatal("Start served a 20s WriteTimeout under the enforced 30s default budget")
	case <-time.After(10 * time.Second):
		t.Fatal("Start neither refused nor became ready")
	}
}

// Pairs that are not a contradiction stay as configured.
func TestRequestTimeoutWriteTimeoutPairsKept(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  AppConfig
		want time.Duration
	}{
		{"default request budget", AppConfig{}, 60 * time.Second},
		{"budget inside the default deadline", AppConfig{RequestTimeout: 45 * time.Second}, 60 * time.Second},
		{"write deadline disabled", AppConfig{RequestTimeout: 2 * time.Minute,
			HTTPServerTimeouts: HTTPServerTimeoutsConfig{WriteTimeout: new(time.Duration(0))}}, 0},
		{"explicit deadline past the budget", AppConfig{RequestTimeout: 2 * time.Minute,
			HTTPServerTimeouts: HTTPServerTimeoutsConfig{WriteTimeout: new(3 * time.Minute)}}, 3 * time.Minute},
		{"timeout middleware off", AppConfig{RequestTimeout: 2 * time.Minute, DisableRequestTimeout: true,
			HTTPServerTimeouts: HTTPServerTimeoutsConfig{WriteTimeout: new(time.Second)}}, time.Second},
		// The framework's own 30s default budget is enforced like any
		// other: a write cut under it can drop the timeout middleware's
		// own 504, so the pair is refused (see the refusal test below).
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := NewApp(WithConfig(tc.cfg))
			_, stop := startOnRandomPort(t, app)
			defer stop()
			app.serverMu.Lock()
			got := app.server.WriteTimeout
			app.serverMu.Unlock()
			if got != tc.want {
				t.Errorf("WriteTimeout = %s, want %s", got, tc.want)
			}
		})
	}
}
