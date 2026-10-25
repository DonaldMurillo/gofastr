package cli

import (
	"fmt"
	"os"
)

// Version info: main sets these from its own vars, the ones -ldflags
// overrides at build time (-X main.version=…), before dispatching.
var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
)

// OsExit is an indirection over os.Exit so tests can observe the exit
// code of CLI entry points without terminating the test binary. In
// production it is os.Exit verbatim. Behavior is identical. Tests swap
// it (and restore via the helper) to capture the requested exit status.
var OsExit = os.Exit

// StdoutIsTTY is true when os.Stdout is connected to a terminal. ANSI
// color helpers below return their input unchanged when this is false,
// so `gofastr docs | less` or `gofastr docs > out.txt` don't get
// littered with escape sequences. Evaluated once at startup;
// re-opening stdout post-init doesn't re-detect.
var StdoutIsTTY = func() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}()

// ANSI color helpers
func Green(msg string) string {
	if !StdoutIsTTY {
		return msg
	}
	return "\033[32m" + msg + "\033[0m"
}

func Red(msg string) string {
	if !StdoutIsTTY {
		return msg
	}
	return "\033[31m" + msg + "\033[0m"
}

func Yellow(msg string) string {
	if !StdoutIsTTY {
		return msg
	}
	return "\033[33m" + msg + "\033[0m"
}

func Bold(msg string) string {
	if !StdoutIsTTY {
		return msg
	}
	return "\033[1m" + msg + "\033[0m"
}

func Dim(msg string) string {
	if !StdoutIsTTY {
		return msg
	}
	return "\033[2m" + msg + "\033[0m"
}

func Success(format string, args ...interface{}) {
	fmt.Printf("  %s %s\n", Green("✓"), fmt.Sprintf(format, args...))
}

func Fail(format string, args ...interface{}) {
	fmt.Printf("  %s %s\n", Red("✗"), fmt.Sprintf(format, args...))
}

func Info(format string, args ...interface{}) {
	fmt.Printf("  %s %s\n", Yellow("→"), fmt.Sprintf(format, args...))
}

// InfoString is info's message formatted for a caller-owned writer:
// for goroutines that must not read the os.Stdout global (see dev.go's
// crash watcher).
func InfoString(format string, args ...interface{}) string {
	return fmt.Sprintf("  %s %s", Yellow("→"), fmt.Sprintf(format, args...))
}

func Warn(format string, args ...interface{}) {
	fmt.Printf("  %s %s\n", Yellow("⚠"), fmt.Sprintf(format, args...))
}

func HasHelpFlag(args []string) bool {
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			return true
		}
	}
	return false
}

func Min(vals ...int) int {
	m := vals[0]
	for _, v := range vals[1:] {
		if v < m {
			m = v
		}
	}
	return m
}
