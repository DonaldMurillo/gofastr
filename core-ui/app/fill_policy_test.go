package app_test

// Unit tests for the decided fill policies (DESIGN-layout-outlets.md,
// "Fills"): candidates load concurrently, one goroutine per outlet,
// bounded; a failing fill is contained to its outlet. The P2/P3
// variant switches (GOFASTR_SPIKE_FILL_ERRORS / _LOAD) are gone — the
// losing variants were deleted, and these tests pin the only path.
// TestFillErrorContainedToOutlet (layout_tree_test.go) carries the
// design-named containment matrix; the tests here pin its details:
// log scrubbing, hostile-text containment, the partial path, the
// declaration-order failure record, and the concurrency contract.

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/store"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// hostileErrorText carries markup, a C1 CSI byte, and a bidi override:
// none of it may reach the HTML raw, and the log line must be scrubbed
// of the control bytes.
const hostileErrorText = "boom <img src=x onerror=alert(1)>\u009b[31m\u202eevil"

// boomLoad's Load returns a hostile non-ErrNoFill error.
type boomLoad struct{}

func (b *boomLoad) Load(ctx context.Context) error { return errors.New(hostileErrorText) }
func (b *boomLoad) Render() render.HTML            { return render.Text("[BOOM-LOAD]") }

// boomRender's Render panics with hostile text.
type boomRender struct{}

func (b *boomRender) Render() render.HTML { panic(hostileErrorText) }

// boomBoundary panics on Render and provides an ErrorBoundary whose
// fallback is a fixed marker (never the error text).
type boomBoundary struct{}

func (b *boomBoundary) Render() render.HTML { panic(hostileErrorText) }
func (b *boomBoundary) RenderError(err error) render.HTML {
	return render.Text("[BOUNDARY-FALLBACK]")
}

// paramPanicFill panics in SetParams, outside the Load/render
// containment: the resolution recover must convert it, never let it
// escape the goroutine.
type paramPanicFill struct {
	id string
}

func (p *paramPanicFill) SetParams(m map[string]string) { panic("SetParams: " + hostileErrorText) }
func (p *paramPanicFill) Render() render.HTML           { return render.Text("[PARAM-PANIC]") }

// dynComp is a screen component that accepts route params.
type dynComp struct {
	id string
}

func (d *dynComp) SetParams(m map[string]string) { d.id = m["id"] }
func (d *dynComp) Render() render.HTML           { return render.Text("DYN-" + d.id) }

// eventLog records load outcomes across goroutines.
type eventLog struct {
	mu  sync.Mutex
	log []string
}

func (e *eventLog) record(s string) { e.mu.Lock(); e.log = append(e.log, s); e.mu.Unlock() }

func (e *eventLog) snapshot() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.log...)
}

func (e *eventLog) has(s string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, l := range e.log {
		if l == s {
			return true
		}
	}
	return false
}

// sleepyFill sleeps (honouring ctx cancellation) then loads or errors.
type sleepyFill struct {
	label   string
	d       time.Duration
	loadErr error
	events  *eventLog
}

func (f *sleepyFill) Load(ctx context.Context) error {
	if f.events != nil {
		f.events.record(f.label + ":start")
	}
	if f.d > 0 {
		select {
		case <-time.After(f.d):
		case <-ctx.Done():
			if f.events != nil {
				f.events.record(f.label + ":cancelled")
			}
			return ctx.Err()
		}
	}
	if f.events != nil {
		f.events.record(f.label + ":done")
	}
	return f.loadErr
}

func (f *sleepyFill) Render() render.HTML { return render.Text("[" + f.label + "]") }

// captureSlog swaps the default logger for a text handler over a
// buffer and restores it on cleanup.
func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

// assertNoHostileEcho fails when control bytes from the hostile error
// text appear raw in html or the captured log, or when markup appears
// raw in html. Markup in a LOG line is inert (a log is not HTML) and
// the screen path's own logging passes it through the same way; the C1
// CSI and bidi bytes are what textsafe exists to strip.
func assertNoHostileEcho(t *testing.T, html, logs string) {
	t.Helper()
	if strings.Contains(html, "onerror") || strings.Contains(html, "\u009b") || strings.Contains(html, "\u202e") {
		t.Errorf("hostile error text leaked into HTML raw: %q", html)
	}
	if strings.Contains(logs, "\u009b") || strings.Contains(logs, "\u202e") {
		t.Errorf("contained-failure log not scrubbed of control bytes: %q", logs)
	}
}

