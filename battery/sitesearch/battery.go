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
	"time"

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
	// MaxAge is how old the live index may get: a query that finds it
	// older starts one rebuild in the background and is answered from
	// the old index meanwhile. Zero never rebuilds; call Refresh after
	// a change instead. New panics on a negative MaxAge.
	MaxAge time.Duration
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

// buildRetry is how long a failed build answers with its error before a
// query may start another one. Each build renders every page, so
// without it every query to a broken site would render the site again.
const buildRetry = 30 * time.Second

// Battery serves a full-text index of the app's rendered pages. On a
// live server it renders every route (the static export's page set,
// StaticPaths expanded) when the app starts, again in the background
// once the index is older than Config.MaxAge, and answers queries at
// Root+"query"; the same index files the static export writes are
// served under Root. Registered on an app that exports, it writes
// those files into the export, so the browser client searches a static
// host with no server.
type Battery struct {
	cfg Config
	app atomic.Pointer[framework.App]

	// building serializes builds: one renders the site at a time.
	building sync.Mutex
	// mu guards the index state. It is held to read or swap it, never
	// across a render, so a query waits only for the first build.
	mu      sync.Mutex
	idx     *Index
	files   map[string][]byte
	builtAt time.Time
	// failed and failedAt hold the last failed build: with no index it
	// is returned, and either way no build retries, until buildRetry
	// has passed.
	failed   error
	failedAt time.Time
	// rebuilding is set while a background rebuild runs.
	rebuilding bool

	// export state: the pages ExportPage saw since ExportStart.
	exportPages []Page

	// life guards warm, which closes when the build OnStart began has
	// finished, and the background rebuild in flight: rebuilt closes
	// when it ends, and stopRebuild ends it. stopped refuses new
	// rebuilds once OnStop has run. It is apart from mu so OnStop never
	// waits on a build to read it.
	life        sync.Mutex
	warm        chan struct{}
	rebuilt     chan struct{}
	stopRebuild context.CancelFunc
	stopped     bool
}

// New returns a site search battery. It panics on a negative MaxAge.
func New(cfg Config) *Battery {
	if cfg.MaxAge < 0 {
		panic("sitesearch: negative Config.MaxAge")
	}
	return &Battery{cfg: cfg}
}

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
// OnStart began, so shutdown does not close what the build renders
// with, and ends a background rebuild and waits for it.
func (b *Battery) OnStop(ctx context.Context) error {
	b.life.Lock()
	b.stopped = true
	warm, rebuilt := b.warm, b.rebuilt
	if b.stopRebuild != nil {
		b.stopRebuild()
	}
	b.life.Unlock()
	for _, done := range []chan struct{}{warm, rebuilt} {
		if done == nil {
			continue
		}
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// Refresh rebuilds the live index from the current pages; a content
// change that does not restart the server calls it. Queries keep the
// old index until the new one is built. A failed Refresh keeps the old
// index and returns the error.
func (b *Battery) Refresh(ctx context.Context) error {
	b.building.Lock()
	defer b.building.Unlock()
	idx, files, err := b.render(ctx)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.settle(ctx, idx, files, err)
	return err
}

// Index returns the live index, building it on first use. A query
// that finds it older than Config.MaxAge gets it at once and starts a
// rebuild in the background.
func (b *Battery) Index(ctx context.Context) (*Index, error) {
	idx, _, err := b.current(ctx)
	return idx, err
}

// current returns the live index and its files, building them on first
// use.
func (b *Battery) current(ctx context.Context) (*Index, map[string][]byte, error) {
	b.mu.Lock()
	if idx, files := b.idx, b.files; idx != nil {
		if b.expired() {
			b.rebuild()
		}
		b.mu.Unlock()
		return idx, files, nil
	}
	b.mu.Unlock()

	b.building.Lock()
	defer b.building.Unlock()
	// The build this one waited behind may have made the index, or
	// failed recently enough to answer for this one.
	b.mu.Lock()
	idx, files, failed := b.idx, b.files, b.failed
	retry := failed != nil && time.Since(b.failedAt) < buildRetry
	b.mu.Unlock()
	if idx != nil {
		return idx, files, nil
	}
	if retry {
		return nil, nil, failed
	}
	idx, files, err := b.render(ctx)
	b.mu.Lock()
	b.settle(ctx, idx, files, err)
	b.mu.Unlock()
	return idx, files, err
}

// settle records a build's outcome under mu: a built index replaces the
// live one, and a failure is held for buildRetry. A build its caller
// cut short did not fail; the next one runs.
func (b *Battery) settle(ctx context.Context, idx *Index, files map[string][]byte, err error) {
	if err != nil {
		if ctx.Err() == nil {
			b.failed, b.failedAt = err, time.Now()
		}
		return
	}
	b.idx, b.files, b.builtAt, b.failed = idx, files, time.Now(), nil
}

// expired reports, under mu, whether the live index is past MaxAge and
// no rebuild runs or waits out a failure.
func (b *Battery) expired() bool {
	return b.cfg.MaxAge > 0 && time.Since(b.builtAt) >= b.cfg.MaxAge && !b.rebuilding &&
		(b.failed == nil || time.Since(b.failedAt) >= buildRetry)
}

// rebuild starts a background rebuild, under mu. OnStop ends it.
func (b *Battery) rebuild() {
	b.life.Lock()
	defer b.life.Unlock()
	if b.stopped {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	b.rebuilt, b.stopRebuild, b.rebuilding = done, cancel, true
	app := b.app.Load()
	go func() {
		defer close(done)
		defer cancel()
		defer func() {
			if r := recover(); r != nil {
				app.Logger().Error("sitesearch: index rebuild panicked", "panic", textsafe.Recovered(r))
			}
			b.mu.Lock()
			b.rebuilding = false
			b.mu.Unlock()
		}()
		b.building.Lock()
		defer b.building.Unlock()
		idx, files, err := b.render(ctx)
		b.mu.Lock()
		b.settle(ctx, idx, files, err)
		b.mu.Unlock()
		if err != nil && ctx.Err() == nil {
			app.Logger().Warn("sitesearch: index rebuild failed; serving the old index", "err", err)
		}
	}()
}

// render renders the pages and builds the index and its files. A page
// that fails to render is logged and left out: one broken page must
// not take search down for the whole site. An export, which bakes what
// it renders, refuses such a page instead.
func (b *Battery) render(ctx context.Context) (*Index, map[string][]byte, error) {
	app := b.app.Load()
	if app == nil {
		return nil, nil, fmt.Errorf("sitesearch: battery not initialized")
	}
	host := mountedHost(app)
	if host == nil {
		return nil, nil, fmt.Errorf("sitesearch: no mounted uihost.UIHost to render pages from")
	}
	ctx, done := anonymous(ctx)
	defer done()
	var pages []Page
	err := static.Walk(ctx, host, b.excluded, func(path, page string, err error) error {
		if err != nil {
			if ctx.Err() != nil {
				return err
			}
			app.Logger().Warn("sitesearch: page left out of the index", "path", path, "err", textsafe.StripUnsafe(err.Error()))
			return nil
		}
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
		return nil, nil, fmt.Errorf("sitesearch: render pages: %w", err)
	}
	return b.build(ctx, pages)
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
	_, files, err := b.current(r.Context())
	body, ok := files[name]
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
