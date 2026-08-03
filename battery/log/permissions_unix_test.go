//go:build !windows

package log

import "testing"

func requirePOSIXFileModes(t *testing.T) {
	t.Helper()
}
