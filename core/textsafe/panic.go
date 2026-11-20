package textsafe

import (
	"context"
	"log/slog"
	"runtime/debug"
)

// LogPanic recovers a panic and logs msg, the Recovered value and the stack; call it directly as a defer.
func LogPanic(ctx context.Context, msg string) {
	if recovered := recover(); recovered != nil {
		slog.Default().ErrorContext(ctx, msg,
			"panic", Recovered(recovered),
			"stack", string(debug.Stack()),
		)
	}
}
