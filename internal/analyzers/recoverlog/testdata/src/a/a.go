// Package a holds the recoverlog fixtures reduced from the real sites:
// the ten oracle shapes the 2026-09-07 red round pinned, the fix
// postures (scrubControlBytes / Recovered in front of the sink), and
// the quiet postures the repo already spells.
package a

import (
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
)

// ---- oracle shapes (each fires) -------------------------------------------

// auditPanic is battery/auth audit.go emitSecurity: the raw value into
// a package-level slog.Warn.
func auditPanic(ev string) {
	defer func() {
		if r := recover(); r != nil {
			slog.Warn("auth: audit sink panic recovered; event lost", "kind", ev, "panic", r) // want `recoverlog: recover\(\) value reaches slog\.Debug/Info/Warn/Error key-value unscrubbed`
		}
	}()
	maybePanic()
}

// sprintPanic is battery/auth core.go deliverRegisterDuplicateNotice:
// fmt.Sprint of the value into the attr.
func sprintPanic(holder string) {
	defer func() {
		if p := recover(); p != nil {
			slog.Warn("register duplicate-notice sender panicked", "panic", fmt.Sprint(p)) // want `recoverlog: recover\(\) value reaches slog\.Debug/Info/Warn/Error key-value unscrubbed`
		}
	}()
	maybePanic()
}

// defaultLoggerPanic is battery/semantic watcher.go safeMetadata and
// core/stream websocket.go: slog.Default().Error with the raw value.
func defaultLoggerPanic(path string) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Default().Error("semantic: MetadataFunc panicked", "path", path, "panic", rec) // want `recoverlog: recover\(\) value reaches slog\.Debug/Info/Warn/Error key-value unscrubbed`
		}
	}()
	maybePanic()
}

type server struct {
	log *slog.Logger
}

// fieldLoggerPanic is core/a2a exec.go invoke: the server's own logger.
func (s *server) fieldLoggerPanic(taskID string) (err error) {
	defer func() {
		if p := recover(); p != nil {
			s.log.Error("a2a: skill handler panicked", "taskId", taskID, "panic", p) // want `recoverlog: recover\(\) value reaches slog\.Debug/Info/Warn/Error key-value unscrubbed`
			err = errors.New("internal error")
		}
	}()
	maybePanic()
	return nil
}

// attrPanic is core/fanout subscriber_queue.go deliver and core/mcp
// runCallGate: slog.Any carrying the value as an attr.
func attrPanic(fn func()) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("fanout: subscriber panicked", slog.Any("panic", r)) // want `recoverlog: recover\(\) value reaches slog\.String/slog\.Any unscrubbed`
		}
	}()
	fn()
}

// attrTwoPanic is core/mcp checkServerGate: two attrs, the value in
// the second.
func attrTwoPanic(name string) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("panic in MCP server gate recovered", slog.String("tool", name), slog.Any("panic", rec)) // want `recoverlog: recover\(\) value reaches slog\.String/slog\.Any unscrubbed`
			err = errors.New("internal tool error")
		}
	}()
	maybePanic()
	return nil
}

// truncatePanic is core/handler handler.go HandlerAdapter and
// core/middleware metrics.go runCollectorSafely: a length cap around
// the rendered value is not a scrub.
func truncatePanic() {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Default().Error("panic recovered in handler", "error", truncateLog(fmt.Sprint(rec), 4096)) // want `recoverlog: recover\(\) value reaches slog\.Debug/Info/Warn/Error key-value unscrubbed`
		}
	}()
	maybePanic()
}

