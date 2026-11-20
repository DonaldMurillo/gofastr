package dev

import (
	"bufio"
	"bytes"
	"errors"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/internal/mediatype"
)

// LiveReloadHTMLInjector returns middleware that splices the livereload
// client into full HTML documents that lack it.
func LiveReloadHTMLInjector() router.Middleware {
	return htmlInjector(func() []string { return []string{LiveReloadScriptURL} })
}

func htmlInjector(scripts func() []string) router.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodHead || r.Header.Get("Range") != "" {
				next.ServeHTTP(w, r)
				return
			}
			iw := &injectWriter{rw: w, status: http.StatusOK, scripts: scripts()}
			next.ServeHTTP(iw, r)
			iw.finish()
		})
	}
}

var (
	devScriptsMu sync.Mutex
	devScripts   = map[*router.Router][]string{}
)

func addDevScript(r *router.Router, url string) {
	devScriptsMu.Lock()
	urls, mounted := devScripts[r]
	if !slices.Contains(urls, url) {
		devScripts[r] = append(urls, url)
	}
	devScriptsMu.Unlock()
	if mounted {
		return
	}
	r.Use(htmlInjector(func() []string {
		devScriptsMu.Lock()
		defer devScriptsMu.Unlock()
		return slices.Clone(devScripts[r])
	}))
}

type injectWriter struct {
	rw         http.ResponseWriter
	status     int
	decided    bool
	buffering  bool
	sentHeader bool
	hijacked   bool
	scripts    []string
	buf        bytes.Buffer
}

func (w *injectWriter) Header() http.Header { return w.rw.Header() }

func (w *injectWriter) Unwrap() http.ResponseWriter { return w.rw }

func (w *injectWriter) decide() {
	if w.decided {
		return
	}
	w.decided = true
	h := w.Header()
	if !mediatype.IsHTML(h.Get("Content-Type")) {
		return
	}
	if ce := h.Get("Content-Encoding"); ce != "" && !strings.EqualFold(ce, "identity") {
		return
	}
	if h.Get("X-Gofastr-Partial") != "" {
		return
	}
	w.buffering = true
}

func (w *injectWriter) WriteHeader(status int) {
	if status >= 100 && status < 200 && status != http.StatusSwitchingProtocols {
		if !w.sentHeader {
			w.rw.WriteHeader(status)
		}
		return
	}
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

func (w *injectWriter) finish() {
	if w.hijacked || !w.buffering {
		return
	}
	body := w.buf.Bytes()
	if i := bytes.LastIndex(body, []byte("</body>")); i >= 0 {
		var tags []byte
		for _, url := range w.scripts {
			if !bytes.Contains(body, []byte(`src="`+url+`"`)) {
				tags = append(tags, `<script src="`+url+`" defer></script>`...)
			}
		}
		if len(tags) > 0 {
			spliced := make([]byte, 0, len(body)+len(tags))
			spliced = append(spliced, body[:i]...)
			spliced = append(spliced, tags...)
			spliced = append(spliced, body[i:]...)
			body = spliced
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		}
	}
	if !w.sentHeader {
		w.rw.WriteHeader(w.status)
		w.sentHeader = true
	}
	if len(body) > 0 {
		_, _ = w.rw.Write(body)
	}
}
