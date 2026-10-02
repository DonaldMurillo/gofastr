//go:build !unix && !windows

package axecov

import "os"

// lockFile is a no-op where the platform has no advisory file lock
// (js/wasm, plan9): writers there are one process by construction.
func lockFile(*os.File) error { return nil }

func unlockFile(*os.File) error { return nil }
