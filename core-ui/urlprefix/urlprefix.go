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
// i18n.LocalePath) or download, any URL whose first segment already
// names a locale, and any path a Rule's Keep claims (a static export's
// copied files). JSON bodies and SSE pushes are not rewritten: code
// that writes a URL there reads i18n.PathPrefix itself.
package urlprefix

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
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
	// Keep, when set, is asked about the path of each root-relative URL
	// the rule would prefix (query and fragment cut off, as written);
	// true leaves the URL alone. A static export sets it: its files are
	// copied once, to the export root, so a link to one keeps its
	// unprefixed path, while the live router serves them under every
	// prefix.
	Keep func(path string) bool
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
			switch rest[0] {
			case '/':
				return o + Path(rest, rule)
			case '?', '#':
				return o + rule.Prefix + rest
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
	// The router splits the locale off the decoded path, so /%65s/ is
	// /es/ to it: compare the segment as the router reads it, or the
	// link gets a second prefix (/es/%65s/about).
	if dec, err := url.PathUnescape(seg); err == nil {
		seg = dec
	}
	if seg == "__gofastr" || slices.Contains(rule.Locales, seg) || "/"+seg == rule.Prefix {
		return u
	}
	if rule.Keep != nil {
		p := u
		if i := strings.IndexAny(p, "?#"); i >= 0 {
			p = p[:i]
		}
		//gofastr:allow(recovercallback) Keep is set only by a static export build (framework/static), which calls HTML on its own goroutine; Middleware's per-request rule never sets it, so no request path reaches this call.
		if rule.Keep(p) {
			return u
		}
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
// X-Gofastr-Push-State and X-Gofastr-Document headers through [Path].
// Any other request passes straight through. An HTML body is buffered
// until the handler returns, so Flush waits for it; other bodies stream.
// A body with no Content-Type is typed by the sniff net/http applies, over
// its first 512 bytes across writes, even when the handler wrote its
// status first. While the status waits, the header stays as it stood at
// WriteHeader, as net/http keeps it: a header set later is ignored,
// trailers aside. A body that is already encoded (gzip) passes through as
// written: the framework places this middleware inside Compress, which
// encodes after it.
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
	// pending: the handler wrote a status with no Content-Type, so the
	// status waits for the first body bytes. net/http sniffs the type
	// from the first 512 of them, across writes, and an untyped HTML
	// body must be rewritten like a typed one. The held bytes sit in buf.
	pending bool
	code    int
	buf     bytes.Buffer
	// snap is the header as it stood at WriteHeader while the real
	// status waits. net/http fixes the header there: a header set after
	// it is ignored, trailers aside, so commit sends snap.
	snap http.Header
}

// sniffLen is how many bytes net/http's sniff reads.
const sniffLen = 512

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
	if h.Get("Content-Encoding") == "" {
		if strings.HasPrefix(h.Get("Content-Type"), "text/html") {
			w.buffering = true
			w.snap = h.Clone()
			w.snap.Del("Content-Length")
			return
		}
		// net/http sniffs only when the key is absent: a key set to nil
		// is the handler opting out, and the body goes out untyped.
		if _, typed := h["Content-Type"]; !typed && bodyAllowed(code) {
			w.pending = true
			w.snap = h.Clone()
			return
		}
	}
	w.ResponseWriter.WriteHeader(code)
}

// commit sends the deferred status with the header as it stood at
// WriteHeader. A trailer the handler declared there, or sets under
// http.TrailerPrefix, is kept from the live map: net/http reads those
// when the handler returns.
func (w *writer) commit() {
	h := w.ResponseWriter.Header()
	declared := map[string]bool{}
	for _, v := range w.snap.Values("Trailer") {
		for _, k := range strings.Split(v, ",") {
			if k = strings.TrimSpace(k); k != "" {
				declared[http.CanonicalHeaderKey(k)] = true
			}
		}
	}
	for k, v := range h {
		if declared[k] || strings.HasPrefix(k, http.TrailerPrefix) {
			w.snap[k] = v
		}
	}
	for k := range h {
		delete(h, k)
	}
	for k, v := range w.snap {
		h[k] = v
	}
	w.ResponseWriter.WriteHeader(w.code)
}

// bodyAllowed mirrors net/http: these statuses carry no body, so it
// never sniffs one for them.
func bodyAllowed(code int) bool {
	return code != http.StatusNoContent && code != http.StatusNotModified
}

// settle decides a pending status from the held body bytes, the same
// sniff net/http applies to an untyped response: HTML stays buffered
// and is rewritten, anything else is forwarded and streams.
func (w *writer) settle() error {
	w.pending = false
	held := w.buf.Bytes()
	if len(held) == 0 {
		// net/http sniffs no type from an empty body.
		w.commit()
		return nil
	}
	ct := http.DetectContentType(held)
	w.snap.Set("Content-Type", ct)
	if strings.HasPrefix(ct, "text/html") {
		w.buffering = true
		w.snap.Del("Content-Length")
		return nil
	}
	w.commit()
	_, err := w.ResponseWriter.Write(held)
	w.buf.Reset()
	return err
}

func (w *writer) Write(b []byte) (int, error) {
	if !w.headed {
		w.WriteHeader(http.StatusOK)
	}
	if w.pending {
		w.buf.Write(b)
		if w.buf.Len() < sniffLen {
			return len(b), nil
		}
		if err := w.settle(); err != nil {
			return 0, err
		}
		return len(b), nil
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
	if w.pending {
		// A flush ends the sniff: net/http types the response from
		// what it holds. With nothing held the type is not HTML, so
		// what follows streams.
		if w.settle() != nil || w.buffering {
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
	if w.pending {
		if w.settle() != nil || !w.buffering {
			return
		}
	}
	if !w.buffering {
		return
	}
	w.commit()
	_, _ = w.ResponseWriter.Write(HTML(w.buf.Bytes(), w.rule))
}
