package framework

import "testing"

func TestProcessModuleDelegationReleasePanicContained(t *testing.T) {
	called := false
	supervisor := &ProcessModuleSupervisor{}
	supervisor.releaseDelegationSafely(func() {
		called = true
		panic("release boom")
	})
	if !called {
		t.Fatal("delegation release was not called")
	}
}
