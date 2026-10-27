package framework

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/moduleproto"
)

// exitedChild is a RunningChild whose process has already exited. It has
// a field so two of them never share an address.
type exitedChild struct{ _ int }

func (*exitedChild) Codec() *moduleproto.Codec     { return nil }
func (*exitedChild) Stderr() *moduleproto.RingSink { return nil }
func (*exitedChild) Pid() int                      { return -1 }
func (*exitedChild) ProcessGroup() int             { return 0 }
func (*exitedChild) CloseStdin() error             { return nil }
func (*exitedChild) Kill() error                   { return nil }
func (*exitedChild) Wait() error                   { return nil }

// A lease drain can settle a slot and start a replacement before the
// old child's exit watcher runs. That late watcher must leave the
// replacement's handles and the Ready state alone.
func TestLateExitKeepsReplacement(t *testing.T) {
	old, replacement := &exitedChild{}, &exitedChild{}
	sl := &moduleSlot{
		name:       "m",
		sup:        &ProcessModuleSupervisor{logf: t.Logf},
		wake:       make(chan struct{}, 1),
		state:      StateReady,
		child:      replacement,
		instanceID: "new",
	}
	sl.watchExit(old, "old")
	if sl.child != RunningChild(replacement) {
		t.Errorf("late watcher cleared the replacement child")
	}
	if sl.state != StateReady {
		t.Errorf("state = %v, want Ready", sl.state)
	}

	// The watcher of the child the slot holds still records its crash.
	sl.watchExit(replacement, "new")
	if sl.child != nil || sl.state != StateCrashed {
		t.Errorf("own exit: child = %v, state = %v, want nil and Crashed", sl.child, sl.state)
	}
}

// taggedChild is a RunningChild a custom runner may return by value. Its
// slice field makes it uncomparable, so == on two of them panics.
type taggedChild struct {
	*exitedChild
	tags []string
}

// A crash is recorded in the same critical section that clears the
// child. Between the two the slot was a childless Ready, which the
// supervise loop reads as a lease drain to recover and respawns, and the
// watcher then marked that replacement Crashed. An uncomparable child
// must not panic the watcher either.
func TestCrashLeavesNoChildlessReady(t *testing.T) {
	child := taggedChild{exitedChild: &exitedChild{}, tags: []string{"a"}}
	var sl *moduleSlot
	sup := &ProcessModuleSupervisor{logf: func(format string, args ...any) {
		sl.mu.Lock()
		defer sl.mu.Unlock()
		if sl.state == StateReady && sl.child == nil {
			t.Errorf("the slot was a childless Ready while the watcher logged")
		}
	}}
	sl = &moduleSlot{name: "m", sup: sup, wake: make(chan struct{}, 1), state: StateReady, child: child, instanceID: "i1"}
	sl.watchExit(child, "i1")
	if sl.child != nil || sl.state != StateCrashed {
		t.Errorf("child = %v, state = %v, want nil and Crashed", sl.child, sl.state)
	}
}
