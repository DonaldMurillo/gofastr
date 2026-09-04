// Package pathnorm pins the normalizer posture (review finding C7):
// qualified calls into the stdlib path and path/filepath packages never
// clear taint — Clean, Base, Dir, Join reorder separators and dots and
// pass every control byte through — and a same-package helper named
// clean* clears only with the byte-level body evidence (a byte-indexed
// filter, or every return handing the parameter to a genuinely
// scrub-named call) the doc already requires.
package pathnorm

import (
	"net/http"
	"path"
	"path/filepath"
)

func redirects(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Location", path.Clean(r.URL.Path))  // want `controlbytes: request-derived value reaches http.Header.Set/Add unscrubbed`
	w.Header().Set("X-Dir", filepath.Clean(r.URL.Path)) // want `controlbytes: request-derived value reaches http.Header.Set/Add unscrubbed`
}

// cleanBytes walks the value as bytes: the body evidence the doc
// requires, so the clean-only name is earned. Quiet.
func cleanBytes(s string) string {
	out := make([]byte, 0, len(s))
	for i := range s {
		if c := s[i]; c >= 0x20 && c != 0x7f {
			out = append(out, c)
		}
	}
	return string(out)
}

// cleanPassthrough shares the name but never looks at bytes: the name
// alone does not clear it.
func cleanPassthrough(s string) string { return s + "" }

// cleanViaStdlib is a one-line wrapper whose every return hands the
// parameter to path.Clean — the wrapper carries no scrub either.
func cleanViaStdlib(p string) string { return path.Clean(p) }

func cleanedHelpers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Location", cleanBytes(r.URL.Path))       // quiet: byte-indexed body
	w.Header().Set("Location", cleanPassthrough(r.URL.Path)) // want `controlbytes: request-derived value reaches http.Header.Set/Add unscrubbed`
	w.Header().Set("Location", cleanViaStdlib(r.URL.Path))   // want `controlbytes: request-derived value reaches http.Header.Set/Add unscrubbed`
	w.Header().Set("Location", sanitizeIt(r.URL.Path))       // want `controlbytes: request-derived value reaches http.Header.Set/Add unscrubbed`
}

// sanitizeIt keeps the strong scrub name and the pass-through body:
// since the email round-2 probe (quoteParamValue), a same-package name
// buys nothing without the byte-level evidence, so this fires.
func sanitizeIt(s string) string { return s }
