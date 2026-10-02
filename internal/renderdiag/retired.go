package renderdiag

import (
	"context"
	"log/slog"
)

// retiredReporterKey is deliberately NOT observerKey: a retired markup
// name is a migration finding, not a render failure, and must never
// flip a test response to 500 the way a reported panic does.
type retiredReporterKey struct{}

// WithRetiredReporter installs a request-local reporter for retired
// markup names found in a rendered response. Render recovery is
// unaffected; see ReportRetired.
func WithRetiredReporter(ctx context.Context, report func(string)) context.Context {
	return context.WithValue(ctx, retiredReporterKey{}, report)
}

// HasRetiredReporter reports whether ctx carries a retired-markup
// reporter, so a caller can pick its logging posture (a dev-mode console
// warning dedupes per path; a test reporter must see every finding).
func HasRetiredReporter(ctx context.Context) bool {
	_, ok := ctx.Value(retiredReporterKey{}).(func(string))
	return ok
}

// ReportRetired delivers one retired-markup finding (already formatted
// for the reader) to the request's reporter, or logs it at warn level
// when none is installed — e.g. an httptest server built by hand, or
// `gofastr dev` before its dedupe layer.
func ReportRetired(ctx context.Context, message string) {
	if report, ok := ctx.Value(retiredReporterKey{}).(func(string)); ok {
		report(message)
		return
	}
	slog.WarnContext(ctx, message)
}
