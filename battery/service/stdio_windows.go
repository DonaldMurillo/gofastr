//go:build windows

package service

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

var (
	kernel32             = windows.NewLazySystemDLL("kernel32.dll")
	user32               = windows.NewLazySystemDLL("user32.dll")
	procGetConsoleWindow = kernel32.NewProc("GetConsoleWindow")
	procShowWindow       = user32.NewProc("ShowWindow")
	procFreeConsole      = kernel32.NewProc("FreeConsole")
)

// redirectDaemonOutput sends the daemon's stdout and stderr to its log
// file. Task Scheduler captures neither, and a console binary started
// at logon gets a console window, which is hidden and detached here.
// Build with -ldflags=-H=windowsgui to avoid the window altogether.
func redirectDaemonOutput(logPath string) error {
	if err := ensurePrivateDir(filepath.Dir(logPath)); err != nil {
		return err
	}
	if err := trimLog(logPath); err != nil {
		return err
	}
	f, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("service: open log: %w", err)
	}
	h := windows.Handle(f.Fd())
	if err := windows.SetStdHandle(windows.STD_OUTPUT_HANDLE, h); err != nil {
		return fmt.Errorf("service: redirect stdout: %w", err)
	}
	if err := windows.SetStdHandle(windows.STD_ERROR_HANDLE, h); err != nil {
		return fmt.Errorf("service: redirect stderr: %w", err)
	}
	os.Stdout = f
	os.Stderr = f
	//gofastr:allow(discardederr) GetConsoleWindow has no error result; LazyProc.Call's error is GetLastError noise and the HWND alone answers
	if hwnd, _, _ := procGetConsoleWindow.Call(); hwnd != 0 {
		const swHide = 0
		procShowWindow.Call(hwnd, swHide)
		procFreeConsole.Call()
	}
	return nil
}
