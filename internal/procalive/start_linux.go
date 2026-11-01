//go:build linux

package procalive

import (
	"bytes"
	"os"
	"strconv"
	"strings"
)

// startTime is field 22 of /proc/<pid>/stat, the clock ticks after boot
// at which the process started, or 0 when it cannot be read.
func startTime(pid int) uint64 {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0
	}
	// The command name (field 2) sits in parentheses and may hold
	// spaces or ")", so count fields from the last ")".
	i := bytes.LastIndexByte(b, ')')
	if i < 0 {
		return 0
	}
	fields := strings.Fields(string(b[i+1:]))
	// fields[0] is field 3 (state), so field 22 is fields[19].
	if len(fields) < 20 {
		return 0
	}
	v, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return 0
	}
	return v
}
