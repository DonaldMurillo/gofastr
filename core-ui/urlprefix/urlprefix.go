// Package urlprefix puts a locale prefix in front of the root-relative URLs a response carries.
package urlprefix

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/internal/mediatype"
	"golang.org/x/net/html"
)

// Rule says how to prefix a URL.
type Rule struct {
	// Prefix goes in front of each root-relative URL: "/es".
	Prefix string
	// Locales lists every locale segment, the default included.
	Locales []string
	// Origins lists absolute origins whose URLs are prefixed too.
	Origins []string
	// Keep, when set and true for a path, leaves its URL alone.
	Keep func(path string) bool
}

// URLAttrs lists the data-cui-* attributes whose value is a same-origin URL.
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
	if dec, err := url.PathUnescape(seg); err == nil {
		seg, _, _ = strings.Cut(dec, "/")
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

// Refresh returns v, a Refresh header or meta refresh content, with the rule applied to its URL.
func Refresh(v string, rule Rule) string {
	start, end, ok := refreshURL(v)
	if !ok || rule.Prefix == "" {
		return v
	}
	return v[:start] + Path(v[start:end], rule) + v[end:]
}

func refreshURL(v string) (start, end int, ok bool) {
	space := func(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\f' || c == '\r' }
	i := 0
	skip := func() {
		for i < len(v) && space(v[i]) {
			i++
		}
	}
	skip()
	t := i
	for i < len(v) && (v[i] >= '0' && v[i] <= '9' || v[i] == '.') {
		i++
	}
	if i == t || i == len(v) || !(v[i] == ';' || v[i] == ',' || space(v[i])) {
		return 0, 0, false
	}
	skip()
	if i < len(v) && (v[i] == ';' || v[i] == ',') {
		i++
	}
	skip()
	if len(v)-i >= 3 && strings.EqualFold(v[i:i+3], "url") {
		at := i
		i += 3
		skip()
		if i == len(v) || v[i] != '=' {
			i = at
		} else {
			i++
			skip()
		}
	}
	end = len(v)
	if i < len(v) && (v[i] == '\'' || v[i] == '"') {
		q := v[i]
		i++
		if j := strings.IndexByte(v[i:], q); j >= 0 {
			end = i + j
		}
	}
	return i, end, i < end
}

// HTML returns page with the rule applied to each navigation URL attribute, or page unchanged when it cannot be read.
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

func rewriteTag(tok *html.Token, rule Rule) bool {
	switch tok.Data {
	case "meta":
		return rewriteMetaRefresh(tok, rule)
	case "link", "script", "img", "source", "video", "audio", "iframe":
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

func rewriteMetaRefresh(tok *html.Token, rule Rule) bool {
	refresh, content := false, -1
	for i, a := range tok.Attr {
		switch a.Key {
		case "http-equiv":
			refresh = strings.EqualFold(strings.TrimSpace(a.Val), "refresh")
		case "content":
			content = i
		}
	}
	if !refresh || content < 0 {
		return false
	}
	v := Refresh(tok.Attr[content].Val, rule)
	if v == tok.Attr[content].Val {
		return false
	}
	tok.Attr[content].Val = v
	return true
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

// Middleware rewrites the URLs of every response to a request under a prefixed locale.
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

type writer struct {
	http.ResponseWriter
	rule      Rule
	headed    bool
	buffering bool
	pending   bool
	code      int
	buf       bytes.Buffer
	snap      http.Header
}

const sniffLen = 512

func (w *writer) rewriteHeaders() {
	h := w.ResponseWriter.Header()
	for _, k := range []string{"Location", "Content-Location", "X-Gofastr-Location", "X-Gofastr-Push-State", "X-Gofastr-Document"} {
		if v := h.Get(k); v != "" {
			h.Set(k, Path(v, w.rule))
		}
	}
	for i, v := range h.Values("Refresh") {
		h["Refresh"][i] = Refresh(v, w.rule)
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
		if mediatype.IsHTML(h.Get("Content-Type")) {
			w.buffering = true
			w.snap = h.Clone()
			w.snap.Del("Content-Length")
			return
		}
		if _, typed := h["Content-Type"]; !typed && bodyAllowed(code) {
			w.pending = true
			w.snap = h.Clone()
			return
		}
	}
	w.ResponseWriter.WriteHeader(code)
}

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

func bodyAllowed(code int) bool {
	return code != http.StatusNoContent && code != http.StatusNotModified
}

func (w *writer) settle() error {
	w.pending = false
	held := w.buf.Bytes()
	if len(held) == 0 {
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
