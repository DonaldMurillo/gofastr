package conformance

import "testing"

// TestInproc runs the conformance suite against the in-process
// transport. v0.1 only ships this transport at first-class; rest, ws,
// and mcpserver add their own *_test.go files that call Run with
// their own factory once they land.
func TestInproc(t *testing.T) {
	Run(t, InprocFactory)
}
