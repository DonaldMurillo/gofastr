package di

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type nestedDB struct{ dsn string }
type nestedRepo struct{ db *nestedDB }
type nestedScreen struct {
	Repo *nestedRepo `inject:""`
}

// within fails the test when fn does not return in time: the container
// held its mutex across a constructor, so one that resolved another
// service blocked forever (2026-10-27 bug bash S6).
func within(t *testing.T, name string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); fn() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s deadlocked: a constructor that resolves another service blocks the container", name)
	}
}

// A constructor may resolve another service, through Resolve or Inject.
func TestFactoryResolvesDependency(t *testing.T) {
	newContainer := func() *Container {
		c := NewContainer()
		if err := c.Provide(func() *nestedDB { return &nestedDB{dsn: "x"} }); err != nil {
			t.Fatal(err)
		}
		if err := c.Provide(func() *nestedRepo {
			var db *nestedDB
			if err := c.Resolve(&db); err != nil {
				t.Errorf("nested Resolve: %v", err)
			}
			return &nestedRepo{db: db}
		}); err != nil {
			t.Fatal(err)
		}
		return c
	}
	t.Run("resolve", func(t *testing.T) {
		c := newContainer()
		var repo *nestedRepo
		within(t, "Resolve", func() {
			if err := c.Resolve(&repo); err != nil {
				t.Errorf("Resolve: %v", err)
			}
		})
		if repo == nil || repo.db == nil || repo.db.dsn != "x" {
			t.Fatalf("repo = %+v, want one holding the resolved db", repo)
		}
	})
	t.Run("inject", func(t *testing.T) {
		c := newContainer()
		var s nestedScreen
		within(t, "Inject", func() {
			if err := c.Inject(&s); err != nil {
				t.Errorf("Inject: %v", err)
			}
		})
		if s.Repo == nil || s.Repo.db == nil {
			t.Fatalf("screen = %+v, want the repo injected with its db", s)
		}
	})
}

// Concurrent first resolves still call the constructor once and share
// its value.
func TestColdResolveCallsFactoryOnce(t *testing.T) {
	c := NewContainer()
	var calls atomic.Int32
	release := make(chan struct{})
	if err := c.Provide(func() *nestedDB {
		calls.Add(1)
		<-release
		return &nestedDB{dsn: "once"}
	}); err != nil {
		t.Fatal(err)
	}
	const n = 8
	got := make([]*nestedDB, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if i%2 == 0 {
				if err := c.Resolve(&got[i]); err != nil {
					t.Errorf("Resolve: %v", err)
				}
				return
			}
			var s struct {
				DB *nestedDB `inject:""`
			}
			if err := c.Inject(&s); err != nil {
				t.Errorf("Inject: %v", err)
			}
			got[i] = s.DB
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	within(t, "concurrent resolve", wg.Wait)
	if n := calls.Load(); n != 1 {
		t.Fatalf("constructor ran %d times, want 1", n)
	}
	for i, v := range got {
		if v != got[0] {
			t.Fatalf("caller %d got %p, want the shared %p", i, v, got[0])
		}
	}
}

// A constructor that panics releases its build: a waiter is not left
// blocked, and a later resolve runs the constructor again.
func TestPanickingFactoryReleasesBuild(t *testing.T) {
	c := NewContainer()
	var calls atomic.Int32
	if err := c.Provide(func() *nestedDB {
		if calls.Add(1) == 1 {
			panic("first boot fails")
		}
		return &nestedDB{dsn: "second"}
	}); err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("first Resolve did not panic")
			}
		}()
		var db *nestedDB
		_ = c.Resolve(&db)
	}()
	var db *nestedDB
	within(t, "Resolve after a panicking constructor", func() {
		if err := c.Resolve(&db); err != nil {
			t.Errorf("Resolve: %v", err)
		}
	})
	if db == nil || db.dsn != "second" {
		t.Fatalf("db = %+v, want the second constructor run", db)
	}
}

// A caller waiting on a constructor that panics gets an error, not the
// zero value as a success.
func TestWaiterOnPanickedBuildErrs(t *testing.T) {
	c := NewContainer()
	building, release := make(chan struct{}), make(chan struct{})
	var started sync.Once
	if err := c.Provide(func() *nestedDB {
		started.Do(func() { close(building) })
		<-release
		panic("boot fails")
	}); err != nil {
		t.Fatal(err)
	}
	go func() {
		defer func() { _ = recover() }()
		var db *nestedDB
		_ = c.Resolve(&db)
	}()
	<-building
	waited := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				waited <- fmt.Errorf("waiter panicked: %v", r)
			}
		}()
		var db *nestedDB
		if err := c.Resolve(&db); err != nil {
			waited <- nil
			return
		}
		waited <- fmt.Errorf("waiter Resolve = nil with db %v, want the build's error", db)
	}()
	time.Sleep(50 * time.Millisecond)
	close(release)
	select {
	case err := <-waited:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waiter never returned")
	}
}
