//go:build linux

package procalive

import (
	"bytes"
	"os"
	"strconv"
	"strings"
)

// statFields reads /proc/<pid>/stat's fields from the process state on,
// or nil when it cannot be read. The command name (field 2) sits in
// parentheses and may hold spaces or ")", so count fields from the last ")".
func statFields(pid int) []string {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return nil
	}
	i := bytes.LastIndexByte(b, ')')
	if i < 0 {
		return nil
	}
	return strings.Fields(string(b[i+1:]))
}

// startTime is field 22 of /proc/<pid>/stat, the clock ticks after boot
// at which the process started, or 0 when it cannot be read.
func startTime(pid int) uint64 {
	// fields[0] is field 3 (state), so field 22 is fields[19].
	fields := statFields(pid)
	if len(fields) < 20 {
		return 0
	}
	v, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// zombie reports whether pid exited and only waits to be reaped: state Z.
func zombie(pid int) bool {
	fields := statFields(pid)
	return len(fields) > 0 && fields[0] == "Z"
}
