package signal

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestSignalNewAndGet(t *testing.T) {
	s := New(42)
	if v := s.Get(); v != 42 {
		t.Errorf("expected 42, got %d", v)
	}
}

func TestSignalSet(t *testing.T) {
	s := New("hello")
	s.Set("world")
	if v := s.Get(); v != "world" {
		t.Errorf("expected world, got %s", v)
	}
}

func TestSignalSetSameValue(t *testing.T) {
	s := New(10)
	called := 0
	s.Subscribe(func(int) { called++ })
	s.Set(10) // same value
	if called != 0 {
		t.Errorf("subscriber should not be called for same value, got %d calls", called)
	}
	s.Set(20)
	if called != 1 {
		t.Errorf("subscriber should be called once for changed value, got %d calls", called)
	}
}

func TestSignalSubscribe(t *testing.T) {
	s := New(0)
	var received int
	s.Subscribe(func(v int) { received = v })
	s.Set(99)
	if received != 99 {
		t.Errorf("expected subscriber to receive 99, got %d", received)
	}
}

func TestSignalSubscribeUnsubscribe(t *testing.T) {
	s := New(0)
	called := 0
	unsub := s.Subscribe(func(int) { called++ })
	unsub()
	s.Set(1)
	if called != 0 {
		t.Errorf("unsubscribed listener should not be called, got %d calls", called)
	}
}

func TestSignalUpdate(t *testing.T) {
	s := New(5)
	s.Update(func(v int) int { return v * 2 })
	if v := s.Get(); v != 10 {
		t.Errorf("expected 10 after Update, got %d", v)
	}
}

func TestSignalMultipleSubscribers(t *testing.T) {
	s := New(0)
	var a, b int
	s.Subscribe(func(v int) { a = v })
	s.Subscribe(func(v int) { b = v })
	s.Set(7)
	if a != 7 || b != 7 {
		t.Errorf("expected both subscribers to get 7, got a=%d b=%d", a, b)
	}
}

func TestComputedBasic(t *testing.T) {
	s := New(3)
	c := NewComputed(func() int { return s.Get() * 2 })
	if v := c.Get(); v != 6 {
		t.Errorf("expected computed 6, got %d", v)
	}
}

func TestComputedUpdatesOnDependencyChange(t *testing.T) {
	s := New(5)
	c := NewComputed(func() int { return s.Get() * 3 })
	if v := c.Get(); v != 15 {
		t.Errorf("expected 15, got %d", v)
	}
	s.Set(10)
	if v := c.Get(); v != 30 {
		t.Errorf("expected 30 after dep change, got %d", v)
	}
}

func TestComputedChain(t *testing.T) {
	a := New(2)
	b := NewComputed(func() int { return a.Get() + 10 })
	c := NewComputed(func() int { return b.Get() * 5 })

	if v := c.Get(); v != 60 {
		t.Errorf("expected 60, got %d", v)
	}

	a.Set(3)
	if v := c.Get(); v != 65 {
		t.Errorf("expected 65 after chain update, got %d", v)
	}
}

func TestComputedAsDependency(t *testing.T) {
	a := New("hello")
	b := NewComputed(func() string { return a.Get() + " world" })
	c := NewComputed(func() int { return len(b.Get()) })

	if v := c.Get(); v != 11 {
		t.Errorf("expected 11, got %d", v)
	}

	a.Set("hi")
	if v := c.Get(); v != 8 {
		t.Errorf("expected 8 after update, got %d", v)
	}
}

func TestEffectBasic(t *testing.T) {
	called := 0
	dispose := Effect(func() { called++ })
	if called != 1 {
		t.Errorf("effect should run immediately, got %d calls", called)
	}
	dispose()
}

func TestEffectReRunsOnDependencyChange(t *testing.T) {
	s := New(0)
	called := 0
	dispose := Effect(func() {
		_ = s.Get()
		called++
	})
	if called != 1 {
		t.Errorf("expected initial run, got %d", called)
	}
	s.Set(1)
	if called != 2 {
		t.Errorf("expected re-run after change, got %d", called)
	}
	s.Set(2)
	if called != 3 {
		t.Errorf("expected second re-run, got %d", called)
	}
	dispose()
}

func TestEffectDispose(t *testing.T) {
	s := New(0)
	called := 0
	dispose := Effect(func() {
		_ = s.Get()
		called++
	})
	dispose()
	s.Set(1)
	if called != 1 {
		t.Errorf("effect should not run after dispose, got %d calls", called)
	}
}

