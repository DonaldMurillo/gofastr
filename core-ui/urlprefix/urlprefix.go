// Package urlprefix puts a path prefix in front of the root-relative
// URLs a response carries. An app that names the reader's language in
// the URL (framework.WithLocalePrefixes) serves "/es/pricing" by
// routing "/pricing" with the es locale on the context. Every screen,
// island and handler keeps writing "/about"; [Middleware] turns that
// into "/es/about" on the way out, so a reader who arrived in Spanish
// keeps following Spanish links.
//
// The rewrite reaches the navigation and request URLs of an HTML
// response (anchor and area href, form action, button formaction, and
// the URL-valued data-cui-* attributes) and the Location,
// X-Gofastr-Location, X-Gofastr-Push-State and X-Gofastr-Document
// headers. It leaves assets alone (src, srcset,
// <link>), the runtime's /__gofastr/ endpoints, external and
// protocol-relative URLs, fragments, query-only and relative URLs, an
// anchor with hreflang (a language switcher, already built with
// i18n.LocalePath) or download, and any URL whose first segment already
// names a locale. JSON bodies and SSE pushes are not rewritten: code
// that writes a URL there reads i18n.PathPrefix itself.
package urlprefix

import (
	"bytes"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"golang.org/x/net/html"
)

// Rule says how to prefix a URL.
type Rule struct {
	// Prefix goes in front of each root-relative URL: "/es". Empty
	// leaves every URL alone.
	Prefix string
	// Locales lists every locale segment of the app, the default
	// included. A URL whose first segment names one is already
	// localized and stays as written.
	Locales []string
	// Origins lists absolute origins ("https://shop.example") whose
	// URLs are prefixed too.
	Origins []string
}

// URLAttrs lists the data-cui-* attributes whose value is a same-origin
// URL the runtime navigates to, requests, or compares with the
// browser's path. core-ui/ARCHITECTURE.md documents each;
// TestURLAttrsAllCovered fails when the table gains a URL-valued
// attribute this list lacks.
var URLAttrs = []string{
	"data-cui-intercept-panel",
	"data-cui-match-prefix",
	"data-cui-poll-src",
	"data-cui-push-state",
	"data-cui-rpc",
	"data-cui-rpc-navigate",
}

// Path returns u with the rule's prefix, or u unchanged when the rule
// leaves it alone.
func Path(u string, rule Rule) string {
	if rule.Prefix == "" || u == "" {
		return u
	}
	for _, o := range rule.Origins {
		if rest, ok := strings.CutPrefix(u, o); ok && o != "" {
			if rest == "" {
				return o + rule.Prefix
			}
			if rest[0] == '/' {
				return o + Path(rest, rule)
			}
			return u
		}
	}
	if u[0] != '/' || strings.HasPrefix(u, "//") {
		return u
	}
	seg := u[1:]
	if i := strings.IndexAny(seg, "/?#"); i >= 0 {
		seg = seg[:i]
	}
	if seg == "__gofastr" || slices.Contains(rule.Locales, seg) || "/"+seg == rule.Prefix {
		return u
	}
	if u == "/" {
		return rule.Prefix
	}
	if u[1] == '?' || u[1] == '#' {
		return rule.Prefix + u[1:]
	}
	return rule.Prefix + u
}

// HTML returns page with the rule applied to each navigation and
// request URL attribute. Markup it does not change keeps its bytes; a
// tag it changes is written back in normalized form. A page the
// tokenizer cannot read comes back unchanged.
func HTML(page []byte, rule Rule) []byte {
	if rule.Prefix == "" {
		return page
	}
	z := html.NewTokenizer(bytes.NewReader(page))
	var out bytes.Buffer
	out.Grow(len(page) + len(page)/32)
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			if z.Err() != io.EOF {
				return page
			}
			return out.Bytes()
		}
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			out.Write(z.Raw())
			continue
		}
		raw := slices.Clone(z.Raw())
		tok := z.Token()
		if !rewriteTag(&tok, rule) {
			out.Write(raw)
			continue
		}
		out.WriteString(tok.String())
	}
}

