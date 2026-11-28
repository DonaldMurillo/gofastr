package themecmd

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/loopback"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/projectfs"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/dev"
	"github.com/DonaldMurillo/gofastr/framework/gallery"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	uitheme "github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	"github.com/DonaldMurillo/gofastr/internal/retired"
)

const maxThemeEditBody = 64 << 10

func runThemeEdit(args []string) {
	addr := "127.0.0.1:0"
	outPath := "theme/theme.go"
	noOpen := false
	force := false
	if cli.HasHelpFlag(args) || slices.Contains(args, "help") {
		printThemeEditHelp()
		return
	}
	if _, err := cli.ParseFlags(args,
		map[string]*string{"--addr": &addr, "--out": &outPath},
		map[string]*bool{"--no-open": &noOpen, "--force": &force}); err != nil {
		cli.Fail("%v", err)
		fmt.Println()
		printThemeEditHelp()
		cli.OsExit(1)
		return
	}

	if !force {
		if _, err := projectfs.LstatFile(projectfs.Split(outPath)); err == nil {
			cli.Warn("%s already exists.", outPath)
			fmt.Println("  The editor regenerates it whole on write-back, which would")
			fmt.Println("  overwrite any hand-edited values. Pass --force to proceed, or")
			fmt.Println("  write to a new path via --out=<path>.")
			cli.OsExit(1)
		} else if !os.IsNotExist(err) {
			cli.Fail("stat %s: %v", outPath, err)
			cli.OsExit(1)
		}
	}

	if err := projectfs.Check(projectfs.Split(outPath)); err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
		return
	}

	base := uitheme.Default()

	a := app.NewApp("theme-editor").WithTheme(base)
	a.Register("/preview", &GalleryPreviewScreen{}, nil)

	host := uihost.New(a, uihost.WithCustomCSS(previewChromeCSS+contrastProbeCSS()))

	srv := &ThemeEditServer{
		Host:    host,
		Base:    base,
		Working: base,
		OutPath: outPath,
		force:   force,
	}

	bound, err := themeEditBindAddr(addr)
	if err != nil {
		cli.Fail("refusing to bind the theme editor to %q: %v", addr, err)
		fmt.Println("  The page carries its own bearer token and the write-back endpoint")
		fmt.Println("  rewrites a Go file on disk. The Host pin stops DNS rebinding from a")
		fmt.Println("  browser, not a direct TCP client. Bind to 127.0.0.1 or localhost.")
		cli.OsExit(1)
	}
	addr = bound
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		cli.Fail("listen %s: %v", addr, err)
		cli.OsExit(1)
	}
	boundAddr := ln.Addr().String()
	srv.Hosts, srv.Origins = loopback.LoopbackGuards(boundAddr)

	tok, err := NewThemeEditToken()
	if err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
		return
	}
	srv.Token = tok

	httpSrv := &http.Server{
		Handler:           srv,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() { _ = httpSrv.Serve(ln) }()

	url := "http://" + boundAddr
	fmt.Println("━─────────────────────────────────────────────")
	fmt.Println("  gofastr theme edit")
	cli.Printf("  %s\n", url)
	cli.Printf("  write-back → %s\n", outPath)
	fmt.Println("  Ctrl-C to stop")
	fmt.Println("━─────────────────────────────────────────────")

	if !noOpen {
		go cli.OpenBrowser(url)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(ctx)
}

// NewThemeEditToken returns a fresh random 32-byte bearer token, hex-encoded,
// for one editor session.
func NewThemeEditToken() (string, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", fmt.Errorf("generate session token: %w", err)
	}
	return hex.EncodeToString(secret), nil
}

func loopbackifyThemeAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if loopback.IsWildcardHost(host) {
		return net.JoinHostPort("127.0.0.1", port)
	}
	return addr
}

func themeEditBindAddr(addr string) (string, error) {
	if strings.TrimSpace(addr) == "" {
		return "", errors.New("empty bind address would bind every interface")
	}
	resolved := loopbackifyThemeAddr(addr)
	host, _, err := net.SplitHostPort(resolved)
	if err != nil {
		return "", fmt.Errorf("not a host:port address: %w", err)
	}
	if !loopback.IsLoopbackHost(host) {
		return "", fmt.Errorf("%q is not loopback", host)
	}
	return resolved, nil
}

