package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// StdoutIsTTY reports whether os.Stdout was a terminal; colour needs one.
var StdoutIsTTY = func() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}()

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

// Success prints a ✓ status line on stdout.
func Success(format string, args ...interface{}) {
	fmt.Printf("  %s %s\n", Green("✓"), scrubbedf(format, args...))
}

// Fail prints a ✗ status line on stderr.
func Fail(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "  %s %s\n", Red("✗"), scrubbedf(format, args...))
}

// UsageFail reports a command-line usage error as Fail does, then exits 2.
func UsageFail(format string, args ...interface{}) {
	Fail(format, args...)
	OsExit(2)
}

// Info prints a → status line on stdout.
func Info(format string, args ...interface{}) {
	fmt.Printf("  %s %s\n", Yellow("→"), scrubbedf(format, args...))
}

// InfoString returns Info's line without printing it, for a goroutine
// that must not read the os.Stdout global (dev's crash watcher).
func InfoString(format string, args ...interface{}) string {
	return fmt.Sprintf("  %s %s", Yellow("→"), scrubbedf(format, args...))
}

// Warn prints a ⚠ status line on stdout.
func Warn(format string, args ...interface{}) {
	fmt.Printf("  %s %s\n", Yellow("⚠"), scrubbedf(format, args...))
}

// Printf is fmt.Printf with every argument scrubbed for the terminal.
func Printf(format string, args ...interface{}) {
	fmt.Print(scrubbedf(format, args...))
}

// Print is fmt.Print with its output scrubbed for the terminal.
func Print(args ...interface{}) {
	fmt.Print(textsafe.ScrubTerminal(fmt.Sprint(args...)))
}

// Println is fmt.Println with its output scrubbed for the terminal.
func Println(args ...interface{}) {
	fmt.Print(textsafe.ScrubTerminal(fmt.Sprintln(args...)))
}

// Fprintf is fmt.Fprintf with every argument scrubbed for the terminal.
func Fprintf(w io.Writer, format string, args ...interface{}) {
	_, _ = io.WriteString(w, scrubbedf(format, args...))
}

// Fprintln is fmt.Fprintln with its output scrubbed for the terminal.
func Fprintln(w io.Writer, args ...interface{}) {
	_, _ = io.WriteString(w, textsafe.ScrubTerminal(fmt.Sprintln(args...)))
}

func scrubbedf(format string, args ...interface{}) string {
	if len(args) == 0 {
		return fmt.Sprintf(format, args...)
	}
	return fmt.Sprintf(format, scrubArgs(args)...)
}

func scrubArgs(args []interface{}) []interface{} {
	scrubbed := make([]interface{}, len(args))
	for i, a := range args {
		switch a.(type) {
		case nil, bool, int, int8, int16, int32, int64,
			uint, uint8, uint16, uint32, uint64, uintptr,
			float32, float64, complex64, complex128:
			scrubbed[i] = a
		default:
			scrubbed[i] = scrubbedArg{a}
		}
	}
	return scrubbed
}

type scrubbedArg struct{ v interface{} }

func (a scrubbedArg) Format(f fmt.State, verb rune) {
	_, _ = io.WriteString(f, textsafe.ScrubTerminal(fmt.Sprintf(fmt.FormatString(f, verb), a.v)))
}
