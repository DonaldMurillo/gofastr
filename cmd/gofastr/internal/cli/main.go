package cli

import (
	"fmt"
	"io"
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

// Success, Fail, Info, Warn and InfoString print one status line. The
// mark's colour is the helper's own; every ARGUMENT is scrubbed of what
// a terminal acts on instead of printing (ScrubTerminalOutput: C0 but
// tab and newline, DEL, C1, bidi overrides and zero-width runes), so a
// path, an error or a value read from a cloned repo cannot write the
// clipboard (OSC 52), clear the screen or reorder the line. The format
// string is the caller's constant and is not scrubbed. Styling belongs
// in the format or the helper, never in an argument: an argument built
// with Bold or Green loses its escapes here.
func Success(format string, args ...interface{}) {
	fmt.Printf("  %s %s\n", Green("✓"), scrubbedf(format, args...))
}

func Fail(format string, args ...interface{}) {
	fmt.Printf("  %s %s\n", Red("✗"), scrubbedf(format, args...))
}

func Info(format string, args ...interface{}) {
	fmt.Printf("  %s %s\n", Yellow("→"), scrubbedf(format, args...))
}

// InfoString is info's message formatted for a caller-owned writer:
// for goroutines that must not read the os.Stdout global (see dev.go's
// crash watcher). Its arguments are scrubbed as Info's are.
func InfoString(format string, args ...interface{}) string {
	return fmt.Sprintf("  %s %s", Yellow("→"), scrubbedf(format, args...))
}

func Warn(format string, args ...interface{}) {
	fmt.Printf("  %s %s\n", Yellow("⚠"), scrubbedf(format, args...))
}

// scrubbedf is fmt.Sprintf with every argument passed through
// ScrubTerminalOutput after it is formatted with its own verb, width and
// flags, so %q, %v, %s and %x read the value the way they always did.
// Numbers and booleans carry no text to scrub and stay bare, which keeps
// a %*d width argument an int. With no arguments it forwards format and
// args unchanged, which is also what lets go vet read it, and the
// helpers above, as printf wrappers that check every call site's verbs.
func scrubbedf(format string, args ...interface{}) string {
	if len(args) == 0 {
		return fmt.Sprintf(format, args...)
	}
	return fmt.Sprintf(format, scrubArgs(args)...)
}

// scrubArgs returns a copy of args with each text-bearing argument
// wrapped in a scrubbedArg.
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

// scrubbedArg formats its value with the verb it was given, then drops
// the terminal-active runes from the result. fmt answers %T and %p from
// the operand itself before it asks a Formatter, so those two verbs read
// the wrapper; no helper call uses either, and a type or a pointer has
// no text to scrub.
type scrubbedArg struct{ v interface{} }

func (a scrubbedArg) Format(f fmt.State, verb rune) {
	_, _ = io.WriteString(f, ScrubTerminalOutput(fmt.Sprintf(fmt.FormatString(f, verb), a.v)))
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