func printThemeEditHelp() {
	fmt.Println("Usage: gofastr theme edit [--addr=host:port] [--out=path] [--no-open] [--force]")
	fmt.Println()
	fmt.Println("Boots a local theme configurator with a live preview.")
	fmt.Println()
	fmt.Println("The controls pane groups tokens (Colors, Component options,")
	fmt.Println("Spacing, ...). Component options render as selects listing the")
	fmt.Println("values each option accepts; picking one re-renders the preview.")
	fmt.Println()
	fmt.Println("Flags:")
	fmt.Println("  --addr=host:port   Bind address (default 127.0.0.1:0 = ephemeral loopback).")
	fmt.Println("  --out=path         Write-back destination (default theme/theme.go).")
	fmt.Println("  --no-open          Do not open a browser automatically.")
	fmt.Println("  --force            Overwrite an existing --out file without prompting.")
}

// ThemeEditServer is one editing session and the tool's http.Handler: the
// controls page at /, the JSON API under /__theme/, and the UIHost for the
// rest.
type ThemeEditServer struct {
	Host        *uihost.UIHost
	Base        style.Theme
	Working     style.Theme
	OutPath     string
	force       bool
	wroteDigest [32]byte

	Hosts   []string
	Origins []string
	Token   string

	mu             sync.Mutex
	previewKey     string
	previewOnce    sync.Once
	previewHandler http.Handler
}

// ServeHTTP refuses a Host the session did not bind, then routes the request.
func (s *ThemeEditServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !loopback.HostAllowed(r.Host, s.Hosts) {
		http.Error(w, "forbidden: unexpected Host", http.StatusForbidden)
		return
	}
	switch {
	case r.URL.Path == "/":
		s.ServeControlsPage(w, r)
	case strings.HasPrefix(r.URL.Path, "/__theme/"):
		s.serveAPI(w, r)
	default:
		s.previewOnce.Do(func() {
			routes := router.New()
			routes.Use(middleware.SecurityHeaders(middleware.SecurityHeadersConfig{}))
			routes.Use(retired.Middleware(dev.Enabled))
			pages := []string{"/preview"}
			gallery.MountDemoWidgets(routes, gallery.DemoWidgetPages{
				Modal: pages, Drawer: pages, BottomSheet: pages, Sidebar: pages,
			})
			gallery.MountDemoFormRoute(routes, func(string) string { return "/preview" })
			s.Host.Mount(routes)
			s.previewHandler = routes
		})
		(&frameFriendlyWriter{rw: w}).serveHost(s.previewHandler, r)
	}
}

func (s *ThemeEditServer) applyToken(key, value string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	updated, err := style.ApplyTokens(s.Working, map[string]string{key: value})
	if err != nil {
		return "", err
	}
	if err := updated.Validate(); err != nil {
		return "", err
	}
	s.Working = updated
	hash := s.Host.RegisterThemeVariant(updated)
	if s.previewKey != "" {
		s.Host.ReleaseThemeVariant(s.previewKey)
	}
	s.previewKey = hash
	return hash, nil
}

func (s *ThemeEditServer) previewThemeKey() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.previewKey
}

func (s *ThemeEditServer) currentTokens() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return style.ThemeToTokens(s.Working)
}

func (s *ThemeEditServer) writeBack() error {
	s.mu.Lock()
	working := s.Working
	outPath := s.OutPath
	force := s.force
	wrote := s.wroteDigest
	s.mu.Unlock()

	rootDir, rel, err := projectfs.UserPath(outPath)
	if err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(outPath), err)
	}
	if err := projectfs.MkdirAll(rootDir, path.Dir(rel), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(outPath), err)
	}
	if !force {
		if existing, err := projectfs.ReadFile(rootDir, rel); err == nil && sha256.Sum256(existing) == wrote {
			force = true
		}
	}
	if !force {
		if _, err := projectfs.LstatFile(rootDir, rel); err == nil {
			return fmt.Errorf("%s already exists; pass --force to replace it", outPath)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("stat %s: %w", outPath, err)
		}
	}

	src, err := emitThemeGoSource(working, packageNameForPath(outPath))
	if err != nil {
		return err
	}
	if err := projectfs.WriteFile(rootDir, rel, src, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", outPath, err)
	}
	s.mu.Lock()
	s.wroteDigest = sha256.Sum256(src)
	s.mu.Unlock()
	return nil
}

