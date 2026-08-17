package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/gallery"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	uitheme "github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// runThemeEdit boots a local theme configurator: an in-process core-ui
// app whose gallery screen is served by a real UIHost (same
// RegisterThemeVariant → /__gofastr/app.css?t=<key> path production uses),
// paired with a controls page that edits token values live.
func runThemeEdit(args []string) {
	addr := "127.0.0.1:0"
	outPath := "theme/theme.go"
	noOpen := false
	force := false
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "--addr="):
			addr = strings.TrimPrefix(a, "--addr=")
		case a == "--no-open":
			noOpen = true
		case strings.HasPrefix(a, "--out="):
			outPath = strings.TrimPrefix(a, "--out=")
		case a == "--force":
			force = true
		case a == "--help" || a == "-h" || a == "help":
			printThemeEditHelp()
			return
		default:
			fmt.Printf("%s Unknown flag: %s\n\n", red("✗"), a)
			printThemeEditHelp()
			osExit(1)
		}
	}

	// Confirm-before-overwrite unless --force. The editor regenerates the
	// file whole from the live session; an existing file is overwritten, so
	// the guard exists to stop a user clobbering hand-edited values by
	// accident.
	if !force {
		if _, err := os.Stat(outPath); err == nil {
			fmt.Printf("%s %s already exists.\n", yellow("⚠"), outPath)
			fmt.Println("  The editor regenerates it whole on write-back, which would")
			fmt.Println("  overwrite any hand-edited values. Pass --force to proceed, or")
			fmt.Println("  write to a new path via --out=<path>.")
			osExit(1)
		}
	}

	base := uitheme.Default()

	// Stand up the in-process core-ui app — no framework.App, no DB. The
	// gallery screen is a pure Component that iterates gallery.Grouped()
	// and renders each Demo closure. UIHost.New takes a core-ui/app.App
	// (not a framework.App); framework/uihost/themevariant_test.go does
	// exactly this.
	a := app.NewApp("theme-editor").WithTheme(base)
	a.Register("/preview", &galleryPreviewScreen{}, nil)

	host := uihost.New(a, uihost.WithCustomCSS(gallery.BaseCSS(base)+previewChromeCSS+contrastProbeCSS()))

	srv := &themeEditServer{
		host:    host,
		base:    base,
		working: base,
		outPath: outPath,
		force:   force,
	}

	// Bind first: the Host/Origin guards must be pinned to the authority we
	// actually got, which for the default "127.0.0.1:0" is only known after
	// net.Listen picks the ephemeral port. Same posture as harness_http.go.
	// A bare ":8090" means "port 8090 on this machine", and that is what the
	// operator wants — but a WILDCARD bind puts the tool on every interface,
	// and this tool serves an unauthenticated page carrying its own bearer
	// token and writes a Go file to disk. Resolve the friendly form to loopback
	// rather than either refusing it or honouring it.
	// An EXPLICIT non-loopback bind is refused. The Host pin below stops a
	// browser from rebinding DNS onto this port; it does not stop a direct TCP
	// client, which chooses its own Host header, fetches "/", and reads the
	// bearer token straight out of the page. Same reasoning the dev MCP bind
	// guard states (framework/devmcp_bind.go).
	bound, err := themeEditBindAddr(addr)
	if err != nil {
		fmt.Printf("%s refusing to bind the theme editor to %q: %v\n", red("✗"), addr, err)
		fmt.Println("  The page carries its own bearer token and the write-back endpoint")
		fmt.Println("  rewrites a Go file on disk. The Host pin stops DNS rebinding from a")
		fmt.Println("  browser, not a direct TCP client. Bind to 127.0.0.1 or localhost.")
		osExit(1)
	}
	addr = bound
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Printf("%s listen %s: %v\n", red("✗"), addr, err)
		osExit(1)
	}
	boundAddr := ln.Addr().String()
	srv.hosts, srv.origins = loopbackGuards(boundAddr)

	// Bearer token delivered in a <meta> tag, never a query string (query
	// strings leak via Referer, history, logs). The page JS sends it as
	// Authorization: Bearer on every POST.
	//
	// Freshly random, NOT deriveListenerSecret(). That function returns
	// sha256("harness-http:" || GOFASTR_HARNESS_MACHINE_KEY) when the machine
	// key is set, and those exact bytes are the HMAC key signing every harness
	// control-plane token. This page is served with no authentication at all —
	// it has to be, it is where the token comes from — so publishing that value
	// handed the harness signing key to any local process, any other user on
	// the box, any browser extension with 127.0.0.1 permission, and any
	// screenshot in a bug report. Nothing here needs a token that survives a
	// restart.
	tok, err := newThemeEditToken()
	if err != nil {
		fail("%v", err)
		osExit(1)
		return
	}
	srv.token = tok

	httpSrv := &http.Server{
		Handler:           srv,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() { _ = httpSrv.Serve(ln) }()

	url := "http://" + boundAddr
	fmt.Println("━─────────────────────────────────────────────")
	fmt.Println("  gofastr theme edit")
	fmt.Printf("  %s\n", url)
	fmt.Printf("  write-back → %s\n", outPath)
	fmt.Println("  Ctrl-C to stop")
	fmt.Println("━─────────────────────────────────────────────")

	if !noOpen {
		go openBrowser(url)
	}

	// Graceful shutdown on SIGINT/SIGTERM with a bounded timeout.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(ctx)
}

