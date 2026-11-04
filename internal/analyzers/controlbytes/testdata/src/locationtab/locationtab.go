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

// keepSideCtl is the same leak spelled on the keep side of the filter
// (`c >= 0x20 || c == '\t'` keeps the byte): the exemption test read
// only != comparisons, so this filter cleared a Location it still
// carries tab into.
func keepSideCtl(s string) string {
	var b strings.Builder
	for i := range len(s) {
		if c := s[i]; c >= 0x20 || c == '\t' {
			b.WriteByte(c)
		}
	}
	return b.String()
}

func keepSideRedirect(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, keepSideCtl(r.URL.Path), http.StatusPermanentRedirect) // want `controlbytes: request-derived value reaches http.Redirect Location unscrubbed`
}

func keepSideHeader(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Location", keepSideCtl(r.URL.Path)) // want `controlbytes: request-derived value reaches http.Header.Set/Add unscrubbed`
}

// refuseNewlineCtl is a REFUSAL, not a carve-out: the == names the
// byte it rejects outright, and the boundary loop beside it strips
// every C0 byte including tab. Nothing is exempt, so a Location
// through it stays quiet — an == counts only beside a printable
// boundary comparison in the same boolean chain.
func refuseNewlineCtl(s string) (string, error) {
	for i := range len(s) {
		if s[i] == '\n' {
			return "", fmt.Errorf("control byte in %q", s)
		}
	}
	var b strings.Builder
	for i := range len(s) {
		if c := s[i]; c >= 0x20 {
			b.WriteByte(c)
		}
	}
	return b.String(), nil
}

func refuseNewlineRedirect(w http.ResponseWriter, r *http.Request) {
	target, err := refuseNewlineCtl(r.URL.Path)
	if err != nil {
		http.Error(w, "bad path", http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, target, http.StatusPermanentRedirect)
}
