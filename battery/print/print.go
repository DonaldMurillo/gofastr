package print

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework"
)

// autoPrintSuffix is the shared, document-independent route that serves
// the external auto-print script, mounted once per battery.
const autoPrintSuffix = "/__autoprint.js"

// printCSP is the Content-Security-Policy for print routes. It is the
// strict default PLUS style-src 'unsafe-inline', the shell emits
// server-generated inline <style> blocks (@page rules, the print base).
// Scripts stay 'self' only (the auto-print script is the external file
// served from autoPrintSuffix), so no script injection vector is opened.
const printCSP = "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; frame-ancestors 'none'; base-uri 'self'"

// pdfCSP is printCSP with style-src widened by origin, the policy the PDF
// document carries when Config.BaseURL makes its app.css link absolute.
// Chromium renders that document as about:blank, an opaque origin, so
// 'self' matches nothing there and the stylesheet would be refused. Only
// style-src is widened: images, fonts and scripts stay 'self'.
func pdfCSP(origin string) string {
	return strings.Replace(printCSP, "style-src 'self' 'unsafe-inline'",
		"style-src 'self' 'unsafe-inline' "+origin, 1)
}

// cspOrigin returns base's scheme://host[:port] when base is an absolute
// http(s) URL whose host is a plain DNS name or IPv4 address, and "" for
// anything else. The result is spliced into a CSP, so a host carrying
// any byte outside [A-Za-z0-9.-] (a space, a ';', a quote, userinfo)
// is refused rather than escaped: a refused origin leaves the stylesheet
// blocked, never the policy widened.
func cspOrigin(base string) string {
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Opaque != "" {
		return ""
	}
	host, port := u.Hostname(), u.Port()
	if host == "" || !cspHostChars(host) || (port != "" && !allDigits(port)) {
		return ""
	}
	if port != "" {
		host += ":" + port
	}
	return u.Scheme + "://" + host
}

func cspHostChars(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '-') {
			return false
		}
	}
	return true
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// Config configures the print battery.
type Config struct {
	// PathPrefix is the URL prefix for all print documents.
	// Defaults to "/print". A trailing slash is trimmed. It must name a
	// path below the root: "/" or a prefix with no leading slash fails
	// the mount, since the embed reservation covers this prefix only.
	PathPrefix string

	// DefaultPage is the page setup inherited by documents that don't
	// set their own. Zero value resolves to A4 portrait, 12mm margins.
	DefaultPage PageConfig

	// DefaultAccess gates documents whose Access is nil. Defaults to
	// RequireAuth, so per-user documents are never world-readable
	// unless a document explicitly opts into Public.
	DefaultAccess AccessPolicy

	// AppCSSURL, when non-empty, is linked into every print shell so
	// documents inherit the host's design tokens (var(--*)). Typically
	// "/__gofastr/app.css". Empty = print base only. runtime.js is
	// NEVER linked.
	AppCSSURL string

	// BaseURL is the canonical, host-configured origin of the app (e.g.
	// "https://app.example.com"), used ONLY on the PDF path to make the
	// app.css link absolute so headless Chromium, which renders the
	// document as about:blank with no origin of its own, can fetch the
	// tokens. The PDF document's CSP adds this origin to style-src, and
	// the renderer must be allowed to reach its host (chromepdf
	// Options.AllowedHosts).
	// It is deliberately NOT derived from the request Host header, which
	// is client-controlled and would otherwise let a spoofed Host point
	// the server-side fetch at an arbitrary/internal address (SSRF).
	// When empty, the PDF app.css link stays relative (and simply won't
	// resolve in the about:blank document. PDFs render with the print
	// base only). The HTML path never needs this.
	BaseURL string

	// PDFRenderer is the pluggable PDF backend. nil = PDF routes return
	// 501. The chromedp-backed implementation lives in the separate
	// battery/print/chromepdf subpackage so this package imports no
	// chromedp.
	PDFRenderer PDFRenderer

	// MaxConcurrentRenders bounds how many RenderPDF calls the battery
	// drives at once, battery-wide across every document's /pdf route.
	// The shipped chromepdf backend runs one headless-Chromium process
	// per render, so an unbounded route turns a handful of concurrent
	// requests into process and memory exhaustion. Requests past the
	// cap are answered 503 with Retry-After instead of spawning another
	// renderer. Defaults to 4; values below 1 are clamped to the
	// default.
	MaxConcurrentRenders int

	// PrintBaseCSS overrides the built-in readable print base stylesheet.
	// Empty = use the battery default.
	PrintBaseCSS string
}