// newThemeEditToken returns a freshly random per-session bearer token. It
// exists as a named seam so the regression test can drive the same path
// runThemeEdit uses and assert the served token is NOT the harness signing
// key (deriveListenerSecret). See TestThemeEditTokenIsNotTheHarnessSigningKey.
func newThemeEditToken() (string, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", fmt.Errorf("generate session token: %w", err)
	}
	return hex.EncodeToString(secret), nil
}

// loopbackifyThemeAddr rewrites a wildcard bind to loopback, leaving every
// other form alone. ":8090", "0.0.0.0:8090" and "[::]:8090" all mean "this
// machine"; for a tool that must not leave it, that is 127.0.0.1.
func loopbackifyThemeAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if isWildcardHost(host) {
		return net.JoinHostPort("127.0.0.1", port)
	}
	return addr
}

// themeEditBindAddr resolves the requested bind address, or refuses it.
//
// It exists because the two guards it replaces were BOTH fail-open on a parse
// error, in sequence. loopbackifyThemeAddr returns its input unchanged when
// SplitHostPort fails, and the non-loopback refusal was written
// `if host, _, err := net.SplitHostPort(addr); err == nil && !isLoopbackHost(host)`
// — so an unparseable address skipped the refusal via the `err == nil` and went
// straight to net.Listen.
//
// The address that does this is the empty string. `net.SplitHostPort("")` fails
// ("missing port in address"), and `net.Listen("tcp", "")` binds EVERY
// interface. It arrives the ordinary way: `gofastr theme edit --addr=$THEME_ADDR`
// in a Makefile with the variable unset. The result was the editor — an
// unauthenticated page carrying its own bearer token, over an endpoint that
// rewrites a Go file on disk — published to the network, verified end to end on
// a real LAN interface.
//
// A bind address that cannot be parsed is now a refusal, not a default.
func themeEditBindAddr(addr string) (string, error) {
	if strings.TrimSpace(addr) == "" {
		return "", errors.New("empty bind address would bind every interface")
	}
	resolved := loopbackifyThemeAddr(addr)
	host, _, err := net.SplitHostPort(resolved)
	if err != nil {
		return "", fmt.Errorf("not a host:port address: %w", err)
	}
	if !isLoopbackHost(host) {
		return "", fmt.Errorf("%q is not loopback", host)
	}
	return resolved, nil
}

func printThemeEditHelp() {
	fmt.Println("Usage: gofastr theme edit [--addr=host:port] [--out=path] [--no-open] [--force]")
	fmt.Println()
	fmt.Println("Boots a local theme configurator with a live preview.")
	fmt.Println()
	fmt.Println("Flags:")
	fmt.Println("  --addr=host:port   Bind address (default 127.0.0.1:0 = ephemeral loopback).")
	fmt.Println("  --out=path         Write-back destination (default theme/theme.go).")
	fmt.Println("  --no-open          Do not open a browser automatically.")
	fmt.Println("  --force            Overwrite an existing --out file without prompting.")
}

