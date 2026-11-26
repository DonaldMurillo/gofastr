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

const autoPrintSuffix = "/__autoprint.js"

const printCSP = "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; frame-ancestors 'none'; base-uri 'self'"

func pdfCSP(origin string) string {
	return strings.Replace(printCSP, "style-src 'self' 'unsafe-inline'",
		"style-src 'self' 'unsafe-inline' "+origin, 1)
}

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
	PathPrefix string

	// DefaultPage is the page setup inherited by documents that don't set
	// their own.
	DefaultPage PageConfig

	// DefaultAccess gates documents whose Access is nil.
	DefaultAccess AccessPolicy

	// AppCSSURL is linked into every shell for the host's design tokens.
	AppCSSURL string

	// BaseURL is the app's origin, used only to make the PDF's app.css link absolute.
	BaseURL string

	// PDFRenderer is the pluggable PDF backend.
	PDFRenderer PDFRenderer

	// MaxConcurrentRenders caps RenderPDF calls battery-wide; default 4.
	MaxConcurrentRenders int

	// PrintBaseCSS overrides the built-in readable print base stylesheet.
	PrintBaseCSS string
}

// Battery is the framework.Battery implementation for printable
// documents.
type Battery struct {
	cfg     Config
	docs    []*Document
	byName  map[string]*Document
	mounted bool

	renderSem chan struct{}
}

const defaultMaxConcurrentRenders = 4

const renderBusyRetryAfter = "1"

// New constructs the print battery.
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

// Document declares a printable document.
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
	if doc.Page != nil {
		d.Page = doc.Page.Ptr()
	}
	b.docs = append(b.docs, &d)
	b.byName[doc.Name] = &d
	return b
}

// Name implements framework.Battery.
func (b *Battery) Name() string { return "print" }

// ReservedEmbedPrefixes reports the prefix this battery actually mounted.
func (b *Battery) ReservedEmbedPrefixes() []string {
	return []string{b.cfg.PathPrefix}
}

// Init implements framework.Battery.
func (b *Battery) Init(app *framework.App) error {
	return b.RegisterRoutes(app.Router())
}

// RegisterRoutes mounts the print routes on the supplied router.
func (b *Battery) RegisterRoutes(r *router.Router) error {
	if b.mounted {
		return nil
	}
	if !strings.HasPrefix(b.cfg.PathPrefix, "/") {
		return fmt.Errorf("print: PathPrefix must be a path below the root, such as /print (got %q after trimming trailing slashes)", b.cfg.PathPrefix)
	}
	hdr := middleware.SecurityHeaders(middleware.SecurityHeadersConfig{ContentSecurityPolicy: printCSP})

	r.Get(b.cfg.PathPrefix+autoPrintSuffix, hdr(http.HandlerFunc(serveAutoPrint)))

	for _, doc := range b.docs {
		r.Get(b.cfg.PathPrefix+doc.Path, hdr(b.serveHTML(doc)))
		r.Get(b.cfg.PathPrefix+doc.Path+"/pdf", hdr(b.servePDF(doc)))
	}
	b.mounted = true
	return nil
}

func serveAutoPrint(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write([]byte(autoPrintScript))
}

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

func (b *Battery) build(w http.ResponseWriter, r *http.Request, doc *Document, forPDF bool) (string, PageConfig, bool) {
	comp, err := safeBuild(doc, r)
	if err != nil {
		writeBuildError(w, r, err)
		return "", PageConfig{}, false
	}

	body, err := component.SafeRenderCtx(r.Context(), comp)
	if err != nil {
		writeBuildError(w, r, err)
		return "", PageConfig{}, false
	}

	title, err := safeTitle(doc, r)
	if err != nil {
		writeBuildError(w, r, err)
		return "", PageConfig{}, false
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
			in.AppCSSHref = b.cfg.BaseURL + b.cfg.AppCSSURL
			if origin := cspOrigin(b.cfg.BaseURL); origin != "" {
				in.CSP = pdfCSP(origin)
			}
		case forPDF:
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

func safeTitle(doc *Document, r *http.Request) (title string, err error) {
	if doc.TitleFunc == nil {
		return doc.Title, nil
	}
	defer func() {
		if recover() != nil {
			err = errors.New("print: document title callback panicked")
		}
	}()
	return doc.TitleFunc(r), nil
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

func (b *Battery) accessOf(doc *Document) AccessPolicy {
	if doc.Access != nil {
		return doc.Access
	}
	return b.cfg.DefaultAccess
}

func (b *Battery) baseCSS() string {
	if b.cfg.PrintBaseCSS != "" {
		return b.cfg.PrintBaseCSS
	}
	return printBaseCSS
}

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

// PrintLink renders a new-tab link to a document, or empty HTML for an unknown one.
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
