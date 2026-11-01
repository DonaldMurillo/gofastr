// Package gatepath decides whether a request path sits under one of the
// path prefixes a loopback host's session gate lets through
// (battery/service and battery/desktop Config.OpenPrefixes).
//
// The match is deliberately narrow. A path the router could resolve to
// something other than what the gate saw is never open:
//
//   - a path that is not already clean ("/api/../admin", "/api//x"),
//     because a router that cleans it serves a different route;
//   - a path with any percent-encoded byte (r.URL.RawPath set:
//     "/api%2Fx"), because a router that matches on the escaped path
//     sees different segments than the decoded one;
//   - a prefix match that is not on a segment boundary ("/apix" is not
//     under "/api").
//
// A request this package refuses stays behind the gate, so every
// mistake fails closed.
package gatepath

import (
	"fmt"
	"net/http"
	"path"
	"regexp"
	"strings"
)

// rePrefix is one or more "/segment" runs of unreserved URL bytes.
var rePrefix = regexp.MustCompile(`^(?:/[A-Za-z0-9._~-]+)+$`)

// Validate refuses a prefix the gate cannot match safely: one that is
// not an absolute clean path of plain segments, "/" (which would be the
// whole app, and there is no switch for that), and anything under
// /__gofastr/, which holds the framework's own runtime, SSE and sign-in
// routes. field names the setting in the error ("service:
// Config.OpenPrefixes").
func Validate(field, prefix string) error {
	if !rePrefix.MatchString(prefix) || path.Clean(prefix) != prefix {
		return fmt.Errorf("%s entry %q must be an absolute path such as /api: ASCII letters, digits, '.', '_', '~' and '-' in each segment, no trailing slash, no empty, '.' or '..' segment", field, prefix)
	}
	if prefix == "/__gofastr" || strings.HasPrefix(prefix, "/__gofastr/") {
		return fmt.Errorf("%s entry %q is under /__gofastr/, the framework's own routes, which always stay behind the gate", field, prefix)
	}
	return nil
}

// Open reports whether r's path is prefix itself or below it, for one
// of prefixes. See the package doc for the paths it refuses.
func Open(r *http.Request, prefixes []string) bool {
	if len(prefixes) == 0 || r.URL == nil || r.URL.RawPath != "" {
		return false
	}
	p := r.URL.Path
	if p == "" || p[0] != '/' {
		return false
	}
	c := path.Clean(p)
	if c != p && c+"/" != p {
		return false
	}
	for _, pre := range prefixes {
		if c == pre || strings.HasPrefix(c, pre+"/") {
			return true
		}
	}
	return false
}