// themeEditServer holds the live editing session. It is the http.Handler
// for the whole tool: it serves the controls page at "/", the JSON API
// under "/__theme/", and delegates everything else (the /preview gallery
// page plus /__gofastr/* framework assets) to the UIHost.
type themeEditServer struct {
	host    *uihost.UIHost
	base    style.Theme
	working style.Theme
	outPath string
	force   bool
	// wroteDigest is the sha256 of the bytes THIS session last wrote to
	// outPath. Without it the no-force guard refused the second Write of a
	// session: the first created the file, and the second saw the file the
	// tool itself had just written and reported "already exists; pass --force"
	// — about a file thirty seconds old, with no recovery but a restart that
	// loses every edit.
	wroteDigest [32]byte

	hosts   []string
	origins []string
	token   string

	mu sync.Mutex
	// previewKey is the variant the preview iframe should load — the WORKING
	// theme's, so a refresh mid-session does not silently drop back to the app
	// palette while the controls show edited values.
	previewKey string
}

// ServeHTTP routes between the editor chrome + API and the UIHost's
// framework assets. Every request passes the loopback host guard first.
func (s *themeEditServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !hostAllowed(r.Host, s.hosts) {
		http.Error(w, "forbidden: unexpected Host", http.StatusForbidden)
		return
	}
	switch {
	case r.URL.Path == "/":
		s.serveControlsPage(w, r)
	case strings.HasPrefix(r.URL.Path, "/__theme/"):
		s.serveAPI(w, r)
	default:
		// /preview and /__gofastr/* (app.css, runtime.js, component CSS).
		// Strip the frame-blocking headers so /preview can live inside the
		// controls page's <iframe>. Loopback binding already defeats the
		// clickjacking threat X-Frame-Options: DENY exists for.
		(&frameFriendlyWriter{rw: w}).serveHost(s.host, r)
	}
}

// applyToken applies one token edit to the working theme, re-registers the
// variant, and returns the content-addressed hash the preview swaps its
// app.css link to. On an invalid value ApplyTokens returns an error and the
// working theme is untouched — the boundary rejects, it does not sanitise.
func (s *themeEditServer) applyToken(key, value string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	updated, err := style.ApplyTokens(s.working, map[string]string{key: value})
	if err != nil {
		return "", err
	}
	// ApplyTokens is not the boundary the written file has to survive.
	// app.WithTheme calls MustValidate at boot, and the two disagree: the
	// spacing, radius and z-index setters accept 0 while Validate rejects it.
	// So a single keystroke — "0" in a number field — produced a green
	// "updated" status, wrote a theme.go, and panicked the operator's app on
	// the next run.
	if err := updated.Validate(); err != nil {
		return "", err
	}
	s.working = updated
	hash := s.host.RegisterThemeVariant(updated)
	// RegisterThemeVariant increments the hash's reference count even when
	// the edited value produces the current hash. Release the session's prior
	// reference after every successful registration so it owns exactly one.
	if s.previewKey != "" {
		s.host.ReleaseThemeVariant(s.previewKey)
	}
	s.previewKey = hash
	return hash, nil
}

// previewThemeKey is the variant key for the working theme, or "" when nothing
// has been edited yet.
func (s *themeEditServer) previewThemeKey() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.previewKey
}

// currentTokens returns the working theme flattened to the control map.
func (s *themeEditServer) currentTokens() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return style.ThemeToTokens(s.working)
}

