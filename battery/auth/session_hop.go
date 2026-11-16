package auth

import (
	"io"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

// withheldCookieArrival reports a top-level page load that began on
// another site and carried no session cookie: a Strict one is withheld there.
func withheldCookieArrival(r *http.Request, cookie string) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	h := r.Header
	return handler.IsCrossSiteRequest(r) && h.Get("Sec-Fetch-Mode") == "navigate" &&
		h.Get("Sec-Fetch-Dest") == "document" && len(r.CookiesNamed(cookie)) == 0
}

// arrivalHopWriter answers a withheld-cookie arrival's 401, or a
// redirect a gate noted (handler.NoteSessionRefusal), with the hop page.
type arrivalHopWriter struct {
	http.ResponseWriter
	r       *http.Request
	pages   Pages
	target  string
	refused func() bool
	wrote   bool
	hopped  bool
}

func (w *arrivalHopWriter) WriteHeader(code int) {
	if w.hopped {
		return
	}
	if w.wrote || code < 200 {
		w.ResponseWriter.WriteHeader(code)
		return
	}
	w.wrote = true
	if code != http.StatusUnauthorized && !(isRedirectStatus(code) && w.refused()) {
		w.ResponseWriter.WriteHeader(code)
		return
	}
	w.hopped = true
	h := w.Header()
	for _, k := range []string{"Location", "Content-Type", "Content-Length", "Content-Encoding", "Www-Authenticate"} {
		h.Del(k)
	}
	writeSameSiteHop(w.ResponseWriter, w.r, w.pages, w.target, sessionHopTitle)
}

func (w *arrivalHopWriter) Write(p []byte) (int, error) {
	if w.hopped {
		return len(p), nil
	}
	w.wrote = true
	return w.ResponseWriter.Write(p)
}

func (w *arrivalHopWriter) ReadFrom(src io.Reader) (int64, error) {
	if w.hopped {
		return io.Copy(io.Discard, src)
	}
	w.wrote = true
	return io.Copy(w.ResponseWriter, src)
}

func (w *arrivalHopWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok && !w.hopped {
		w.wrote = true
		f.Flush()
	}
}

func (w *arrivalHopWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func isRedirectStatus(code int) bool {
	switch code {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return true
	}
	return false
}
