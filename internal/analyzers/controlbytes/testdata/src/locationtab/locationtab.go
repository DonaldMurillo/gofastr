// Package locationtab is framework/uihost's scrubCtl around 2a52a980.
// The scrub percent-encoded C0 and DEL but let tab through, so
// RedirectPattern("/old/{rest...}", "/{rest...}") answered
// GET /old/%09/evil.example with Location "/\t/evil.example", which a
// browser (deleting tab, CR and LF before it parses) follows to
// //evil.example. A scrub that exempts a C0 byte clears a log or header
// value, but not a Location.
package locationtab

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
)

// scrubCtlPreFix is scrubCtl before 2a52a980: tab passes.
func scrubCtlPreFix(s string) string {
	var b strings.Builder
	for i := range len(s) {
		if c := s[i]; c < 0x20 && c != '\t' || c == 0x7f {
			fmt.Fprintf(&b, "%%%02x", c)
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// scrubCtl is the fix: every C0 byte and DEL is encoded.
func scrubCtl(s string) string {
	var b strings.Builder
	for i := range len(s) {
		if c := s[i]; c < 0x20 || c == 0x7f {
			fmt.Fprintf(&b, "%%%02x", c)
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func redirectPreFix(w http.ResponseWriter, r *http.Request) {
	target := "/" + strings.TrimPrefix(r.URL.Path, "/old/")
	http.Redirect(w, r, scrubCtlPreFix(target), http.StatusPermanentRedirect) // want `controlbytes: request-derived value reaches http.Redirect Location unscrubbed`
}

func redirectFixed(w http.ResponseWriter, r *http.Request) {
	target := "/" + strings.TrimPrefix(r.URL.Path, "/old/")
	http.Redirect(w, r, scrubCtl(target), http.StatusPermanentRedirect)
}

// The Location header set by name is the same sink.
func locationHeader(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Location", scrubCtlPreFix(r.URL.Path)) // want `controlbytes: request-derived value reaches http.Header.Set/Add unscrubbed`
	w.Header().Set("Location", scrubCtl(r.URL.Path))
}

// Near misses: tab is a legal header byte and harmless in a log line,
// so the same scrub still clears every other sink.
func otherSinks(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Original-Path", scrubCtlPreFix(r.URL.Path))
	slog.Info("redirect", slog.String("path", scrubCtlPreFix(r.URL.Path)))
}
