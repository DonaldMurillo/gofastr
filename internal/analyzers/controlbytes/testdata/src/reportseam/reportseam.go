// Package reportseam is battery/log reduced: recoveryMiddleware fills
// an ErrorReport with a recover() value and the request path, and the
// default reporter logs the report's fields. The reporter is a public
// seam (Plugin.Reporter) and must scrub what it is handed — the
// round-2 probes TestRecoveryLogRedScrubsPanic and
// TestErrorReporterRedScrubsAttrs. This package also pins the scoped
// recover() decision: the panic value counts when it marks the struct
// it lands in as carrying, and NOT as a source at an in-function log.
package reportseam

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
)

// ErrorReport mirrors battery/log's payload handed to an ErrorReporter.
type ErrorReport struct {
	Message string
	Error   string
	Stack   string
	Method  string
	Path    string
	Route   string
}

// Reporter receives application errors.
type Reporter interface {
	Report(r ErrorReport)
}

// recoverInto hands the panic value and the request path to the
// reporter, truncated but never scrubbed — recoveryMiddleware reduced.
func recoverInto(rep Reporter, w http.ResponseWriter, r *http.Request) {
	defer func() {
		if v := recover(); v != nil {
			rep.Report(ErrorReport{
				Message: "http.panic",
				Error:   truncateString(fmt.Sprint(v)),
				Path:    truncateString(r.URL.Path),
				Method:  r.Method,
			})
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	}()
}

// Report is SlogErrorReporter.Report reduced: every request-derived
// attr is logged verbatim.
func (s slogReporter) Report(r ErrorReport) {
	attrs := []slog.Attr{
		slog.String("panic", r.Error),   // want `controlbytes: request-derived value reaches slog.String/slog.Any unscrubbed`
		slog.String("method", r.Method), // want `controlbytes: request-derived value reaches slog.String/slog.Any unscrubbed`
		slog.String("path", r.Path),     // want `controlbytes: request-derived value reaches slog.String/slog.Any unscrubbed`
	}
	s.log.LogAttrs(context.Background(), slog.LevelError, "http.panic", attrs...)
}

// ReportScrubbed is the fixed spelling: the reporter scrubs what it was
// handed, whoever filled the report.
func (s slogReporter) ReportScrubbed(r ErrorReport) {
	attrs := []slog.Attr{
		slog.String("panic", scrubCtl(r.Error)),
		slog.String("method", scrubCtl(r.Method)),
		slog.String("path", scrubCtl(r.Path)),
	}
	s.log.LogAttrs(context.Background(), slog.LevelError, "http.panic", attrs...)
}

// directRecoverLog pins the scope decision: a recover() value logged
// in the function that recovered it is NOT a source at the sink — the
// carrying-struct seam above is where the panic value enters this
// rule. Quiet.
func directRecoverLog(rep Reporter) {
	defer func() {
		if v := recover(); v != nil {
			slog.Any("panic", v)
		}
	}()
	rep.Report(ErrorReport{Message: "x"})
}

// tokenRedirect pins the payload handoff: a whole same-package struct
// handed to a helper does not taint the call's result, so the flash
// token comes back clean whatever the record carried.
func tokenRedirect(w http.ResponseWriter, r *http.Request) {
	vals := map[string]string{"path": r.URL.Path}
	token := putToken(&ErrorReport{Path: vals["path"]})
	http.Redirect(w, r, "/back?e="+token, http.StatusSeeOther)
}

func putToken(e *ErrorReport) string { return "tok" }

// Note is a carrying struct with one request-derived field and one the
// package only ever fills with its own enums: only Path is a source.
type Note struct {
	Kind string
	Path string
}

func emitNote(w http.ResponseWriter, r *http.Request, sink func(Note)) {
	sink(Note{Kind: "login.succeeded", Path: r.URL.Path})
}

func logNote(n Note) {
	slog.String("kind", n.Kind) // quiet: Kind never carried request bytes
	slog.String("path", scrubCtl(n.Path))
}

type slogReporter struct {
	log *slog.Logger
}

// truncateString truncates but never scrubs: the name says nothing and
// the body never looks at bytes, so taint passes straight through.
func truncateString(s string) string {
	if len(s) > 4000 {
		return s[:4000] + "... (truncated)"
	}
	return s
}

func scrubCtl(s string) string {
	var b strings.Builder
	for i := range s {
		if c := s[i]; c >= 0x20 && c != 0x7f {
			b.WriteByte(c)
		}
	}
	return b.String()
}