// TestP2FillLoadError pins the load-error containment details: the
// outlet degrades to its Default, the failure is logged once, the
// hostile error text never reaches the HTML raw, and the log line is
// scrubbed of control bytes.
func TestP2FillLoadError(t *testing.T) {
	a := app.NewApp("t")
	shell, outlets := labShell()
	a.SetDefaultLayout(shell)
	a.RegisterScreen(app.NewScreen("/", &stubComp{html: "HOME"}).
		Fill(outlets.Aside, &boomLoad{}), nil)
	logs := captureSlog(t)

	res, err := a.RenderPageResult(context.Background(), "/")
	if err != nil {
		t.Fatalf("a fill Load error must be contained to its outlet: %v", err)
	}
	html := string(res.HTML)
	if !strings.Contains(html, "HOME") {
		t.Errorf("page content missing under containment: %s", html)
	}
	if !strings.Contains(html, "[HELP]") {
		t.Errorf("aside must degrade to its Default: %s", html)
	}
	if !strings.Contains(logs.String(), "app: fill failed; outlet degraded to fallback") {
		t.Errorf("containment must log the failure, got: %q", logs.String())
	}
	assertNoHostileEcho(t, html, logs.String())
}

// TestP2FillRenderPanic pins the render-panic containment: the outlet
// degrades to its Default without leaking SafeRenderCtx's generic
// error box.
func TestP2FillRenderPanic(t *testing.T) {
	a := app.NewApp("t")
	shell, outlets := labShell()
	a.SetDefaultLayout(shell)
	a.RegisterScreen(app.NewScreen("/", &stubComp{html: "HOME"}).
		Fill(outlets.Aside, &boomRender{}), nil)
	logs := captureSlog(t)

	res, err := a.RenderPageResult(context.Background(), "/")
	if err != nil {
		t.Fatalf("a fill render panic must be contained to its outlet: %v", err)
	}
	html := string(res.HTML)
	if !strings.Contains(html, "[HELP]") {
		t.Errorf("aside must degrade to its Default: %s", html)
	}
	if strings.Contains(html, "cui-render-error") {
		t.Errorf("generic error box leaked into a contained outlet: %s", html)
	}
	if !strings.Contains(logs.String(), "app: fill failed; outlet degraded to fallback") {
		t.Errorf("containment must log the failure, got: %q", logs.String())
	}
	assertNoHostileEcho(t, html, logs.String())
}

// TestP2FillBoundaryFallback: a fill implementing
// component.ErrorBoundary that panics gets its own fallback in the
// outlet; the Default never overrides it.
func TestP2FillBoundaryFallback(t *testing.T) {
	a := app.NewApp("t")
	shell, outlets := labShell()
	a.SetDefaultLayout(shell)
	a.RegisterScreen(app.NewScreen("/", &stubComp{html: "HOME"}).
		Fill(outlets.Aside, &boomBoundary{}), nil)
	captureSlog(t)

	res, err := a.RenderPageResult(context.Background(), "/")
	if err != nil {
		t.Fatalf("a boundary fill's panic must be contained: %v", err)
	}
	html := string(res.HTML)
	if !strings.Contains(html, "[BOUNDARY-FALLBACK]") {
		t.Errorf("aside must carry the boundary's own fallback: %s", html)
	}
	if strings.Contains(html, "[HELP]") {
		t.Errorf("Default must not override the boundary's fallback: %s", html)
	}
}

// TestP2ErrNoFillStillDeclines pins that ErrNoFill declines: the next
// candidate resolves, and a decline is never a contained failure.
func TestP2ErrNoFillStillDeclines(t *testing.T) {
	a := app.NewApp("t")
	shell, outlets := labShell()
	a.SetDefaultLayout(shell)
	var loaded []string
	a.RegisterScreen(app.NewScreen("/", &stubComp{html: "HOME"}).
		Fill(outlets.Toolbar, &fillComp{label: "DECLINED", loadErr: app.ErrNoFill}).
		Fill(outlets.Aside, &fillComp{label: "ALSO_OUT", loaded: &loaded, loadErr: app.ErrNoFill}), nil)

	res, err := a.RenderPageResult(context.Background(), "/")
	if err != nil {
		t.Fatal(err)
	}
	html := string(res.HTML)
	if strings.Contains(html, "[DECLINED]") || strings.Contains(html, "[ALSO_OUT]") {
		t.Errorf("declined candidates must not render: %s", html)
	}
	if !strings.Contains(html, "[HELP]") {
		t.Errorf("aside must fall to its Default after declines: %s", html)
	}
}