// writeBack emits the working theme as a Go file and writes it to outPath.
func (s *themeEditServer) writeBack() error {
	s.mu.Lock()
	working := s.working
	outPath := s.outPath
	force := s.force
	wrote := s.wroteDigest
	s.mu.Unlock()

	dir := filepath.Dir(outPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	if !force {
		// A file THIS session wrote is ours to rewrite. Anything else is
		// someone's hand-authored theme and still needs --force. Without this
		// the button worked exactly once: the first Write created the file, and
		// every later Write refused, citing a file the tool itself had just
		// produced.
		if existing, err := os.ReadFile(outPath); err == nil && sha256.Sum256(existing) == wrote {
			force = true
		}
	}
	if !force {
		if _, err := os.Stat(outPath); err == nil {
			return fmt.Errorf("%s already exists; pass --force to replace it", outPath)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("stat %s: %w", outPath, err)
		}
	}

	src, err := emitThemeGoSource(working, packageNameForPath(outPath))
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(outPath)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary theme file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temporary theme file: %w", err)
	}
	if _, err := tmp.Write(src); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary theme file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("fsync temporary theme file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary theme file: %w", err)
	}
	// Remember what we wrote, so this session can rewrite its own file without
	// --force on the next click.
	s.mu.Lock()
	s.wroteDigest = sha256.Sum256(src)
	s.mu.Unlock()
	if err := os.Rename(tmpPath, outPath); err != nil {
		return fmt.Errorf("rename temporary theme file: %w", err)
	}
	return nil
}

