package framework

import "testing"

// A spawn's failure is written only while it is still the slot's current
// spawn, checked under the same lock as the write: a replacement that
// took the slot and reached Ready keeps Ready (2026-11-01 review). The
// read-then-write it replaced let an old spawn mark the replacement
// Crashed, and the restart started a child beside the live one.
func TestStaleSpawnFailKeepsReady(t *testing.T) {
	sup := &ProcessModuleSupervisor{logf: t.Logf}
	sl := &moduleSlot{name: "m", sup: sup, wake: make(chan struct{}, 1)}
	sl.spawnSeq = 2
	sl.state = StateReady
	if sl.failSpawn(1, "boom", false) {
		t.Fatal("a superseded spawn reported writing its failure")
	}
	if sl.state != StateReady {
		t.Fatalf("state = %v after a superseded spawn failed, want Ready", sl.state)
	}
	if !sl.failSpawn(2, "boom", true) || sl.state != StateFailed {
		t.Fatalf("the current spawn's failure was not written: state = %v", sl.state)
	}
}
