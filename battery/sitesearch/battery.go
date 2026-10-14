package sitesearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/static"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// Config configures the battery.
type Config struct {
	// Groups label results by URL prefix ("/docs/" → "Docs"), on whole
	// path segments ("/doc" never labels "/docs"); the longest matching
	// prefix wins. A page no prefix matches has no group.
	Groups []Group
	// Exclude keeps pages out of the index by path: "/lab" covers
	// "/lab" and every page under it, never "/labs". It applies to the
	// expanded paths, so one value of a dynamic route can go
	// ("/examples/headless/dense"), and to Sources' pages. An excluded
	// page is never rendered, so one that cannot render is no error.
	// Pages a static export leaves out are never indexed there either.
	Exclude []string
	// Sources add pages no route renders (records from a database, an
	// external changelog). They run on every build, live and export.
	Sources []Source
}

// Group labels results under one URL prefix.
type Group struct {
	Prefix, Label string
}

// Source returns pages to index beside the rendered routes.
type Source func(ctx context.Context) ([]Page, error)

// maxQueryBytes and maxLimit bound the query endpoint's parameters.
const (
	maxQueryBytes = 256
	defaultLimit  = 8
	maxLimit      = 20
)

// Battery serves a full-text index of the app's rendered pages. On a
// live server it renders every route (the static export's page set,
// StaticPaths expanded) once, on first use, and answers queries at
// Root+"query"; the same index files the static export writes are
// served under Root. Registered on an app that exports, it writes
// those files into the export, so the browser client searches a static
// host with no server.
type Battery struct {
	cfg Config
	app atomic.Pointer[framework.App]

	// mu guards the index and is held for a whole build, so a query
	// that arrives mid-build waits for it.
	mu    sync.Mutex
	idx   *Index
	files map[string][]byte

	// export state: the pages ExportPage saw since ExportStart.
	exportPages []Page

	// life guards warm, which closes when the build OnStart began has
	// finished. It is apart from mu so OnStop never waits on a build to
	// read it.
	life sync.Mutex
	warm chan struct{}
}

// New returns a site search battery.
func New(cfg Config) *Battery { return &Battery{cfg: cfg} }

// Name implements framework.Battery.
func (b *Battery) Name() string { return "sitesearch" }

// Init implements framework.Battery: it registers the query endpoint
// and the index files. The index builds on the first request.
func (b *Battery) Init(app *framework.App) error {
	b.app.Store(app)
	r := app.Router()
	r.Get(Root+"query", http.HandlerFunc(b.serveQuery))
	r.Get(Root+"{file}", http.HandlerFunc(b.serveFile))
	return nil
}

// OnStart implements framework.BatteryLifecycle: it builds the index in
// the background, so the first query does not wait for every page to
// render. A failed build is logged and retried by the next query.
func (b *Battery) OnStart(ctx context.Context) error {
	done := make(chan struct{})
	b.life.Lock()
	b.warm = done
	b.life.Unlock()
	app := b.app.Load()
	go func() {
		defer close(done)
		defer func() {
			if r := recover(); r != nil {
				app.Logger().Error("sitesearch: index build panicked", "panic", textsafe.Recovered(r))
			}
		}()
		if _, err := b.Index(ctx); err != nil && ctx.Err() == nil {
			app.Logger().Warn("sitesearch: index build failed; the next query retries", "err", err)
		}
	}()
	return nil
}

