package procalive

// Identity names one process: its pid and the kernel's record of when it
// started. A pid alone does not name a process for long: once the
// process exits the kernel hands the number to the next one, and a pid
// check then reports the newcomer as the process that left. The start
// value tells them apart.
//
// Start is the raw value the OS keeps (clock ticks since boot on Linux,
// the start timeval on macOS, the creation FILETIME on Windows). It is
// only ever compared for equality with a later reading of the same pid
// on the same machine, so no clock conversion is involved. Zero means
// it could not be read; such an Identity falls back to the pid alone.
type Identity struct {
	PID   int
	Start uint64
}

// Identify reads pid's start value. Call it while pid is known to be the
// process you mean (it just answered you, or it is this process): read
// later, it may already be a newcomer's.
func Identify(pid int) Identity {
	if pid <= 0 {
		return Identity{PID: pid}
	}
	return Identity{PID: pid, Start: readStart(pid)}
}

// readStart is the per-OS startTime, a variable so a test can make the
// OS refuse to describe a process.
var readStart = startTime

// Alive reports whether the process id names is still running: the pid
// is live and, when both readings exist, it started at the same moment.
// A start value that cannot be read now (another user's process the OS
// will not describe) keeps the pid's answer, so the check never calls a
// live process gone because it could not look.
func (id Identity) Alive() bool {
	if !Alive(id.PID) {
		return false
	}
	if id.Start == 0 {
		return true
	}
	now := readStart(id.PID)
	return now == 0 || now == id.Start
}