// TestP2PartialPathContainment runs the same broken aside through the
// subtree-partial path (RenderPartialFromResult), where the failing
// outlet belongs to a KEPT layer and its degraded fill travels on the
// result.
func TestP2PartialPathContainment(t *testing.T) {
	a := app.NewApp("t")
	shell, outlets := labShell()
	a.SetDefaultLayout(shell)
	a.RegisterScreen(app.NewScreen("/", &stubComp{html: "HOME"}), nil)
	a.RegisterScreen(app.NewScreen("/broken", &stubComp{html: "BROKEN"}).
		Fill(outlets.Aside, &boomLoad{}), nil)
	captureSlog(t)

	res, err := a.RenderPartialFromResult(context.Background(), "/broken", "/")
	if err != nil {
		t.Fatalf("the partial must survive a kept-layer fill failure: %v", err)
	}
	var aside string
	for _, f := range res.Fills {
		if f.Addr == "l:shell#aside" {
			aside = string(f.HTML)
		}
	}
	if !strings.Contains(aside, "[HELP]") {
		t.Errorf("kept-layer aside fill must degrade to Default, got %q (fills=%d)", aside, len(res.Fills))
	}
}

// threeOutletShell is the concurrency fixture: three outlets in
// declaration order alpha, beta, gamma.
func threeOutletShell() (*app.Layout, *shellOutlets) {
	o := &shellOutlets{
		alpha: app.NewOutlet("alpha"),
		beta:  app.NewOutlet("beta"),
		gamma: app.NewOutlet("gamma"),
	}
	return app.NewLayout("shell", app.LayoutSpec{
		Outlets: []*app.Outlet{o.alpha, o.beta, o.gamma},
	}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
		return render.Join(
			l.Place(o.alpha),
			l.Primary(),
			l.Place(o.beta),
			l.Place(o.gamma),
		)
	}), o
}

// shellOutlets holds one shell's handles so the tests' fills and the
// build's placements name the same typed values.
type shellOutlets struct {
	alpha, beta, gamma, toolbar, aside *app.Outlet
}

// outletCell extracts one outlet's rendered content from a page by its
// data-cui-outlet address.
func outletCell(t *testing.T, html, addr string) string {
	t.Helper()
	i := strings.Index(html, `data-cui-outlet="`+addr+`"`)
	if i < 0 {
		t.Fatalf("outlet %s missing from %s", addr, html)
	}
	gt := strings.Index(html[i:], ">")
	if gt < 0 {
		t.Fatalf("outlet %s cell unterminated", addr)
	}
	rest := html[i+gt:]
	end := strings.Index(rest, "</div>")
	if end < 0 {
		t.Fatalf("outlet %s cell not closed", addr)
	}
	return rest[:end]
}

// TestFillsLoadConcurrently (DESIGN "Fills load concurrently"): three
// fills whose Loads each sleep 200ms finish in one batch, not in
// series — the render returns well under the 600ms a sequential walk
// would take. The -race gate is the other referee
// (TestP3ConcurrentSharedStateExercise).
func TestFillsLoadConcurrently(t *testing.T) {
	a := app.NewApp("t")
	shell, outlets := threeOutletShell()
	a.SetDefaultLayout(shell)
	a.RegisterScreen(app.NewScreen("/", &stubComp{html: "HOME"}).
		Fill(outlets.alpha, &sleepyFill{label: "A", d: 200 * time.Millisecond}).
		Fill(outlets.beta, &sleepyFill{label: "B", d: 200 * time.Millisecond}).
		Fill(outlets.gamma, &sleepyFill{label: "G", d: 200 * time.Millisecond}), nil)

	start := time.Now()
	res, err := a.RenderPageResult(context.Background(), "/")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	html := string(res.HTML)
	for _, label := range []string{"[A]", "[B]", "[G]", "HOME"} {
		if !strings.Contains(html, label) {
			t.Errorf("missing %s: %s", label, html)
		}
	}
	if elapsed >= 300*time.Millisecond {
		t.Errorf("three 200ms fills took %v; a sequential walk takes 600ms — they must load concurrently", elapsed)
	}
}

