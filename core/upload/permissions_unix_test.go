//go:build !windows

package upload_test

import "testing"

func requirePOSIXFileModes(t *testing.T) {
	t.Helper()
}
