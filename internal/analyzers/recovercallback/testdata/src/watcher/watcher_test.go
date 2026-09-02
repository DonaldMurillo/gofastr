package watcher

import (
	"context"
	"testing"
)

// Test-file posture: a panic failing the test is the intended
// outcome, so the rule stays out of _test.go entirely.
func TestCallHandlersDirectly(t *testing.T) {
	w := &Watcher{onCreate: func(string) {}}
	w.onCreate("x")
	var cancel context.CancelFunc
	cancel()
}
