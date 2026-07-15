package dev

import (
	"bufio"
	"bytes"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// LiveReloadHTMLInjector returns dev-only middleware that splices the
// livereload client into full HTML documents that don't already carry it.
// uihost pages inject the script themselves; this covers every OTHER way an
// app serves a page under `gofastr dev` — static.Handler file serving (the
// SPA and static-site shapes), widget-server pages, hand-rolled handlers
// that set a text/html Content-Type — so "edit → rebuild → browser
// refreshes" holds across the whole surface, not just uihost screens.
//
// What is deliberately left alone:
//   - responses without an explicit text/html Content-Type (no sniffing);
//   - fragments: anything without a closing </body> (island RPC swaps,
//     SPA-nav partials) — an appended script node would corrupt the DOM the
//     runtime swaps them into;
//   - encoded bodies (Content-Encoding) — opaque bytes even when labeled
//     HTML;
//   - HEAD and Range requests — http.ServeFile/ServeContent semantics
//     (real Content-Length with no body, 206 byte ranges) must survive;
//   - streams: SSE and anything the handler Flushes pass through
//     unbuffered, and a Flush mid-HTML abandons injection so progressive
//     rendering keeps painting;
//   - hijacked connections (WebSocket upgrades) — Hijack is forwarded and
//     the wrapper never writes afterwards.
func LiveReloadHTMLInjector() router.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodHead || r.Header.Get("Range") != "" {
				next.ServeHTTP(w, r)
				return
			}
			iw := &injectWriter{rw: w, status: http.StatusOK}
			next.ServeHTTP(iw, r)
			iw.finish()
		})
	}
}

type injectWriter struct {
	rw         http.ResponseWriter
	status     int
	decided    bool
	buffering  bool
	sentHeader bool
	hijacked   bool
	buf        bytes.Buffer
}

func (w *injectWriter) Header() http.Header { return w.rw.Header() }

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *injectWriter) Unwrap() http.ResponseWriter { return w.rw }

func (w *injectWriter) decide() {
	if w.decided {
		return
	}
	w.decided = true
	h := w.Header()
	ct := h.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		return
	}
	if ce := h.Get("Content-Encoding"); ce != "" && !strings.EqualFold(ce, "identity") {
		return // opaque compressed bytes — never splice or re-measure
	}
	if h.Get("X-Gofastr-Partial") != "" {
		return // SPA-nav partial: a fragment by contract
	}
	w.buffering = true
}

func (w *injectWriter) WriteHeader(status int) {
	w.decide()
	w.status = status
	if !w.buffering && !w.sentHeader {
		w.rw.WriteHeader(status)
		w.sentHeader = true
	}
}

func (w *injectWriter) Write(p []byte) (int, error) {
	if w.hijacked {
		return 0, http.ErrHijacked
	}
	w.decide()
	if w.buffering {
		return w.buf.Write(p)
	}
	if !w.sentHeader {
		w.rw.WriteHeader(w.status)
		w.sentHeader = true
	}
	return w.rw.Write(p)
}

// Flush is a streaming signal. For buffered HTML it means the handler wants
// progressive rendering: release the buffered prefix raw, abandon injection
// for this response, and stream from here on. For everything else forward
// to the underlying Flusher (SSE heartbeats, chunked APIs).
func (w *injectWriter) Flush() {
	if w.hijacked {
		return
	}
	w.decide()
	if w.buffering {
		w.buffering = false
		if !w.sentHeader {
			w.rw.WriteHeader(w.status)
			w.sentHeader = true
		}
		if w.buf.Len() > 0 {
			_, _ = w.rw.Write(w.buf.Bytes())
			w.buf.Reset()
		}
	}
	if f, ok := w.rw.(http.Flusher); ok {
		if !w.sentHeader {
			w.rw.WriteHeader(w.status)
			w.sentHeader = true
		}
		f.Flush()
	}
}

// Hijack forwards connection takeover (WebSocket upgrades — core/stream
// asserts http.Hijacker directly). After a hijack the wrapper never writes.
func (w *injectWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := w.rw.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("dev: underlying ResponseWriter does not support hijacking")
	}
	conn, rw, err := hj.Hijack()
	if err == nil {
		w.hijacked = true
		w.buf.Reset()
	}
	return conn, rw, err
}

const liveReloadScriptTag = `<script src="` + LiveReloadScriptURL + `" defer></script>`

func (w *injectWriter) finish() {
	if w.hijacked || !w.buffering {
		return
	}
	body := w.buf.Bytes()
	// Full documents only: splice before the LAST </body>. Fragments pass
	// through byte-for-byte. The guard matches the script TAG, not the bare
	// URL, so a page that merely mentions the URL in prose still gets the
	// client.
	if i := bytes.LastIndex(body, []byte("</body>")); i >= 0 &&
		!bytes.Contains(body, []byte(`src="`+LiveReloadScriptURL+`"`)) {
		spliced := make([]byte, 0, len(body)+len(liveReloadScriptTag))
		spliced = append(spliced, body[:i]...)
		spliced = append(spliced, liveReloadScriptTag...)
		spliced = append(spliced, body[i:]...)
		body = spliced
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	}
	if !w.sentHeader {
		w.rw.WriteHeader(w.status)
		w.sentHeader = true
	}
	if len(body) > 0 {
		_, _ = w.rw.Write(body)
	}
}