// Battery is the framework.Battery implementation for printable documents.
type Battery struct {
	cfg     Config
	docs    []*Document
	byName  map[string]*Document
	mounted bool

	// renderSem bounds concurrent RenderPDF calls at
	// cfg.MaxConcurrentRenders; see servePDF.
	renderSem chan struct{}
}

// defaultMaxConcurrentRenders is the render cap hosts get when they do
// not set MaxConcurrentRenders: enough parallelism to keep a small
// dashboard snappy, few enough headless Chromiums that a burst of
// requests cannot exhaust the node.
const defaultMaxConcurrentRenders = 4

// renderBusyRetryAfter is the Retry-After (seconds) served with the 503
// the battery answers while all render slots are busy.
const renderBusyRetryAfter = "1"

// New constructs the print battery. Pass the result to
// framework.App.RegisterBattery after declaring documents with Document.
func New(cfg Config) *Battery {
	if cfg.PathPrefix == "" {
		cfg.PathPrefix = "/print"
	}
	cfg.PathPrefix = strings.TrimRight(cfg.PathPrefix, "/")
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if cfg.DefaultPage.Size == "" {
		cfg.DefaultPage.Size = A4
	}
	if cfg.DefaultPage.Orientation == "" {
		cfg.DefaultPage.Orientation = Portrait
	}
	if cfg.MaxConcurrentRenders < 1 {
		cfg.MaxConcurrentRenders = defaultMaxConcurrentRenders
	}
	if cfg.DefaultAccess == nil {
		cfg.DefaultAccess = RequireAuth
	}
	return &Battery{
		cfg:       cfg,
		byName:    map[string]*Document{},
		renderSem: make(chan struct{}, cfg.MaxConcurrentRenders),
	}
}

// Document declares a printable document. Returns the battery for
// chaining. Panics on a duplicate name, an invalid declaration, or a
// call after Init, documents are immutable once routes are mounted.
func (b *Battery) Document(doc Document) *Battery {
	if b.mounted {
		panic("print: Document called after Init: declare documents before RegisterBattery/Start")
	}
	if doc.Name == "" {
		panic("print: Document.Name is required")
	}
	if _, dup := b.byName[doc.Name]; dup {
		panic(fmt.Sprintf("print: duplicate document name %q", doc.Name))
	}
	if !strings.HasPrefix(doc.Path, "/") {
		panic(fmt.Sprintf("print: document %q Path must begin with %q", doc.Name, "/"))
	}
	if doc.Path == autoPrintSuffix {
		panic(fmt.Sprintf("print: document %q Path %q collides with the shared auto-print route", doc.Name, autoPrintSuffix))
	}
	for _, existing := range b.docs {
		if existing.Path == doc.Path {
			panic(fmt.Sprintf("print: duplicate document Path %q (%q and %q)", doc.Path, existing.Name, doc.Name))
		}
	}
	if doc.Build == nil {
		panic(fmt.Sprintf("print: document %q has no Build func", doc.Name))
	}
	d := doc
	b.docs = append(b.docs, &d)
	b.byName[doc.Name] = &d
	return b
}

// Name implements framework.Battery.
func (b *Battery) Name() string { return "print" }

