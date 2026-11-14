package cron

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// Every run of a job is a trace named "cron <job>", failed when the run
// returns an error or panics: a scheduled run used to leave nothing a
// trace reader could find, so one that failed or took long showed up
// only as a log line, if anywhere. The scheduler still gets the error.
func TestEachRunIsATrace(t *testing.T) {
	prev := otel.GetTracerProvider()
	sr := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr)))
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	s := NewScheduler()
	var mu sync.Mutex
	reported := map[string]string{}
	s.OnError = func(job string, err error) {
		mu.Lock()
		defer mu.Unlock()
		reported[job] = err.Error()
	}
	for name, run := range map[string]func(context.Context) error{
		"cleanup": func(context.Context) error { return nil },
		"export":  func(context.Context) error { return errors.New("bucket unreachable") },
		"rollup":  func(context.Context) error { panic("nil ledger") },
	} {
		if err := s.Register(CronJob{Name: name, Spec: "* * * * *", Run: run}); err != nil {
			t.Fatal(err)
		}
	}
	s.RunOnce(context.Background(), time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC))
	deadline := time.Now().Add(5 * time.Second)
	for len(sr.Ended()) < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	got := map[string]codes.Code{}
	for _, sp := range sr.Ended() {
		got[sp.Name()] = sp.Status().Code
		if sp.Parent().IsValid() {
			t.Errorf("%s is a step of another trace", sp.Name())
		}
	}
	if len(got) != 3 || got["cron cleanup"] == codes.Error || got["cron export"] != codes.Error || got["cron rollup"] != codes.Error {
		t.Fatalf("spans: %v", got)
	}
	// Reported as before: the error as returned, the panic with its stack.
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(reported)
		mu.Unlock()
		if n == 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if reported["export"] != "bucket unreachable" || len(reported["rollup"]) < len("panic: nil ledger") || reported["cleanup"] != "" {
		t.Fatalf("reported: %v", reported)
	}
}