func (s *ThemeEditServer) serveAPI(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/__theme/tokens":
		s.handleTokens(w, r)
	case "/__theme/apply":
		s.handleApply(w, r)
	case "/__theme/writeback":
		s.handleWriteback(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *ThemeEditServer) handleTokens(w http.ResponseWriter, r *http.Request) {
	if !s.checkBearer(w, r) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	tokens := s.currentTokens()
	keys := make([]string, 0, len(tokens))
	for k := range tokens {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	type tokenOut struct {
		Key   string `json:"key"`
		Value string `json:"value"`
		Type  string `json:"type"`
	}
	out := make([]tokenOut, 0, len(keys))
	for _, k := range keys {
		out = append(out, tokenOut{Key: k, Value: tokens[k], Type: tokenControlType(k)})
	}
	_ = json.NewEncoder(w).Encode(out)
}

func (s *ThemeEditServer) handleApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.checkBearer(w, r) || !s.checkOrigin(w, r) {
		return
	}
	var req struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxThemeEditBody)
	if err := handler.DecodeStrict(r.Body, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "malformed JSON body")
		return
	}
	if req.Key == "" {
		writeJSONError(w, http.StatusBadRequest, "missing key")
		return
	}
	hash, err := s.applyToken(req.Key, req.Value)
	if err != nil {
		writeJSONError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"hash": hash})
}

