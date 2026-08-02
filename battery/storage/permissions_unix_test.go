//go:build !windows

package storage_test

import "testing"

func requirePOSIXPermissions(t *testing.T) {
	t.Helper()
}
