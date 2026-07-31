//go:build windows

package tui

import "os"

// notifyWindowResize is a no-op on Windows: the platform has no SIGWINCH
// signal. The TUI reads the terminal size once at startup; live
// re-layout on resize is not wired on Windows (which ships the web
// client rather than the raw-mode TUI). The signature mirrors the Unix
// build so terminal.go stays platform-agnostic.
func notifyWindowResize(_ chan<- os.Signal) {}
