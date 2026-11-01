//go:build darwin

package procalive

import "golang.org/x/sys/unix"

// startTime is the process's p_starttime from kern.proc.pid, packed as
// microseconds, or 0 when it cannot be read.
func startTime(pid int) uint64 {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || kp.Proc.P_pid != int32(pid) {
		return 0
	}
	tv := kp.Proc.P_starttime
	if tv.Sec <= 0 {
		return 0
	}
	return uint64(tv.Sec)*1_000_000 + uint64(tv.Usec)
}
