// Package srcapi pins the request-source surface (review finding C4):
// the header wrapper accessors (Referer, UserAgent, PostFormValue,
// BasicAuth) are r.Header.Get in disguise, RequestURI is the raw
// undecoded request line, and a cookie bound from r.Cookie is
// request-derived through its .Value field.
package srcapi

import (
	"log/slog"
	"net/http"
)

func wrappers(r *http.Request) {
	_ = slog.String("referer", r.Referer())       // want `controlbytes: request-derived value reaches slog.String/slog.Any unscrubbed`
	_ = slog.String("agent", r.UserAgent())       // want `controlbytes: request-derived value reaches slog.String/slog.Any unscrubbed`
	_ = slog.String("post", r.PostFormValue("x")) // want `controlbytes: request-derived value reaches slog.String/slog.Any unscrubbed`
	user, pass, _ := r.BasicAuth()
	_ = slog.String("user", user) // want `controlbytes: request-derived value reaches slog.String/slog.Any unscrubbed`
	_ = slog.String("pass", pass) // want `controlbytes: request-derived value reaches slog.String/slog.Any unscrubbed`
}

func rawLine(r *http.Request) {
	_ = slog.String("uri", r.RequestURI) // want `controlbytes: request-derived value reaches slog.String/slog.Any unscrubbed`
}
func cookies(r *http.Request) {
	c, err := r.Cookie("session")
	if err == nil {
		_ = slog.String("sid2", c.Value) // want `controlbytes: request-derived value reaches slog.String/slog.Any unscrubbed`
	}
}

func scrubbed(r *http.Request) {
	_ = slog.String("referer", scrubCtl(r.Referer()))
	_ = slog.String("uri", scrubCtl(r.RequestURI))
	c, _ := r.Cookie("session")
	_ = slog.String("sid", scrubCtl(c.Value))
}

func scrubCtl(s string) string {
	out := make([]byte, 0, len(s))
	for i := range s {
		if c := s[i]; c >= 0x20 && c != 0x7f {
			out = append(out, c)
		}
	}
	return string(out)
}

// The listheader rule steers reads toward .Values and the index
// spelling; those bytes are as request-derived as Get's, and so are
// the cookies the request supplied.
func headerListReads(r *http.Request) {
	_ = slog.String("xff", r.Header.Values("X-Forwarded-For")[0]) // want `controlbytes: request-derived value reaches slog.String/slog.Any unscrubbed`
	_ = slog.String("tok", r.Header["X-Token"][0])                // want `controlbytes: request-derived value reaches slog.String/slog.Any unscrubbed`
	for _, v := range r.Header.Values("X-Token") {
		_ = slog.String("tok2", v) // want `controlbytes: request-derived value reaches slog.String/slog.Any unscrubbed`
	}
	_ = slog.String("cs", r.Cookies()[0].Value) // want `controlbytes: request-derived value reaches slog.String/slog.Any unscrubbed`
}

// A response's headers and cookies are the server's own text.
func responseListReads(resp *http.Response) {
	_ = slog.String("etag", resp.Header.Values("Etag")[0])
	_ = slog.String("cs", resp.Cookies()[0].Value)
}
