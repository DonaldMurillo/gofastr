package event

import (
	"context"
	"sync"
	"testing"
)

func TestObserverSeesEveryEmission(t *testing.T) {
	t.Cleanup(func() { SetObserver(nil) })
	var mu sync.Mutex
	var seen []Emission
	SetObserver(func(e Emission) {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, e)
	})

	bus := NewEventBus()
	bus.On("order.placed", func(ctx context.Context, e Event) error { return nil })

	if err := bus.Emit(context.Background(), Event{Type: "order.placed"}); err != nil {
		t.Fatal(err)
	}
	// An event with no subscribers is still an emission. Coverage cares
	// that the type was published; the subscriber count is the extra
	// signal for "published, and nothing listening".
	if err := bus.Emit(context.Background(), Event{Type: "order.ignored"}); err != nil {
		t.Fatal(err)
	}
	if err := bus.EmitStrict(context.Background(), Event{Type: "order.placed"}); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 3 {
		t.Fatalf("emissions = %+v, want 3 (Emit, Emit, EmitStrict)", seen)
	}
	if seen[0].Type != "order.placed" || seen[0].Subscribers != 1 {
		t.Errorf("first emission = %+v", seen[0])
	}
	if seen[1].Type != "order.ignored" || seen[1].Subscribers != 0 {
		t.Errorf("unsubscribed emission = %+v", seen[1])
	}
	if seen[2].Type != "order.placed" {
		t.Errorf("EmitStrict emission = %+v", seen[2])
	}
}

func TestObserverDoesNotDisturbTheInternalTap(t *testing.T) {
	// setTap is a single slot the outbox/fanout bridge owns. Coverage
	// recording must ride alongside it, not compete for it.
	t.Cleanup(func() { SetObserver(nil) })
	observed := 0
	SetObserver(func(Emission) { observed++ })

	bus := NewEventBus()
	tapped := 0
	clear := bus.setTap(func(context.Context, Event) { tapped++ })
	defer clear()

	if err := bus.Emit(context.Background(), Event{Type: "x"}); err != nil {
		t.Fatal(err)
	}
	if tapped != 1 {
		t.Errorf("tap fired %d times", tapped)
	}
	if observed != 1 {
		t.Errorf("observer fired %d times", observed)
	}
}

func TestClearedObserverIsNotCalled(t *testing.T) {
	called := false
	SetObserver(func(Emission) { called = true })
	SetObserver(nil)

	bus := NewEventBus()
	if err := bus.Emit(context.Background(), Event{Type: "x"}); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Error("observer fired after being cleared")
	}
}

func TestEmitIsConcurrencySafeWithAnObserver(t *testing.T) {
	t.Cleanup(func() { SetObserver(nil) })
	var mu sync.Mutex
	count := 0
	SetObserver(func(Emission) {
		mu.Lock()
		count++
		mu.Unlock()
	})

	bus := NewEventBus()
	bus.On("tick", func(ctx context.Context, e Event) error { return nil })

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := bus.Emit(context.Background(), Event{Type: "tick"}); err != nil {
				t.Errorf("emit: %v", err)
			}
		}()
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if count != 32 {
		t.Errorf("observed %d emissions, want 32", count)
	}
}
