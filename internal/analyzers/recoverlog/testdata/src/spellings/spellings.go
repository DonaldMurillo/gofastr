// Package spellings holds the log sinks recoverlog's doc names whose
// arms no fixture exercised (cmd/mutate: both the always- and the
// never-mutant of the package-level std log and slog.Log arms
// survived), plus the std log Fatal/Panic families, which log the value
// the same way before they exit or panic.
package spellings

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
)

func pkgLog() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("panic: %v", r) // want `recover\(\) value reaches std log print unscrubbed`
		}
	}()
}

func slogLog(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			slog.Log(ctx, slog.LevelError, "panic", "value", r) // want `recover\(\) value reaches slog.Log key-value unscrubbed`
		}
	}()
}

func fatal() {
	defer func() {
		if r := recover(); r != nil {
			log.Fatalf("panic: %v", r) // want `recover\(\) value reaches std log print unscrubbed`
		}
	}()
}

func loggerPanic(l *log.Logger) {
	defer func() {
		if r := recover(); r != nil {
			l.Panicln("panic:", r) // want `recover\(\) value reaches std log print unscrubbed`
		}
	}()
}

func stderrWrite() {
	defer func() {
		if r := recover(); r != nil {
			os.Stderr.WriteString("panic: " + fmt.Sprint(r) + "\n") // want `recover\(\) value reaches stdout/stderr print unscrubbed`
		}
	}()
}

func stderrWriteAsserted() {
	defer func() {
		if r := recover(); r != nil {
			msg, _ := r.(string)
			fmt.Fprintln(os.Stderr, "panic:", msg) // want `recover\(\) value reaches stdout/stderr print unscrubbed`
		}
	}()
}

// Silent: the level argument of slog.Log is not the value.
func slogLogQuiet(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			_ = r
			slog.Log(ctx, slog.LevelError, "panic recovered")
		}
	}()
}
