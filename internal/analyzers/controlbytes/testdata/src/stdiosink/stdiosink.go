// Package stdiosink pins the fmt print arms: writes to
// os.Stdout/os.Stderr are sinks — including fmt.Print/Printf/Println,
// which have no writer argument and write to os.Stdout unconditionally
// — and so is the std log package's Print* (the default logger writes
// to stderr). Any other writer (a response writer, a buffer) has its
// own framing and stays quiet.
package stdiosink

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
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
// the terminal sinks this rule means, and fmt.Sprint* build a string
// rather than print one.
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

func escapeQuery(q string) string { return url.QueryEscape(q) }

// quoteCtl keeps the quote-named clearance with the body that earns it
// since the email round-2 probe: a byte walk naming the control range.
func quoteCtl(s string) string {
	for i := range s {
		if c := s[i]; c < 0x20 || c == 0x7f {
			return strings.ReplaceAll(s, "\n", "\\n")
		}
	}
	return s
}

// quoteOnlyName is quoteParamValue's lesson on this arm: the strong
// "quote" name with a body that compares only '"' and '\\' — never the
// control range — clears nothing.
func quoteOnlyName(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' || c == '\\' {
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
	b.WriteByte('"')
	return b.String()
}

// badBarePrint: fmt.Print/Printf/Println write to os.Stdout with no
// writer argument to inspect — they are the terminal sink outright.
func badBarePrint(r *http.Request) {
	fmt.Printf("serving %s\n", r.URL.Path) // want `controlbytes: request-derived value reaches stdout/stderr print unscrubbed`
	fmt.Println("remote", r.RemoteAddr)    // want `controlbytes: request-derived value reaches stdout/stderr print unscrubbed`
	fmt.Print(r.Host)                      // want `controlbytes: request-derived value reaches stdout/stderr print unscrubbed`
}

func goodBarePrint(r *http.Request) {
	fmt.Printf("serving %q\n", quoteCtl(r.URL.Path))
	fmt.Println("static line")
}

// bareQuotePass pins the classification fix: a quote-named local helper
// without control-range body evidence does not clear.
func bareQuotePass(r *http.Request) {
	fmt.Printf("quoted=%s\n", quoteOnlyName(r.URL.Path)) // want `controlbytes: request-derived value reaches stdout/stderr print unscrubbed`
}

// badLogPrint: log.Print/Printf/Println write to the default logger
// (stderr); a *log.Logger receiver is the same sink.
func badLogPrint(lg *log.Logger, r *http.Request) {
	log.Printf("bad path %s", r.URL.Path) // want `controlbytes: request-derived value reaches std log print unscrubbed`
	log.Println("remote", r.RemoteAddr)   // want `controlbytes: request-derived value reaches std log print unscrubbed`
	lg.Print(r.Host)                      // want `controlbytes: request-derived value reaches std log print unscrubbed`
}

func goodLogPrint(lg *log.Logger, r *http.Request) {
	log.Printf("bad path %q", quoteCtl(r.URL.Path))
	lg.Print("static line")
}
