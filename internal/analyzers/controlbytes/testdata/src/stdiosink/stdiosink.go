// Package stdiosink pins the fmt.Fprint* arm: only writes to
// os.Stdout/os.Stderr are sinks; any other writer (a response writer, a
// buffer) has its own framing and stays quiet.
package stdiosink

import (
	"fmt"
	"io"
	"net/http"
	"os"
)

func badStdout(r *http.Request) {
	fmt.Fprintf(os.Stdout, "serving %s\n", r.URL.Path) // want `controlbytes: request-derived value reaches stdout/stderr print unscrubbed`
	fmt.Fprintln(os.Stderr, "remote", r.RemoteAddr)    // want `controlbytes: request-derived value reaches stdout/stderr print unscrubbed`
}

func goodStdout(r *http.Request) {
	fmt.Fprintf(os.Stdout, "serving %q\n", quoteCtl(r.URL.Path))
	fmt.Fprintln(os.Stderr, "static line")
}

// otherWritersAreQuiet: an http.ResponseWriter and an io.Writer are not
// the terminal sinks this rule means, and a print with no writer at all
// is not one either.
func otherWritersAreQuiet(w http.ResponseWriter, out io.Writer, r *http.Request) {
	fmt.Fprintf(w, "echo %s", r.URL.Path)
	fmt.Fprint(out, r.URL.Path)
	_ = fmt.Sprintf("%s", r.URL.Path)
}

// escapeClearance pins the escape/quote clearance on the stdio arm too:
// a value arg AND the format string itself are both sink arguments.
func escapeClearance(r *http.Request) {
	fmt.Fprintf(os.Stdout, "path=%s\n", r.URL.Path)              // want `controlbytes: request-derived value reaches stdout/stderr print unscrubbed`
	fmt.Fprintf(os.Stdout, "query=%s\n", r.URL.Query().Get("q")) // want `controlbytes: request-derived value reaches stdout/stderr print unscrubbed`
	fmt.Fprintf(os.Stdout, "escaped=%s\n", escapeQuery(r.URL.RawQuery))
}

func escapeQuery(q string) string { return q }

func quoteCtl(s string) string { return "\"" + s + "\"" }