// serveAPI dispatches the /__theme/* JSON endpoints. Every endpoint
// requires the bearer token (Authorization: Bearer <token>) and, for
// state-changing POSTs, a same-origin check.
func (s *themeEditServer) serveAPI(w http.ResponseWriter, r *http.Request) {
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

func (s *themeEditServer) handleTokens(w http.ResponseWriter, r *http.Request) {
	if !s.checkBearer(w, r) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	tokens := s.currentTokens()
	// Stable key order so the controls render deterministically.
	keys := make([]string, 0, len(tokens))
	for k := range tokens {
		keys = append(keys, k)
	}
	sortStrings(keys)
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

func (s *themeEditServer) handleApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.checkBearer(w, r) || !s.checkOrigin(w, r) {
		return
	}
	var req struct {
		Key, Value string
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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

func (s *themeEditServer) handleWriteback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.checkBearer(w, r) || !s.checkOrigin(w, r) {
		return
	}
	if err := s.writeBack(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "path": s.outPath})
}

// checkBearer verifies the Authorization: Bearer <token> header. The token
// is generated per-process and embedded in the page's <meta> tag, so a
// cross-origin page (DNS-rebinding) that lacks the token cannot drive the
// /__theme/apply or /__theme/writeback endpoints even if it passes the Host
// guard.
func (s *themeEditServer) checkBearer(w http.ResponseWriter, r *http.Request) bool {
	got := r.Header.Get("Authorization")
	want := "Bearer " + s.token
	// Constant-time, like every other secret comparison in the repo
	// (core/middleware/csrf.go, framework/experimental/harness/control/auth). Loopback
	// makes the timing channel unattractive rather than absent, and matching
	// the house rule costs nothing.
	if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

// checkOrigin rejects a POST whose Origin header names a different origin.
// curl/CLI callers send none and pass; a browser always sends one.
func (s *themeEditServer) checkOrigin(w http.ResponseWriter, r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	for _, allowed := range s.origins {
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

// tokenControlType classifies a ThemeToTokens key into the input type the
// controls page renders. Derived purely from the key prefix — the same
// prefix walkTokens/tokenPair use — so a token added to style.Theme later
// gets a usable control automatically. "color" tokens get a colour picker,
// integer-px and unitless-integer tokens get number inputs, everything else
// (fonts, shadows, durations, easings, font-sizes, code colours) gets a
// text input. An unrecognised prefix falls through to "text" — never hidden.
func tokenControlType(key string) string {
	base := strings.TrimPrefix(key, "dark.")
	switch {
	case strings.HasPrefix(base, "color-"):
		return "color"
	case strings.HasPrefix(base, "z-"):
		return "number"
	case strings.HasPrefix(base, "spacing-"), strings.HasPrefix(base, "radii-"), strings.HasPrefix(base, "breakpoint-"):
		return "number-px"
	default:
		return "text"
	}
}

// frameFriendlyWriter wraps the host's response so X-Frame-Options is
// dropped and CSP frame-ancestors widened from 'none' to 'self'. Without
// this the UIHost's SecurityHeaders middleware (X-Frame-Options: DENY,
// frame-ancestors 'none') blocks the /preview page from loading inside the
// controls page's <iframe>. The threat those headers exist for — a
// cross-origin attacker framing the page — is already defeated by loopback
// binding + host pinning; this is a dev-only loopback tool.
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

func (f *frameFriendlyWriter) Flush() {
	if fl, ok := f.rw.(http.Flusher); ok {
		f.strip()
		fl.Flush()
	}
}

// strip removes the frame-blocking headers exactly once, before the first
// byte goes on the wire. Modifying the header map after WriteHeader is a
// no-op, so this must run inside the WriteHeader/Write intercept.
func (f *frameFriendlyWriter) strip() {
	if f.flushed {
		return
	}
	f.flushed = true
	h := f.rw.Header()
	h.Del("X-Frame-Options")
	if csp := h.Get("Content-Security-Policy"); csp != "" {
		h.Set("Content-Security-Policy", strings.Replace(csp, "frame-ancestors 'none'", "frame-ancestors 'self'", 1))
	}
}

// serveHost delegates to the UIHost through the frame-header-stripping
// wrapper.
func (f *frameFriendlyWriter) serveHost(host http.Handler, r *http.Request) {
	host.ServeHTTP(f, r)
}

// galleryPreviewScreen is the component the UIHost renders at /preview. It
// renders the same gallery demos used by /components/<slug>, plus the contrast
// probes. The page composes gallery.BaseCSS, previewChromeCSS, and framework/ui
// primitives.
type galleryPreviewScreen struct{}

func (g *galleryPreviewScreen) ScreenTitle() string { return "Theme Preview" }

func (g *galleryPreviewScreen) Render() render.HTML {
	sections := []render.HTML{}
	for _, group := range gallery.Grouped() {
		demos := []render.HTML{}
		for _, entry := range group.Entries {
			if gallery.IsNoteOnly(entry.Slug) || entry.Demo == nil {
				continue
			}
			demos = append(demos, ui.Stack(ui.StackConfig{Gap: ui.GapSM},
				ui.Muted(render.Text(entry.Name)),
				entry.Demo(),
			))
		}
		if len(demos) == 0 {
			continue
		}
		sections = append(sections, ui.Section(ui.SectionConfig{Heading: group.Name},
			ui.Stack(ui.StackConfig{Gap: ui.GapLG}, demos...)))
	}
	// ui.Stack / ui.Section, not hand-rolled .tp-* markup with a CSS string
	// behind it. A page whose entire job is to show what the design system
	// looks like has no business laying itself out with anything else.
	return render.Join(
		ui.Stack(ui.StackConfig{Gap: ui.GapXL}, sections...),
		contrastProbeHTML(),
	)
}

// contrastProbeHTML emits a hidden region of elements whose computed
// styles resolve the token pairs the WCAG contrast check measures. Each
// probe carries a data-cp attribute naming the pair so the page JS can find
// it and read getComputedStyle().color + .backgroundColor — the browser
// resolves every colour space (hex, oklch, color-mix) natively, which is
// why the check runs in the browser and not in Go.
//
// The pairs are documented at core-ui/style/theme.go:425-449: text tiers
// on surface, primary-fg on primary, and each status tone BOTH as a
// white-text fill AND as label text on its own 15% tint (the tint is the
// harder target).
func contrastProbeHTML() render.HTML {
	var b strings.Builder
	b.WriteString(`<div class="tp-probes" aria-hidden="true">`)
	for _, p := range contrastPairs {
		fmt.Fprintf(&b, `<span data-cp=%q class=%q>a</span>`, p.name, "tp-probe--"+p.slug)
	}
	b.WriteString(`</div>`)
	return render.HTML(b.String())
}

// contrastPair is one foreground/background combination the checker measures.
//
// The colours live in a STYLESHEET, not in a style attribute. The preview is
// served by a real UIHost under the framework's default CSP — default-src
// 'self', no 'unsafe-inline' — so an inline style attribute is discarded by the
// browser. Every probe then measured the inherited text colour against a
// transparent background, ratio() came out around 20:1 for all of them, and the
// tool reported "no issues" for every theme ever loaded. A checker that cannot
// fail is worse than no checker: it is an assurance.
type contrastPair struct{ name, slug, fg, bg string }

var contrastPairs = buildContrastPairs()

func buildContrastPairs() []contrastPair {
	pairs := []contrastPair{
		// A readiness sentinel with literal colours and no var() references.
		// If it reads back as black-on-white the generated stylesheet has
		// applied; if it does not, nothing else on the page is worth
		// measuring yet. Without it the checker has no way to tell "the sheet
		// has not landed" from "this pair really is transparent", and guessing
		// either way produces a confident wrong answer.
		{"ready|sentinel", "ready", "#000000", "#ffffff"},
		{"text|surface", "text-surface", "var(--color-text)", "var(--color-surface)"},
		{"text-muted|surface", "text-muted-surface", "var(--color-text-muted)", "var(--color-surface)"},
		{"text-subtle|surface", "text-subtle-surface", "var(--color-text-subtle)", "var(--color-surface)"},
		{"primary-fg|primary", "primary-fg-primary", "var(--color-primary-fg)", "var(--color-primary)"},
	}
	// Each status tone twice: as a filled control, and as label text on its own
	// 15% tint. core-ui/style/theme.go is explicit that the tint is the harder
	// target, which is exactly why it must actually be measured.
	//
	// The fill's foreground is var(--color-primary-fg), because that is what the
	// design system paints there — `.ui-button--danger` and `.ui-badge--danger`
	// both set `color: var(--color-primary-fg)` on a `--color-danger`
	// background, and styles_components.go says so explicitly: "Themes that
	// override --color-danger own keeping >=4.5:1 against --color-primary-fg."
	//
	// Hardcoding #ffffff here measured a pair the UI never renders. In the
	// default dark scheme --color-primary-fg is #111827 and the status tones are
	// light, so the probe reported four failures — white on #F87171 at 2.77:1 —
	// for text nothing paints. A checker that invents failures is as useless as
	// one that cannot report them; both teach the operator to ignore it.
	for _, tone := range []string{"danger", "success", "warning", "info"} {
		pairs = append(pairs,
			contrastPair{"primary-fg|" + tone, "primary-fg-" + tone,
				"var(--color-primary-fg)", "var(--color-" + tone + ")"},
			contrastPair{tone + "|" + tone + "-tint", tone + "-tint", "var(--color-" + tone + ")",
				"color-mix(in srgb, var(--color-" + tone + ") 15%, var(--color-surface))"})
	}
	return pairs
}

// contrastProbeCSS renders the probe pairs as real CSS rules.
func contrastProbeCSS() string {
	var b strings.Builder
	for _, p := range contrastPairs {
		fmt.Fprintf(&b, ".tp-probe--%s { color: %s; background-color: %s; }\n", p.slug, p.fg, p.bg)
	}
	return b.String()
}

// previewChromeCSS only parks the contrast probes off-screen. Everything
// visible on the page is design-system output.
//
// The gallery owns its .demo-row / .demo-stack layout contract, so this page
// composes gallery.BaseCSS() with the block below.
var previewChromeCSS = `
.tp-probes { position: absolute; left: -9999px; top: -9999px; visibility: hidden; pointer-events: none; }
`

// htmlEscape escapes a string for safe inclusion in HTML text content.
// Delegates to the canonical 5-char escaper so no reduced-shape escaper
// (the documented past-XSS shape) survives in the tree.
func htmlEscape(s string) string {
	return render.Escape(s)
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

// openBrowser tries to open the OS default browser at url. Best-effort —
// a failure logs a hint and returns; the URL is already printed so the user
// can open it manually.
func openBrowser(url string) {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		c = exec.Command("open", url)
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		c = exec.Command("xdg-open", url)
	}
	if c == nil {
		return
	}
	if err := c.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "  (could not auto-open browser: %v)\n", err)
	}
}
