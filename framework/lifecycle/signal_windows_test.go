//go:build windows

package lifecycle_test

import "testing"

func requireSIGTERMForTest(t *testing.T) {
	t.Helper()
	t.Skip("self-signalling SIGTERM is not portable on Windows")
}

func sendSIGTERMForTest(t *testing.T) {
	t.Helper()
}