// TestP3ConcurrentResolvesEveryOutlet: every outlet resolves and lands
// on its own address, whatever order the goroutines finish in.
func TestP3ConcurrentResolvesEveryOutlet(t *testing.T) {
	a := app.NewApp("t")
	shell, outlets := threeOutletShell()
	a.SetDefaultLayout(shell)
	// Staggered sleeps: gamma (declared last) finishes first, alpha
	// (declared first) last. Declaration order must win regardless.
	a.RegisterScreen(app.NewScreen("/", &stubComp{html: "HOME"}).
		Fill(outlets.alpha, &sleepyFill{label: "A", d: 30 * time.Millisecond}).
		Fill(outlets.beta, &sleepyFill{label: "B", d: 15 * time.Millisecond}).
		Fill(outlets.gamma, &sleepyFill{label: "G"}), nil)

	res, err := a.RenderPageResult(context.Background(), "/")
	if err != nil {
		t.Fatal(err)
	}
	html := string(res.HTML)
	for _, tc := range []struct{ addr, label string }{
		{"l:shell#alpha", "[A]"},
		{"l:shell#beta", "[B]"},
		{"l:shell#gamma", "[G]"},
	} {
		if cell := outletCell(t, html, tc.addr); !strings.Contains(cell, tc.label) {
			t.Errorf("outlet %s = %q, want it to carry %s", tc.addr, cell, tc.label)
		}
	}
}

// TestContainedFillFailuresRecordedInDeclarationOrder: two outlets
// fail with real errors — the slow one declared FIRST. Siblings run to
// completion (nothing cancels), the page renders, and the recorded
// failures (RenderResult.FillFailures) follow declaration order, not
// whichever goroutine lost the race.
func TestContainedFillFailuresRecordedInDeclarationOrder(t *testing.T) {
	a := app.NewApp("t")
	shell, outlets := threeOutletShell()
	a.SetDefaultLayout(shell)
	// alpha (first declared) is slow to fail; beta (second) fails at
	// once. The recorded failures must still lead with alpha's.
	a.RegisterScreen(app.NewScreen("/", &stubComp{html: "HOME"}).
		Fill(outlets.alpha, &sleepyFill{label: "A", d: 25 * time.Millisecond, loadErr: errors.New("alpha-failed")}).
		Fill(outlets.beta, &sleepyFill{label: "B", loadErr: errors.New("beta-failed")}), nil)
	logs := captureSlog(t)

	res, err := a.RenderPageResult(context.Background(), "/")
	if err != nil {
		t.Fatalf("both failures must be contained, not fail the page: %v", err)
	}
	if !strings.Contains(string(res.HTML), "HOME") {
		t.Errorf("page must render under contained failures: %s", res.HTML)
	}
	if len(res.FillFailures) != 2 {
		t.Fatalf("both contained failures must be recorded, got %d: %+v", len(res.FillFailures), res.FillFailures)
	}
	if res.FillFailures[0].Addr != "l:shell#alpha" || res.FillFailures[1].Addr != "l:shell#beta" {
		t.Errorf("failures must be recorded in declaration order, got %+v", res.FillFailures)
	}
	if !strings.Contains(res.FillFailures[0].Err.Error(), "alpha-failed") {
		t.Errorf("first failure must be alpha's, got: %v", res.FillFailures[0].Err)
	}
	if strings.Contains(logs.String(), "context canceled") {
		t.Errorf("a failing sibling must not cancel the rest: %q", logs.String())
	}
}

// TestFillingSiblingRunsToCompletionAfterFailure: a failing outlet
// stops nothing — later-declared outlets still load and render (the
// page needs every outlet's own outcome).
func TestFillingSiblingRunsToCompletionAfterFailure(t *testing.T) {
	a := app.NewApp("t")
	shell, outlets := threeOutletShell()
	a.SetDefaultLayout(shell)
	events := &eventLog{}
	a.RegisterScreen(app.NewScreen("/", &stubComp{html: "HOME"}).
		Fill(outlets.alpha, &sleepyFill{label: "first", loadErr: errors.New("nope")}).
		Fill(outlets.beta, &sleepyFill{label: "after", d: 20 * time.Millisecond, events: events}), nil)
	captureSlog(t)

	res, err := a.RenderPageResult(context.Background(), "/")
	if err != nil {
		t.Fatalf("the failing fill must be contained: %v", err)
	}
	if !events.has("after:done") {
		t.Errorf("a sibling after a failure must run to completion, events=%v", events.snapshot())
	}
	if !strings.Contains(string(res.HTML), "[after]") {
		t.Errorf("the sibling's fill must render: %s", res.HTML)
	}
}

