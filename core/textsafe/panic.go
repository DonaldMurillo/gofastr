package textsafe

import (
	"context"
	"log/slog"
	"runtime/debug"
)

// LogPanic recovers a panic and logs msg, its Recovered value, and the
// stack through slog.Default().ErrorContext. Call it directly as a defer:
// defer textsafe.LogPanic(ctx, "observer panicked; ignored"). Wrapping it
// in another deferred function prevents recover from catching the panic.
// Without a panic it does nothing. After a panic the caller returns with
// its current named results; initialize them to fail closed where needed.
func LogPanic(ctx context.Context, msg string) {
	if recovered := recover(); recovered != nil {
		slog.Default().ErrorContext(ctx, msg,
			"panic", Recovered(recovered),
			"stack", string(debug.Stack()),
		)
	}
}
