package framework

import "testing"

// An in-process listener (a mobile shell's "memory" or "pipe" network)
// counts as local-only, with no bound host.
func TestSweepServeInProcessListenerIsLocalOnly(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	if app.servesLocalOnly() {
		t.Fatal("an App with no listener and no service mark reads as local-only")
	}
	app.serverMu.Lock()
	app.boundInProcess = inProcessNetwork("memory")
	app.serverMu.Unlock()
	if !app.servesLocalOnly() {
		t.Fatal("an in-process listener does not read as local-only")
	}
}
