package n

import (
	"fmt"
	"net/http"
)

type sessionStore struct{}

func (sessionStore) Delete(tok string) error  { return nil }
func (sessionStore) Burn(tok string) error    { return nil }
func (sessionStore) Revoke(tok string) error  { return nil }
func (sessionStore) Cleanup(tok string) error { return nil }
func (sessionStore) Reset()                   {}

type logger struct{}

func (logger) Warn(msg string) error { return nil }

type counter struct{ n int }

func (c *counter) Reset() bool { c.n = 0; return true }

// Discard followed by return only: nothing is acknowledged.
func discardThenReturn(store sessionStore) error {
	_ = store.Delete("t")
	return nil
}

// Error write after the discard: the failure path stays visible.
func discardThenError(store sessionStore, w http.ResponseWriter) {
	_ = store.Delete("t")
	w.WriteHeader(http.StatusUnauthorized)
}

func discardThenErrorBody(store sessionStore, w http.ResponseWriter) {
	_ = store.Delete("t")
	http.Error(w, "store unavailable", http.StatusInternalServerError)
}

// Receiver not state-shaped.
func loggerDiscard(log logger, w http.ResponseWriter) {
	_ = log.Warn("x")
	w.WriteHeader(http.StatusOK)
}

func counterReset(c *counter, w http.ResponseWriter) {
	_ = c.Reset()
	w.WriteHeader(http.StatusOK)
}

// Package selector, mutator name absent: fmt.Fprintf is not a discard.
func fmtDiscard(w http.ResponseWriter) {
	_, _ = fmt.Fprintf(w, "ok")
	w.Write([]byte("done"))
}

// State-shaped receiver but a non-mutator method.
func nonMutatorMethod(store sessionStore, w http.ResponseWriter) {
	_ = store.Cleanup("t")
	w.WriteHeader(http.StatusOK)
}

// No ResponseWriter in scope at all (janitor shape).
func janitor(store sessionStore) {
	_ = store.Delete("t")
	_ = store.Burn("u")
}

// Write BEFORE the discard: the response does not cover the mutation.
func writeBeforeDiscard(store sessionStore, w http.ResponseWriter) {
	w.WriteHeader(http.StatusOK)
	_ = store.Delete("t")
}

// Void mutator called bare: nothing is discarded.
func voidReset(store sessionStore, w http.ResponseWriter) {
	store.Reset()
	w.WriteHeader(http.StatusOK)
}

// Non-constant status: success cannot be proven.
func dynamicStatus(store sessionStore, w http.ResponseWriter, code int) {
	_ = store.Delete("t")
	w.WriteHeader(code)
}

// Success write in another function: intra-procedural scope.
func helperWrite(w http.ResponseWriter) { w.WriteHeader(http.StatusOK) }

func discardCallsHelper(store sessionStore, w http.ResponseWriter) {
	_ = store.Delete("t")
	helperWrite(w)
}

// Success write inside a nested closure: separate function scope.
func writeInClosure(store sessionStore) func(http.ResponseWriter) {
	_ = store.Delete("t")
	return func(w http.ResponseWriter) { w.WriteHeader(http.StatusOK) }
}