// ReservedEmbedPrefixes reports the prefix this battery actually mounted. Each
// request under it spawns headless Chrome, so a grant living in a stranger's
// page must never reach it, an unbounded amplification primitive behind a
// credential anyone with devtools can read. See framework.EmbedReserving.
func (b *Battery) ReservedEmbedPrefixes() []string {
	return []string{b.cfg.PathPrefix}
}

// Init implements framework.Battery. Mounts every declared document's
// HTML route, its sibling PDF route, and the shared auto-print script.
func (b *Battery) Init(app *framework.App) error {
	return b.RegisterRoutes(app.Router())
}

// RegisterRoutes mounts the print routes on the supplied router. Exposed
// so apps composing their own router can mount without the battery
// lifecycle.
func (b *Battery) RegisterRoutes(r *router.Router) error {
	// Idempotent: a host may wire the battery via RegisterBattery (whose
	// Init calls this) OR call RegisterRoutes directly for an eagerly-
	// composed router. Guarding here prevents a double-mount panic on the
	// underlying ServeMux if both happen. Use exactly one in practice.
	if b.mounted {
		return nil
	}
	// The prefix is what ReservedEmbedPrefixes names, so it must be a
	// path below the root: "/" trims to "" and would mount the routes at
	// the root, outside any reservation, and "print" reads as a host
	// pattern to the mux.
	if !strings.HasPrefix(b.cfg.PathPrefix, "/") {
		return fmt.Errorf("print: PathPrefix must be a path below the root, such as /print (got %q after trimming trailing slashes)", b.cfg.PathPrefix)
	}
	hdr := middleware.SecurityHeaders(middleware.SecurityHeadersConfig{ContentSecurityPolicy: printCSP})

	// Shared auto-print script (mounted once).
	r.Get(b.cfg.PathPrefix+autoPrintSuffix, hdr(http.HandlerFunc(serveAutoPrint)))

	for _, doc := range b.docs {
		r.Get(b.cfg.PathPrefix+doc.Path, hdr(b.serveHTML(doc)))
		r.Get(b.cfg.PathPrefix+doc.Path+"/pdf", hdr(b.servePDF(doc)))
	}
	b.mounted = true
	return nil
}

// serveAutoPrint serves the external auto-print script.
func serveAutoPrint(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write([]byte(autoPrintScript))
}

// serveHTML returns the handler for a document's print-friendly HTML route.
func (b *Battery) serveHTML(doc *Document) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !b.admit(w, r, doc) {
			return
		}
		html, _, ok := b.build(w, r, doc, false)
		if !ok {
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = io.WriteString(w, html)
	})
}

// admit runs the document's access policy. On a refusal it writes the
// response itself and returns false. Every route calls it before build,
// and the PDF route before it takes a render slot.
func (b *Battery) admit(w http.ResponseWriter, r *http.Request, doc *Document) bool {
	if pol := b.accessOf(doc); pol != nil {
		if status, reason := pol(r); status != 0 {
			if reason.IsZero() {
				reason = i18n.StatusPhrase(status)
			}
			i18n.HTTPError(w, r, status, reason)
			return false
		}
	}
	return true
}

