// Package telemetrytest provides the file-seeding primitive shared by the
// telemetry tests: append a record to the on-disk file it belongs to,
// creating the file if needed.
package telemetrytest

import (
	"os"
	"testing"
	"time"

	itel "github.com/DonaldMurillo/gofastr/internal/telemetry"
)

// Append writes line to dir's file for records of kind stamped at,
// creating the file if needed — the seeding primitive of the file-backed
// telemetry tests.
func Append(t *testing.T, dir, kind string, at time.Time, line []byte) {
	t.Helper()
	// t.TempDir is private to the test, but the write still goes
	// through OpenRoot: a symlinked component must not silently
	// redirect the seed file outside the telemetry dir.
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	f, err := root.OpenFile(itel.FileName(kind, at), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Write(line); err != nil {
		t.Fatal(err)
	}
}
