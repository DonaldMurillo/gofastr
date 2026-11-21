//go:build !unix

package proctree

import "time"

// KillUnder is a no-op where the cwd scan is unavailable.
func KillUnder(string, time.Duration) {}
