package hook

import (
	"context"
	"testing"
)

// #669 (was #657): ExecuteHooks ran the installed observer inline with
// no recover, so a panicking observer tore down the request goroutine
// that fired the hook. The observer is host-installed code (the
// coverage recorder today, any tooling tomorrow); a fault in it must
// not take the operation with it. The hooks themselves still run.
func TestPanickingObserverDoesNotKillHookExecution(t *testing.T) {
	SetObserver(func(Firing) { panic("observer boom") })
	defer SetObserver(nil)

	hr := NewHookRegistry()
	ran := false
	hr.RegisterHook(BeforeCreate, func(ctx context.Context, data any) error {
		ran = true
		return nil
	})
	// A panic in the observer would cross this call and fail the test.
	if err := hr.ExecuteHooks(context.Background(), BeforeCreate, nil); err != nil {
		t.Fatalf("ExecuteHooks returned the observer's fault: %v", err)
	}
	if !ran {
		t.Fatal("the registered hook did not run after the observer panicked")
	}
}
