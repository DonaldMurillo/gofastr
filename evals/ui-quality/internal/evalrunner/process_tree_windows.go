//go:build windows

package evalrunner

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// configureCommandCancellation makes context cancellation own the complete
// process tree on Windows. os/exec's default CommandContext cancellation kills
// only the direct process there; agent CLIs can leave browser helpers and
// `go run` servers behind after a timeout. taskkill /T is intentionally scoped
// to the PID that this harness just started.
func configureCommandCancellation(cmd *exec.Cmd) {
	cmd.WaitDelay = 5 * time.Second
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		pid := strconv.Itoa(cmd.Process.Pid)
		if err := exec.Command("taskkill.exe", "/PID", pid, "/T", "/F").Run(); err == nil {
			return nil
		}
		err := cmd.Process.Kill()
		if err == nil || errors.Is(err, os.ErrProcessDone) {
			return nil
		}
		return err
	}
}

// attachCommandProcessTree assigns a newly started agent to a Windows job with
// KILL_ON_JOB_CLOSE. Closing the job after the direct CLI exits also terminates
// any browser helper or `go run` server that it forgot to stop.
func attachCommandProcessTree(process *os.Process) (func(), error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	closeJob := func() { _ = windows.CloseHandle(job) }

	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		closeJob()
		return nil, fmt.Errorf("configure kill-on-close job: %w", err)
	}

	handle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(process.Pid))
	if err != nil {
		closeJob()
		return nil, err
	}
	defer windows.CloseHandle(handle)
	if err := windows.AssignProcessToJobObject(job, handle); err != nil {
		closeJob()
		return nil, err
	}
	return closeJob, nil
}
