//go:build race

package framework

// raceEnabled reports whether this test binary was built with -race. A
// race-built binary reserves ThreadSanitizer's shadow memory at startup,
// which a data-memory cap (the Linux sandbox's RLIMIT_DATA) refuses, so it
// cannot run as a capped probe child.
const raceEnabled = true