func (s *ThemeEditServer) handleWriteback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.checkBearer(w, r) || !s.checkOrigin(w, r) {
		return
	}
	if err := s.writeBack(); err != nil {
		log.Printf("theme edit: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "could not apply the edit")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "path": s.OutPath})
}

func (s *ThemeEditServer) checkBearer(w http.ResponseWriter, r *http.Request) bool {
	got := r.Header.Get("Authorization")
	want := "Bearer " + s.Token
	if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

func (s *ThemeEditServer) checkOrigin(w http.ResponseWriter, r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	for _, allowed := range s.Origins {
		if origin == allowed {
			return true
		}
	}
	http.Error(w, "forbidden: unexpected Origin", http.StatusForbidden)
	return false
}

func writeJSONError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func tokenControlType(key string) string {
	base := strings.TrimPrefix(key, "dark.")
	switch {
	case strings.HasPrefix(base, "color-"):
		return "color"
	case strings.HasPrefix(base, "component."):
		return "select"
	case strings.HasPrefix(base, "z-"), strings.HasPrefix(base, "font-weight-"):
		return "number"
	case strings.HasPrefix(base, "spacing-"), strings.HasPrefix(base, "radii-"), strings.HasPrefix(base, "breakpoint-"):
		return "number-px"
	default:
		return "text"
	}
}

type frameFriendlyWriter struct {
	rw      http.ResponseWriter
	flushed bool
}

func (f *frameFriendlyWriter) Header() http.Header {
	return f.rw.Header()
}

func (f *frameFriendlyWriter) WriteHeader(code int) {
	f.strip()
	f.rw.WriteHeader(code)
}

func (f *frameFriendlyWriter) Write(b []byte) (int, error) {
	f.strip()
	return f.rw.Write(b)
}

func (f *frameFriendlyWriter) ReadFrom(src io.Reader) (int64, error) {
	f.strip()
	return io.Copy(f.rw, src)
}

func (f *frameFriendlyWriter) Flush() {
	if fl, ok := f.rw.(http.Flusher); ok {
		f.strip()
		fl.Flush()
	}
}

func (f *frameFriendlyWriter) Unwrap() http.ResponseWriter { return f.rw }

func (f *frameFriendlyWriter) strip() {
	if f.flushed {
		return
	}
	f.flushed = true
	h := f.rw.Header()
	h.Del("X-Frame-Options")
	fields := slices.Clone(h.Values("Content-Security-Policy"))
	h.Del("Content-Security-Policy")
	for _, csp := range fields {
		h.Add("Content-Security-Policy", strings.ReplaceAll(csp, "frame-ancestors 'none'", "frame-ancestors 'self'"))
	}
}

func (f *frameFriendlyWriter) serveHost(host http.Handler, r *http.Request) {
	host.ServeHTTP(f, r)
}

// GalleryPreviewScreen renders the gallery previews and contrast probes at /preview.
type GalleryPreviewScreen struct{}

func (g *GalleryPreviewScreen) ScreenTitle(ctx context.Context) string { return "Theme Preview" }

func (g *GalleryPreviewScreen) Render() render.HTML {
	sections := []render.HTML{}
	for _, group := range gallery.Grouped() {
		demos := []render.HTML{}
		for _, entry := range group.Entries {
			if gallery.IsNoteOnly(entry.Slug) || entry.Demo == nil {
				continue
			}
			name, demo := entry.Name, entry.Demo()
			if entry.Slug == "stepwizard" {
				name += " (static preview)"
				demo = html.Div(html.DivConfig{ExtraAttrs: html.Attrs{"inert": ""}}, demo)
			}
			demos = append(demos, ui.Stack(ui.StackConfig{Gap: ui.GapSM},
				ui.Muted(render.Text(name)),
				html.Div(html.DivConfig{}, demo),
			))
		}
		if len(demos) == 0 {
			continue
		}
		sections = append(sections, ui.Section(ui.SectionConfig{Heading: group.Name},
			ui.Stack(ui.StackConfig{Gap: ui.GapLG}, demos...)))
	}
	return render.Join(
		ui.Stack(ui.StackConfig{Gap: ui.GapXL}, sections...),
		contrastProbeHTML(),
	)
}

func contrastProbeHTML() render.HTML {
	var b strings.Builder
	b.WriteString(`<div class="tp-probes" aria-hidden="true">`)
	for _, p := range contrastPairs {
		fmt.Fprintf(&b, `<span data-cp=%q class=%q>a</span>`, p.name, "tp-probe--"+p.slug)
	}
	b.WriteString(`</div>`)
	return render.HTML(b.String())
}

type contrastPair struct{ name, slug, fg, bg string }

var contrastPairs = buildContrastPairs()

func buildContrastPairs() []contrastPair {
	pairs := []contrastPair{
		{"ready|sentinel", "ready", "#000000", "#ffffff"},
		{"text|surface", "text-surface", "var(--color-text)", "var(--color-surface)"},
		{"text-muted|surface", "text-muted-surface", "var(--color-text-muted)", "var(--color-surface)"},
		{"text-subtle|surface", "text-subtle-surface", "var(--color-text-subtle)", "var(--color-surface)"},
		{"primary-fg|primary", "primary-fg-primary", "var(--color-primary-fg)", "var(--color-primary)"},
	}
	for _, tone := range []string{"danger", "success", "warning", "info"} {
		fillFg := "var(--color-primary-fg)"
		if tone == "danger" {
			fillFg = "var(--color-danger-fg)"
		}
		pairs = append(pairs,
			contrastPair{tone + "-fg|" + tone, tone + "-fg-" + tone,
				fillFg, "var(--color-" + tone + ")"},
			contrastPair{tone + "|" + tone + "-tint", tone + "-tint", "var(--color-" + tone + ")",
				"color-mix(in oklab, var(--color-" + tone + ") 15%, var(--color-surface))"},
		)
	}
	return pairs
}

func contrastProbeCSS() string {
	var b strings.Builder
	for _, p := range contrastPairs {
		fmt.Fprintf(&b, ".tp-probe--%s { color: %s; background-color: %s; }\n", p.slug, p.fg, p.bg)
	}
	return b.String()
}

var previewChromeCSS = `
.tp-probes { position: absolute; left: -9999px; top: -9999px; visibility: hidden; pointer-events: none; }
`
