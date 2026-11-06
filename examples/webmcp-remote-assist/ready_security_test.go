package main

import "testing"

func TestReadySignalOnlyFromSupport(t *testing.T) {
	_, app := newTestApp(t)
	session := app.createSession()
	if !app.relaySignal(session, roleSupport, signalMessage{To: roleOperator, Type: "ready"}) {
		t.Fatal("support readiness was refused")
	}
	if app.relaySignal(session, roleOperator, signalMessage{To: roleSupport, Type: "ready"}) {
		t.Fatal("operator spoofed support readiness")
	}
}
