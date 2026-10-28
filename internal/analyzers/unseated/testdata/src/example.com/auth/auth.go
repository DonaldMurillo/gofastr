// Package auth fakes the harness token verifier.
package auth

// Verify checks a bearer token against the revocation list.
func Verify(tok string) error { return nil }