func truncateLog(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

// stdLogPanic is cmd/kiln agent_watcher.go safeBuildArgs: a *log.Logger
// Printf with the raw value (a genuine sibling of the round's oracles).
func stdLogPanic(logger *log.Logger, name string) (argv []string) {
	defer func() {
		if rec := recover(); rec != nil {
			logger.Printf("agent: adapter %q panicked building argv: %v", name, rec) // want `recoverlog: recover\(\) value reaches std log print unscrubbed`
			argv = nil
		}
	}()
	maybePanic()
	return nil
}

// helperHop is the one-hop arm: the value handed to a same-package
// helper whose own body sinks the parameter raw.
func helperHop() {
	defer func() {
		if r := recover(); r != nil {
			logPanic(r) // want `recoverlog: recover\(\) value passed to logPanic reaches its log sink unscrubbed`
		}
	}()
	maybePanic()
}

func logPanic(v any) {
	slog.Error("panicked", "value", v)
}

// errorfChain: the value wrapped into an error and then logged.
func errorfChain() {
	defer func() {
		if r := recover(); r != nil {
			err := fmt.Errorf("handler panicked: %v", r)
			slog.Error("panic", "err", err) // want `recoverlog: recover\(\) value reaches slog\.Debug/Info/Warn/Error key-value unscrubbed`
		}
	}()
	maybePanic()
}

// stderrPanic: fmt.Fprintln to os.Stderr.
func stderrPanic() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintln(os.Stderr, "panic:", r) // want `recoverlog: recover\(\) value reaches stdout/stderr print unscrubbed`
		}
	}()
	maybePanic()
}

// ---- fix postures (quiet) --------------------------------------------------

// scrubbedPanic is core/middleware recovery.go RecoveryFn: the value
// through a scrub-named call before the truncate and the sink.
func scrubbedPanic(logger *slog.Logger) {
	defer func() {
		if err := recover(); err != nil {
			logger.Error("panic recovered",
				"error", truncate(scrubControlBytes(fmt.Sprint(err)), 4096))
		}
	}()
	maybePanic()
}

// recoveredSpelling is the core/textsafe.Recovered fix spelling.
func recoveredSpelling() {
	defer func() {
		if p := recover(); p != nil {
			slog.Warn("panicked", "panic", Recovered(p))
		}
	}()
	maybePanic()
}

// quotePanic: a %q format pre-escapes the bytes; the format is the
// scrub.
func quotePanic() {
	defer func() {
		if r := recover(); r != nil {
			slog.Warn("panicked", "panic", fmt.Sprintf("%q", fmt.Sprint(r)))
		}
	}()
	maybePanic()
}

// helperHopScrubbed: the helper scrubs before its sink, so the call
// site is quiet.
func helperHopScrubbed() {
	defer func() {
		if r := recover(); r != nil {
			logPanicScrubbed(r)
		}
	}()
	maybePanic()
}

func logPanicScrubbed(v any) {
	slog.Error("panicked", "value", scrubControlBytes(fmt.Sprint(v)))
}

// ---- quiet postures --------------------------------------------------------

// noSink: the value becomes a returned error, never a log line
// (core/a2a's errHandlerPanicked posture).
func noSink() (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = errors.New("a2a: skill handler panicked")
		}
	}()
	maybePanic()
	return nil
}

// storedValue: the value lands in a struct field, not a sink — the
// carrying-struct seam is controlbytes' business.
type report struct {
	Value string
}

func storedValue() *report {
	defer func() {
		_ = recover()
	}()
	maybePanic()
	return &report{}
}

// boolResult: a comparison on the value is not a carrier.
func boolResult() bool {
	defer func() {
		_ = recover()
	}()
	maybePanic()
	return true
}

// plainWork: recover with no log at all.
func plainWork() {
	defer func() {
		if r := recover(); r != nil {
			_ = r
		}
	}()
	maybePanic()
}

// ---- local scrub spellings (name-matched) ----------------------------------

func scrubControlBytes(s string) string { return stripUnsafeRunes(s) }

func Recovered(v any) string { return scrubControlBytes(fmt.Sprint(v)) }

func stripUnsafeRunes(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			continue
		}
		out = append(out, r)
	}
	return string(out)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

func maybePanic() {
	panic("boom\x1b[31m")
}

// bodylessDecl is the battery/desktop/internal/ffi shape that crashed
// run (checkFunc passed fn.Body == nil into newTaint's ast.Inspect):
// a top-level func declaration with no body, the //go:linkname stub
// (ffi.runtime_cgocall) and assembly-trampoline (ffi.callTrampoline)
// spelling. A bodyless function can hold no recover(); it must stay
// quiet, never panic.
func bodylessDecl()