func TestEffectMultipleDeps(t *testing.T) {
	a := New(1)
	b := New("x")
	called := 0
	dispose := Effect(func() {
		_ = a.Get()
		_ = b.Get()
		called++
	})
	if called != 1 {
		t.Errorf("expected initial run, got %d", called)
	}
	a.Set(2)
	if called != 2 {
		t.Errorf("expected re-run on a change, got %d", called)
	}
	b.Set("y")
	if called != 3 {
		t.Errorf("expected re-run on b change, got %d", called)
	}
	dispose()
}

func TestBatchBasic(t *testing.T) {
	s := New(0)
	var notifications atomic.Int32
	s.Subscribe(func(int) { notifications.Add(1) })

	Batch(func() {
		s.Set(1)
		s.Set(2)
		s.Set(3)
	})

	// After batch, only one notification should fire (last set wins — but all 3 sets
	// are distinct values, so each gets queued and fired).
	// Actually, each Set queues a notification. So we expect 3 notifications.
	if n := notifications.Load(); n != 3 {
		t.Errorf("expected 3 notifications after batch, got %d", n)
	}
}

func TestBatchDeduplication(t *testing.T) {
	s := New(0)
	var notifications atomic.Int32
	s.Subscribe(func(int) { notifications.Add(1) })

	Batch(func() {
		s.Set(1)
		s.Set(1) // same value — should be deduplicated
		s.Set(2)
	})

	// Set(1) queues notification, Set(1) same value skipped, Set(2) queues notification
	if n := notifications.Load(); n != 2 {
		t.Errorf("expected 2 notifications (deduplicated same value), got %d", n)
	}
}

func TestConcurrentAccess(t *testing.T) {
	s := New(0)
	var wg sync.WaitGroup

	// Concurrent writers
	for i := range 100 {
		wg.Add(1)
		go func(v int) {
			defer wg.Done()
			s.Set(v)
		}(i)
	}

	// Concurrent readers
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.Get()
		}()
	}

	wg.Wait()
	// If we get here without panicking or deadlocking, the test passes.
}

func TestSignalString(t *testing.T) {
	s := New(42)
	if str := s.String(); str != "42" {
		t.Errorf("expected String() = 42, got %s", str)
	}

	s2 := New("hello")
	if str := s2.String(); str != "hello" {
		t.Errorf("expected String() = hello, got %s", str)
	}
}

func TestComputedGetTracksDependency(t *testing.T) {
	a := New(1)
	b := NewComputed(func() int { return a.Get() * 2 })

	// b.Get() should be trackable as a dependency of an effect
	effectRan := 0
	dispose := Effect(func() {
		_ = b.Get()
		effectRan++
	})
	if effectRan != 1 {
		t.Errorf("expected initial effect run, got %d", effectRan)
	}

	a.Set(5)
	if effectRan != 2 {
		t.Errorf("expected effect re-run when computed dep changes, got %d", effectRan)
	}

	if v := b.Get(); v != 10 {
		t.Errorf("expected computed value 10, got %d", v)
	}

	dispose()
}

func TestComputedUnchangedValue(t *testing.T) {
	s := New(5)
	c := NewComputed(func() int { return s.Get() % 2 }) // 5%2=1
	notifications := 0
	c.Subscribe(func(int) { notifications++ })

	s.Set(7) // 7%2=1 — same computed value
	if notifications != 0 {
		t.Errorf("computed should not notify when derived value unchanged, got %d", notifications)
	}

	s.Set(4) // 4%2=0 — different
	if notifications != 1 {
		t.Errorf("computed should notify when derived value changes, got %d", notifications)
	}
}

func TestSignalValueAlias(t *testing.T) {
	s := New("test")
	if v := s.Value(); v != "test" {
		t.Errorf("expected Value() to return 'test', got %s", v)
	}
}

func TestComputedDispose(t *testing.T) {
	s := New(1)
	c := NewComputed(func() int { return s.Get() * 10 })
	if v := c.Get(); v != 10 {
		t.Errorf("expected 10, got %d", v)
	}
	c.Dispose()
	s.Set(2)
	// After dispose, computed should NOT recompute
	if v := c.Get(); v != 10 {
		t.Errorf("expected 10 (stale after dispose), got %d", v)
	}
}