// OnStop implements framework.BatteryLifecycle: it waits for the build
// OnStart began, so shutdown does not close what the build renders with.
func (b *Battery) OnStop(ctx context.Context) error {
	b.life.Lock()
	done := b.warm
	b.life.Unlock()
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Refresh rebuilds the live index from the current pages; a content
// change that does not restart the server calls it.
func (b *Battery) Refresh(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.idx, b.files = nil, nil
	_, err := b.indexLocked(ctx)
	return err
}

// Index returns the live index, building it on first use.
func (b *Battery) Index(ctx context.Context) (*Index, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.indexLocked(ctx)
}

func (b *Battery) indexLocked(ctx context.Context) (*Index, error) {
	if b.idx != nil {
		return b.idx, nil
	}
	app := b.app.Load()
	if app == nil {
		return nil, fmt.Errorf("sitesearch: battery not initialized")
	}
	host := mountedHost(app)
	if host == nil {
		return nil, fmt.Errorf("sitesearch: no mounted uihost.UIHost to render pages from")
	}
	ctx, done := anonymous(ctx)
	defer done()
	var pages []Page
	err := static.Walk(ctx, host, b.excluded, func(path, page string) error {
		p, ok, err := b.extract(path, page)
		if err != nil {
			return err
		}
		if ok {
			pages = append(pages, p)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("sitesearch: render pages: %w", err)
	}
	idx, files, err := b.build(ctx, pages)
	if err != nil {
		return nil, err
	}
	b.idx, b.files = idx, files
	return idx, nil
}

// anonymous returns a context that ends when ctx does but carries none
// of its values. Every visitor searches the same index, so it is built
// as no one in particular: the identity of the request or the caller
// that asked for a build never reaches the pages' policies.
func anonymous(ctx context.Context) (context.Context, context.CancelFunc) {
	c, cancel := context.WithCancel(context.Background())
	stop := context.AfterFunc(ctx, cancel)
	return c, func() { stop(); cancel() }
}

func mountedHost(app *framework.App) *uihost.UIHost {
	for _, m := range app.Mountables() {
		if h, ok := m.(*uihost.UIHost); ok {
			return h
		}
	}
	return nil
}

// extract reads one rendered page and labels its group.
func (b *Battery) extract(path, page string) (Page, bool, error) {
	p, ok, err := Extract(path, strings.NewReader(page))
	if err != nil {
		return Page{}, false, fmt.Errorf("sitesearch: extract %q: %w", path, err)
	}
	if ok && p.Group == "" {
		p.Group = b.group(path)
	}
	return p, ok, nil
}

func (b *Battery) group(path string) string {
	best, label := -1, ""
	for _, g := range b.cfg.Groups {
		if under(path, g.Prefix) && len(g.Prefix) > best {
			best, label = len(g.Prefix), g.Label
		}
	}
	return label
}

// build adds the Sources' pages and builds the index and its files.
func (b *Battery) build(ctx context.Context, pages []Page) (*Index, map[string][]byte, error) {
	for _, src := range b.cfg.Sources {
		extra, err := src(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("sitesearch: source: %w", err)
		}
		for _, p := range extra {
			if b.excluded(p.URL) {
				continue
			}
			if p.Group == "" {
				p.Group = b.group(p.URL)
			}
			if len(p.Sections) == 0 {
				p.Sections = []Section{{}}
			}
			pages = append(pages, p)
		}
	}
	idx := Build(pages)
	files, err := idx.Files()
	if err != nil {
		return nil, nil, err
	}
	return idx, files, nil
}

// ExportStart implements static.Contributor: it drops the pages a
// build that failed partway collected.
func (b *Battery) ExportStart(context.Context) error {
	b.mu.Lock()
	b.exportPages = nil
	b.mu.Unlock()
	return nil
}

// ExportPage implements static.Contributor: it indexes one exported
// page.
func (b *Battery) ExportPage(path, html string) error {
	if b.excluded(path) {
		return nil
	}
	p, ok, err := b.extract(path, html)
	if err != nil || !ok {
		return err
	}
	b.mu.Lock()
	b.exportPages = append(b.exportPages, p)
	b.mu.Unlock()
	return nil
}

// ExportFiles implements static.Contributor: it returns the index the
// exported pages make, keyed under Root.
func (b *Battery) ExportFiles(ctx context.Context) (map[string][]byte, error) {
	b.mu.Lock()
	pages := b.exportPages
	b.exportPages = nil
	b.mu.Unlock()
	ctx, done := anonymous(ctx)
	defer done()
	_, files, err := b.build(ctx, pages)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]byte, len(files))
	for name, body := range files {
		out[Root+name] = body
	}
	return out, nil
}

// excluded applies Config.Exclude to a page's path.
func (b *Battery) excluded(path string) bool {
	for _, e := range b.cfg.Exclude {
		if under(path, e) {
			return true
		}
	}
	return false
}

// under reports whether path is prefix or below it, on whole segments:
// "/docs/" and "/docs" cover "/docs/x", never "/docsx".
func under(path, prefix string) bool {
	prefix = strings.TrimRight(prefix, "/")
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

func (b *Battery) serveQuery(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if len(q) > maxQueryBytes {
		http.Error(w, "query too long", http.StatusRequestURITooLong)
		return
	}
	limit := defaultLimit
	if s := r.URL.Query().Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 {
			http.Error(w, "bad limit", http.StatusBadRequest)
			return
		}
		limit = min(n, maxLimit)
	}
	idx, err := b.Index(r.Context())
	if err != nil {
		http.Error(w, "search index unavailable", http.StatusServiceUnavailable)
		return
	}
	results := idx.Search(q, limit)
	if results == nil {
		results = []Result{}
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(map[string]any{"results": results}); err != nil {
		http.Error(w, "encode", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(buf.Bytes())
}

func (b *Battery) serveFile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	b.mu.Lock()
	_, err := b.indexLocked(r.Context())
	body, ok := b.files[name]
	b.mu.Unlock()
	if err != nil {
		http.Error(w, "search index unavailable", http.StatusServiceUnavailable)
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if name == "manifest.json" {
		w.Header().Set("Cache-Control", "no-cache")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	_, _ = w.Write(body)
}

var (
	_ framework.Battery  = (*Battery)(nil)
	_ static.Contributor = (*Battery)(nil)
)
