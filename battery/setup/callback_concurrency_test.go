package setup

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunStepsCancelsBeforeClaim(t *testing.T) {
	for _, busy := range []bool{true, false} {
		name := "free"
		if busy {
			name = "busy"
		}
		t.Run(name, func(t *testing.T) {
			var checks, runs atomic.Int32
			r := New(Config{
				Complete: func(context.Context) (bool, error) {
					checks.Add(1)
					return false, nil
				},
				Steps: []Step{{Name: "headless", Run: func(context.Context, map[string]string) error {
					runs.Add(1)
					return nil
				}}},
			})
			var wantChecks int32
			var ctx context.Context
			var cancel context.CancelFunc
			var wantErr error
			if busy {
				started, release := make(chan struct{}), make(chan struct{})
				finished := make(chan error, 1)
				defer func() {
					close(release)
					if err := <-finished; err != nil {
						t.Error(err)
					}
				}()
				go func() {
					finished <- r.runStepSerialized(t.Context(), 0, Step{
						Name: "interactive",
						Run: func(context.Context, map[string]string) error {
							close(started)
							<-release
							return nil
						},
					}, nil)
				}()
				select {
				case <-started:
				case <-time.After(time.Second):
					t.Fatal("interactive step did not start")
				}
				wantChecks = 1
				ctx, cancel = context.WithTimeout(t.Context(), 50*time.Millisecond)
				wantErr = context.DeadlineExceeded
			} else {
				ctx, cancel = context.WithCancel(t.Context())
				cancel()
				wantErr = context.Canceled
			}
			defer cancel()
			result := make(chan error, 1)
			go func() { result <- r.RunSteps(ctx) }()
			select {
			case err := <-result:
				if !errors.Is(err, wantErr) {
					t.Errorf("RunSteps = %v, want %v", err, wantErr)
				}
			case <-time.After(time.Second):
				t.Fatal("RunSteps did not return before the setup claim was released")
			}
			if got := checks.Load(); got != wantChecks || runs.Load() != 0 {
				t.Errorf("completion checks = %d, runs = %d, want %d checks and no headless runs", got, runs.Load(), wantChecks)
			}
		})
	}
}

func TestSetupCallbacksReenterRunner(t *testing.T) {
	for _, mode := range []string{"headless", "interactive"} {
		t.Run(mode, func(t *testing.T) {
			var r *Runner
			query := func() error {
				_, err := r.CanRunHeadless(context.Background())
				return err
			}
			step := Step{
				Name: "reentrant",
				Fields: []Field{{Name: "value", Validate: func(string) error {
					return query()
				}}},
				Run: func(context.Context, map[string]string) error { return query() },
			}
			r = New(Config{
				Complete: func(context.Context) (bool, error) { return false, query() },
				Steps:    []Step{step},
			})
			result := make(chan error, 1)
			go func() {
				ctx := context.Background()
				if mode == "headless" {
					result <- r.RunSteps(ctx)
					return
				}
				result <- r.runStepSerialized(ctx, 0, step, nil)
			}()
			select {
			case err := <-result:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("setup did not return after its callback queried the runner")
			}
		})
	}
}
