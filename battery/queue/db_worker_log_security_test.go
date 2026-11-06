package queue

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

func panicClockQueue(logger *slog.Logger, now func() time.Time) *DBQueue {
	return &DBQueue{
		handlers: map[string]Handler{"job": func(context.Context, Job) error { return nil }},
		stop:     make(chan struct{}), logger: logger, now: now,
	}
}

func TestWorkerPanicIsLogged(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "worker-log")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	queue := panicClockQueue(slog.New(slog.NewJSONHandler(file, nil)), func() time.Time {
		panic("claim\nforged\x1b\u202e")
	})
	if queue.runWorker(t.Context(), "priority") {
		t.Fatal("panicking worker reported a clean shutdown")
	}
	data, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("missing worker diagnostic: %q (%v)", data, err)
	}
	panicText, _ := record["panic"].(string)
	stack, _ := record["stack"].(string)
	if record["lane"] != "priority" || !strings.Contains(panicText, "claim") ||
		strings.ContainsAny(panicText, "\n\x1b\u202e") || !strings.Contains(stack, "runtime/debug.Stack") {
		t.Fatalf("incomplete or unsafe worker diagnostic: %#v", record)
	}
}

func TestWorkerRespawnBacksOff(t *testing.T) {
	for _, shutdown := range []string{"cancel", "stop"} {
		t.Run(shutdown, func(t *testing.T) {
			calls := make(chan time.Time, 16)
			queue := panicClockQueue(slog.New(slog.NewTextHandler(io.Discard, nil)), func() time.Time {
				select {
				case calls <- time.Now():
				default:
				}
				panic("claim failed")
			})
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan struct{}, 1)
			exited := make(chan struct{})
			go func() {
				defer close(exited)
				queue.superviseWorker(ctx, "", done)
			}()
			t.Cleanup(func() {
				cancel()
				select {
				case <-exited:
				case <-time.After(time.Second):
					t.Error("worker did not stop")
				}
			})
			nextCall := func() time.Time {
				t.Helper()
				select {
				case when := <-calls:
					return when
				case <-time.After(time.Second):
					t.Fatal("worker did not start or respawn")
					return time.Time{}
				}
			}
			first := nextCall()
			second := nextCall()
			if delay := second.Sub(first); delay < 75*time.Millisecond {
				t.Fatalf("worker respawned without backoff: %v", delay)
			}
			if shutdown == "cancel" {
				cancel()
			} else {
				close(queue.stop)
			}
			select {
			case <-done:
			case <-time.After(75 * time.Millisecond):
				t.Fatal("shutdown waited for the respawn delay")
			}
		})
	}
}
