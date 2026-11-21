package proctree

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const observeBeforeReap = true

type commandTree struct {
	job      windows.Handle
	attached bool
}

func startCommand(cmd *exec.Cmd) (commandTree, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return commandTree{}, fmt.Errorf("create command job: %w", err)
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(job)
		return commandTree{}, fmt.Errorf("configure command job: %w", err)
	}

	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_SUSPENDED}
	if err := cmd.Start(); err != nil {
		_ = windows.CloseHandle(job)
		return commandTree{}, err
	}

	tree := commandTree{job: job}
	err = withProcessHandle(cmd.Process, func(handle windows.Handle) error {
		return windows.AssignProcessToJobObject(job, handle)
	})
	if err != nil {
		return tree, fmt.Errorf("assign command to job: %w", err)
	}
	tree.attached = true
	if err := resumeMainThread(uint32(cmd.Process.Pid)); err != nil {
		return tree, fmt.Errorf("resume command: %w", err)
	}
	return tree, nil
}

func resumeMainThread(pid uint32) error {
	// os/exec closes the initial thread handle, so reopen it by owner PID.
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	for err = windows.Thread32First(snapshot, &entry); err == nil; err = windows.Thread32Next(snapshot, &entry) {
		if entry.OwnerProcessID == pid {
			thread, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
			if err != nil {
				return err
			}
			defer windows.CloseHandle(thread)
			count, err := windows.ResumeThread(thread)
			if err == nil && count != 1 {
				return fmt.Errorf("unexpected initial thread suspend count %d", count)
			}
			return err
		}
	}
	return err
}

func observeExit(process *os.Process) error {
	err := withProcessHandle(process, func(handle windows.Handle) error {
		_, err := windows.WaitForSingleObject(handle, windows.INFINITE)
		return err
	})
	if err != nil {
		return fmt.Errorf("observe command exit: %w", err)
	}
	return nil
}

func withProcessHandle(process *os.Process, call func(windows.Handle) error) error {
	var callErr error
	if err := process.WithHandle(func(handle uintptr) { callErr = call(windows.Handle(handle)) }); err != nil {
		return err
	}
	return callErr
}

func (tree commandTree) stop(process *os.Process) {
	if err := windows.CloseHandle(tree.job); err != nil || !tree.attached {
		_ = process.Kill()
	}
}
