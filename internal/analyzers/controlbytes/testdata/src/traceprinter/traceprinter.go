// Package traceprinter is a NOVEL instantiation of the shape: a CLI
// debug handler (never in this repo) echoing request parts onto the
// terminal and into a security response header, with the cleaned
// spellings beside them.
package traceprinter

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
)

type DebugHandler struct {
	allowedOrigins map[string]bool
}

func (d *DebugHandler) dump(r *http.Request) {
	who := r.RemoteAddr
	where := r.URL.Path
	asked := r.URL.Query().Get("explain")
	fmt.Fprintf(os.Stderr, "debug: %s asked about %s (%s)\n", who, where, asked) // want `controlbytes: request-derived value reaches stdout/stderr print unscrubbed`
}

func (d *DebugHandler) dumpScrubbed(r *http.Request) {
	fmt.Fprintf(os.Stderr, "debug: %s\n", redactCtl(r.RemoteAddr))
}

// corsAllowlisted is QUIET by design: the origin reached the sink only
// after passing an allowlist lookup keyed by the value itself — a
// control byte in the header cannot be in the configured set, so the
// guard is the sanitizer (core/middleware's CORS and battery/auth's
// BFF spell it exactly this way).
func (d *DebugHandler) corsAllowlisted(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if d.allowedOrigins[origin] {
		w.Header().Set("Access-Control-Allow-Origin", origin)
	}
}

// corsEcho reflects the origin with NO test: the shape a probe would
// actually reach.
func (d *DebugHandler) corsEcho(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin")) // want `controlbytes: request-derived value reaches http.Header.Set/Add unscrubbed`
}

func (d *DebugHandler) corsScrubbed(w http.ResponseWriter, r *http.Request) {
	origin := redactCtl(r.Header.Get("Origin"))
	if d.allowedOrigins[origin] {
		w.Header().Set("Access-Control-Allow-Origin", origin)
	}
}

// cleanValues pins that a url.Values carried in a variable is still a
// source, and that quote-named helpers clear it.
func cleanValues(q url.Values) {
	fmt.Fprintln(os.Stdout, q.Get("token")) // want `controlbytes: request-derived value reaches stdout/stderr print unscrubbed`
	fmt.Fprintln(os.Stdout, quoteForLog(q.Get("token")))
}

// redactCtl and quoteForLog keep their names and carry the byte-level
// bodies that earn them.
func redactCtl(s string) string {
	out := make([]byte, 0, len(s))
	for i := range s {
		if c := s[i]; c >= 0x20 && c != 0x7f {
			out = append(out, c)
		}
	}
	return string(out)
}

func quoteForLog(s string) string {
	for i := range s {
		if c := s[i]; c < 0x20 || c == 0x7f {
			return "'['" + redactCtl(s) + "]'"
		}
	}
	return "'" + s + "'"
}
