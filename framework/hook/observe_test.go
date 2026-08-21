package hook

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestHookTypeStringsAreStableAndDistinct(t *testing.T) {
	// These strings are manifest keys. Changing one silently invalidates
	// every recorded coverage entry for that lifecycle point, so they are
	// pinned here rather than left to a switch nobody re-reads.
	want := map[HookType]string{
		BeforeCreate: "beforecreate", AfterCreate: "aftercreate",
		BeforeUpdate: "beforeupdate", AfterUpdate: "afterupdate",
		BeforeDelete: "beforedelete", AfterDelete: "afterdelete",
		BeforeList: "beforelist", AfterList: "afterlist",
		BeforeGet: "beforeget", AfterGet: "afterget",
	}
	seen := map[string]HookType{}
	for hookType, name := range want {
		if got := hookType.String(); got != name {
			t.Errorf("HookType(%d).String() = %q, want %q", hookType, got, name)
		}
		if prior, dup := seen[name]; dup {
			t.Errorf("%q is shared by HookType(%d) and HookType(%d)", name, prior, hookType)
		}
		seen[name] = hookType
	}
	if got := HookType(99).String(); got != "unknown" {
		t.Errorf("unknown hook type = %q", got)
	}
}

func TestObserverFiresOnlyForRegisteredHooks(t *testing.T) {
	t.Cleanup(func() { SetObserver(nil) })

	var mu sync.Mutex
	var fired []Firing
	SetObserver(func(f Firing) {
		mu.Lock()
		defer mu.Unlock()
		fired = append(fired, f)
	})

	reg := NewHookRegistry()
	reg.SetLabel("posts")
	if got := reg.Label(); got != "posts" {
		t.Fatalf("Label() = %q", got)
	}
	reg.RegisterHook(AfterCreate, func(ctx context.Context, data any) error { return nil })

	// A lifecycle point with nothing registered: ExecuteHooks runs on
	// every CRUD operation, so counting the call would credit an app with
	// no hooks at all with complete hook coverage.
	if err := reg.ExecuteHooks(context.Background(), BeforeDelete, nil); err != nil {
		t.Fatal(err)
	}
	if err := reg.ExecuteHooks(context.Background(), AfterCreate, nil); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(fired) != 1 {
		t.Fatalf("fired = %+v, want exactly the registered hook", fired)
	}
	if fired[0].Entity != "posts" || fired[0].Type != AfterCreate {
		t.Errorf("firing = %+v", fired[0])
	}
}

func TestObserverFiresEvenWhenTheHookFails(t *testing.T) {
	// The hook ran, that is what coverage records. Whether it returned
	// an error is the test's business, not the manifest's.
	t.Cleanup(func() { SetObserver(nil) })
	fired := 0
	SetObserver(func(Firing) { fired++ })

	reg := NewHookRegistry()
	reg.RegisterHook(BeforeCreate, func(ctx context.Context, data any) error {
		return errors.New("rejected")
	})
	if err := reg.ExecuteHooks(context.Background(), BeforeCreate, nil); err == nil {
		t.Fatal("expected the hook's error to surface")
	}
	if fired != 1 {
		t.Errorf("fired = %d, want 1", fired)
	}
}

func TestClearedObserverIsNotCalled(t *testing.T) {
	called := false
	SetObserver(func(Firing) { called = true })
	SetObserver(nil)

	reg := NewHookRegistry()
	reg.RegisterHook(AfterGet, func(ctx context.Context, data any) error { return nil })
	if err := reg.ExecuteHooks(context.Background(), AfterGet, nil); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Error("observer fired after being cleared")
	}
}

func TestExecuteHooksIsConcurrencySafeWithAnObserver(t *testing.T) {
	t.Cleanup(func() { SetObserver(nil) })
	var mu sync.Mutex
	count := 0
	SetObserver(func(Firing) {
		mu.Lock()
		count++
		mu.Unlock()
	})

	reg := NewHookRegistry()
	reg.SetLabel("widgets")
	reg.RegisterHook(BeforeList, func(ctx context.Context, data any) error { return nil })

	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			if err := reg.ExecuteHooks(context.Background(), BeforeList, nil); err != nil {
				t.Errorf("execute: %v", err)
			}
		})
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if count != 32 {
		t.Errorf("observed %d firings, want 32", count)
	}
}