// build runs the Build hook and assembles the shell, for a request admit
// has already let through. On any failure it writes the response itself
// and returns ok=false. The forPDF flag switches the app.css href to
// absolute and drops auto-print.
func (b *Battery) build(w http.ResponseWriter, r *http.Request, doc *Document, forPDF bool) (string, PageConfig, bool) {
	comp, err := safeBuild(doc, r)
	if err != nil {
		writeBuildError(w, r, err)
		return "", PageConfig{}, false
	}

	// A render panic fails the request like a Build error. SafeRenderCtx
	// has already reported it; its fallback box carries the panic text,
	// and serving that as the document (or as a PDF) would hand it to
	// the client with a 200.
	body, err := component.SafeRenderCtx(r.Context(), comp)
	if err != nil {
		writeBuildError(w, r, err)
		return "", PageConfig{}, false
	}

	title := doc.Title
	if doc.TitleFunc != nil {
		title = doc.TitleFunc(r)
	}

	page := effectivePage(doc.Page, b.cfg.DefaultPage)

	in := shellInput{
		Lang:         i18n.FromContext(r.Context()).Tag,
		Title:        title,
		Body:         body,
		PageCSS:      pageCSS(page),
		BaseCSS:      b.baseCSS(),
		ComponentCSS: componentCSS(body),
		DocCSS:       doc.Stylesheet,
	}
	if b.cfg.AppCSSURL != "" {
		switch {
		case forPDF && b.cfg.BaseURL != "":
			// The about:blank document (headless Chromium) can't resolve
			// a relative link, so make it absolute against the host-
			// configured canonical origin, NOT the request Host header,
			// and let the document's CSP fetch a stylesheet from it. A
			// BaseURL that is not a clean http(s) origin keeps printCSP.
			in.AppCSSHref = b.cfg.BaseURL + b.cfg.AppCSSURL
			if origin := cspOrigin(b.cfg.BaseURL); origin != "" {
				in.CSP = pdfCSP(origin)
			}
		case forPDF:
			// No trusted base configured: keep it relative. It won't
			// resolve in the about:blank document (PDF gets the print base
			// only), but we never emit a Host-derived absolute URL.
			in.AppCSSHref = b.cfg.AppCSSURL
		default:
			in.AppCSSHref = b.cfg.AppCSSURL
		}
	}
	if doc.AutoPrint && !forPDF {
		in.AutoPrintSrc = b.cfg.PathPrefix + autoPrintSuffix
	}

	return renderShell(in), page, true
}

// safeBuild calls a document's Build hook, converting a panic into an
// ordinary error so a misbehaving host closure yields a clean 500 status
// page (honoring the battery's "never a stack trace" contract) instead of
// crashing the handler goroutine. The panic value is intentionally NOT
// folded into the error message (it could carry sensitive data), the
// returned error is non-sentinel, so writeBuildError maps it to 500.
func safeBuild(doc *Document, r *http.Request) (comp component.Component, err error) {
	defer func() {
		if v := recover(); v != nil {
			comp = nil
			err = fmt.Errorf("print: document %q Build panicked (%T)", doc.Name, v)
		}
	}()
	return doc.Build(r)
}

// accessOf returns the effective access policy for a document.
func (b *Battery) accessOf(doc *Document) AccessPolicy {
	if doc.Access != nil {
		return doc.Access
	}
	return b.cfg.DefaultAccess
}

// baseCSS returns the configured or default print base stylesheet.
func (b *Battery) baseCSS() string {
	if b.cfg.PrintBaseCSS != "" {
		return b.cfg.PrintBaseCSS
	}
	return printBaseCSS
}

// writeBuildError maps a Build error to a clean status page (never a
// stack trace).
func writeBuildError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		i18n.HTTPError(w, r, http.StatusNotFound, i18n.StatusPhrase(http.StatusNotFound))
	case errors.Is(err, ErrForbidden):
		i18n.HTTPError(w, r, http.StatusForbidden, i18n.StatusPhrase(http.StatusForbidden))
	default:
		i18n.HTTPError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
	}
}

// PrintLink renders an anchor to a print document, opened in a new tab so
// the host SPA is untouched. params fill the document's {placeholders}.
// label defaults to "Print" in the language of ctx (nil is English).
// Returns empty HTML for an unknown document.
func (b *Battery) PrintLink(ctx context.Context, docName string, params map[string]string, label string) render.HTML {
	doc, ok := b.byName[docName]
	if !ok {
		return ""
	}
	if label == "" {
		label = printLabel.Text(ctx)
	}
	path := doc.Path
	for k, v := range params {
		path = strings.ReplaceAll(path, "{"+k+"}", url.PathEscape(v))
	}
	href := b.cfg.PathPrefix + path
	return render.Tag("a", map[string]string{
		"href":   href,
		"target": "_blank",
		"rel":    "noopener",
		"class":  "print-link",
	}, render.Text(label))
}
