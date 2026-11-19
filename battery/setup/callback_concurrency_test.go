package setup

import (
	"context"
	"testing"
	"time"
)

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