// TestP3RequestCancellationPropagates: the request context reaches
// every fill goroutine, so a cancelled request (client gone) aborts a
// ctx-aware Load instead of running it out.
func TestP3RequestCancellationPropagates(t *testing.T) {
	a := app.NewApp("t")
	shell, outlets := threeOutletShell()
	a.SetDefaultLayout(shell)
	events := &eventLog{}
	a.RegisterScreen(app.NewScreen("/", &stubComp{html: "HOME"}).
		Fill(outlets.alpha, &sleepyFill{label: "slow", d: 300 * time.Millisecond, events: events}), nil)
	captureSlog(t)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	_, _ = a.RenderPageResult(ctx, "/")
	if !events.has("slow:cancelled") {
		t.Errorf("request cancellation must reach the fill Load, events=%v", events.snapshot())
	}
}

// TestP3PanicInResolutionConverted: a panic outside the Load/render
// containment (SetParams) is converted and contained — never an
// escaped panic out of the resolution goroutine.
func TestP3PanicInResolutionConverted(t *testing.T) {
	a := app.NewApp("t")
	shell, outlets := labShell()
	a.SetDefaultLayout(shell)
	a.RegisterScreen(app.NewScreen("/{id}", &dynComp{}).
		Fill(outlets.Aside, &paramPanicFill{}), nil)
	logs := captureSlog(t)

	res, err := a.RenderPageResult(context.Background(), "/7")
	if err != nil {
		t.Fatalf("the panic must be contained to its outlet: %v", err)
	}
	if html := string(res.HTML); !strings.Contains(html, "[HELP]") {
		t.Errorf("aside must degrade to Default: %s", html)
	}
	if !strings.Contains(logs.String(), "fill resolution panicked") && !strings.Contains(logs.String(), "SetParams") {
		t.Errorf("the converted panic must be diagnosable in the log, got: %q", logs.String())
	}
	assertNoHostileEcho(t, string(res.HTML), logs.String())
}

// seedFill seeds a store slice during Load, exercising the shared
// per-request seed bag under concurrency.
type seedFill struct {
	sl    *store.Slice[string]
	label string
}

var seedCount = store.New("fillpolicy").String("count", "0")

func (f *seedFill) Load(ctx context.Context) error {
	f.sl.Seed(ctx, f.label)
	return nil
}

func (f *seedFill) Render() render.HTML { return render.Text("[" + f.label + "]") }

// injectedFill carries an inject-tagged field so concurrent resolution
// exercises the DI container's Inject under its lock.
type injectedFill struct {
	Label string
	Svc   *policySvc `inject:""`
}

type policySvc struct{ N int }

func (f *injectedFill) Load(ctx context.Context) error { return nil }

func (f *injectedFill) Render() render.HTML {
	if f.Svc == nil || f.Svc.N != 42 {
		panic("injectedFill: service not injected")
	}
	return render.Text("[" + f.Label + "]")
}

// TestP3ConcurrentSharedStateExercise hammers the concurrent path with
// fills that Seed the shared per-request store bag and take DI
// injection in parallel. Correctness is asserted by output; the -race
// gate is the real referee (go test ./core-ui/app/... -count=1 -race).
func TestP3ConcurrentSharedStateExercise(t *testing.T) {
	a := app.NewApp("t")
	if err := a.Provide(&policySvc{N: 42}); err != nil {
		t.Fatal(err)
	}
	shell, outlets := threeOutletShell()
	a.SetDefaultLayout(shell)
	a.RegisterScreen(app.NewScreen("/", &stubComp{html: "HOME"}).
		Fill(outlets.alpha, &seedFill{sl: seedCount, label: "SEED-A"}).
		Fill(outlets.beta, &injectedFill{Label: "DI-B"}).
		Fill(outlets.gamma, &seedFill{sl: seedCount, label: "SEED-G"}), nil)

	for range 20 {
		res, err := a.RenderPageResult(context.Background(), "/")
		if err != nil {
			t.Fatal(err)
		}
		html := string(res.HTML)
		for _, want := range []string{"[SEED-A]", "[DI-B]", "[SEED-G]", "HOME"} {
			if !strings.Contains(html, want) {
				t.Fatalf("missing %s under concurrent load: %s", want, html)
			}
		}
	}
}
