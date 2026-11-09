// Package contractstest builds filesystem fixtures for contract tooling tests.
package contractstest

import (
	"os"
	"path/filepath"
	"testing"
)

// ObstructApply leaves source readable by discovery, but outside the project
// root used by Report.Apply. The kernel refuses Apply's read through the link
// for every uid, including root; no permission bits or timing are involved.
func ObstructApply(t testing.TB, path string) {
	t.Helper()
	target := filepath.Join(t.TempDir(), "source.txt")
	if err := os.Rename(path, target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}