// rewriteTag applies rule to tok's URL attributes and reports whether
// any changed.
func rewriteTag(tok *html.Token, rule Rule) bool {
	switch tok.Data {
	case "link", "meta", "script", "img", "source", "video", "audio", "iframe":
		return false
	}
	if tok.Data == "a" || tok.Data == "area" {
		for _, a := range tok.Attr {
			if a.Key == "hreflang" || a.Key == "download" {
				return false
			}
		}
	}
	changed := false
	for i, a := range tok.Attr {
		if !urlAttr(tok.Data, a.Key) {
			continue
		}
		if v := Path(a.Val, rule); v != a.Val {
			tok.Attr[i].Val = v
			changed = true
		}
	}
	return changed
}

func urlAttr(tag, key string) bool {
	switch key {
	case "href":
		return tag == "a" || tag == "area"
	case "action":
		return tag == "form"
	case "formaction":
		return tag == "button" || tag == "input"
	}
	return strings.HasPrefix(key, "data-cui-") && slices.Contains(URLAttrs, key)
}

// Middleware rewrites the responses to a request whose URL named a
// prefixed locale (i18n.PathPrefix is not empty): text/html bodies
// through [HTML] and the Location, X-Gofastr-Location,
// X-Gofastr-Push-State and X-Gofastr-Document headers through [Path]. Any other request passes straight through. An HTML
// body is buffered until the handler returns, so Flush waits for it;
// other bodies stream. A body that is already encoded (gzip) passes
// through as written: the framework places this middleware inside
// Compress, which encodes after it.
func Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			prefix := i18n.PathPrefix(r.Context())
			if prefix == "" {
				next.ServeHTTP(w, r)
				return
			}
			set, _, _ := i18n.PathLocale(r.Context())
			rule := Rule{
				Prefix:  prefix,
				Locales: append([]string{set.Default}, set.Prefixed...),
				Origins: []string{"http://" + r.Host, "https://" + r.Host},
			}
			rw := &writer{ResponseWriter: w, rule: rule}
			next.ServeHTTP(rw, r)
			rw.finish()
		})
	}
}

// writer buffers an HTML body and rewrites the URL headers once, on the
// first write or at the end.
type writer struct {
	http.ResponseWriter
	rule      Rule
	headed    bool
	buffering bool
	code      int
	buf       bytes.Buffer
}

func (w *writer) rewriteHeaders() {
	h := w.ResponseWriter.Header()
	for _, k := range []string{"Location", "X-Gofastr-Location", "X-Gofastr-Push-State", "X-Gofastr-Document"} {
		if v := h.Get(k); v != "" {
			h.Set(k, Path(v, w.rule))
		}
	}
}

func (w *writer) WriteHeader(code int) {
	if w.headed {
		return
	}
	if code >= 100 && code < 200 {
		w.ResponseWriter.WriteHeader(code)
		return
	}
	w.headed = true
	w.code = code
	w.rewriteHeaders()
	h := w.ResponseWriter.Header()
	if strings.HasPrefix(h.Get("Content-Type"), "text/html") && h.Get("Content-Encoding") == "" {
		w.buffering = true
		h.Del("Content-Length")
		return
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *writer) Write(b []byte) (int, error) {
	if !w.headed {
		h := w.ResponseWriter.Header()
		if h.Get("Content-Type") == "" && len(b) > 0 {
			h.Set("Content-Type", http.DetectContentType(b))
		}
		w.WriteHeader(http.StatusOK)
	}
	if w.buffering {
		return w.buf.Write(b)
	}
	return w.ResponseWriter.Write(b)
}

// Flush forwards a flush of a streamed body. A buffered HTML body
// flushes once, rewritten, when the handler returns.
func (w *writer) Flush() {
	if w.buffering {
		return
	}
	if !w.headed {
		w.WriteHeader(http.StatusOK)
		if w.buffering {
			return
		}
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *writer) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *writer) finish() {
	if !w.headed {
		w.rewriteHeaders()
		return
	}
	if !w.buffering {
		return
	}
	w.ResponseWriter.WriteHeader(w.code)
	_, _ = w.ResponseWriter.Write(HTML(w.buf.Bytes(), w.rule))
}
