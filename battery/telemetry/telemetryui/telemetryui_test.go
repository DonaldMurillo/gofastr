package telemetryui_test

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/DonaldMurillo/gofastr/battery/telemetry"
	"github.com/DonaldMurillo/gofastr/battery/telemetry/telemetryui"
	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	itel "github.com/DonaldMurillo/gofastr/internal/telemetry"
)

const (
	okID     = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa1"
	failedID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa2"
	slowID   = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa3"
	oldID    = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa4"
	evilID   = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa5"
	// A second failure of the first kind, and one of another kind.
	failedAgainID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa6"
	otherFailID   = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa7"
)

// seed writes a small, known set of traces and log lines into dir, all
// stamped relative to now.
func seed(t *testing.T, dir string, now time.Time) {
	t.Helper()
	ms := time.Millisecond
	traces := []itel.Trace{
		{Time: now.Add(-50 * time.Hour), TraceID: oldID, Route: "GET /old", Path: "/old", Status: 200, Dur: 4 * ms},
		{Time: now.Add(-4 * time.Minute), TraceID: okID, Route: "GET /orders/{id}", Path: "/orders/42", Status: 200, Dur: 20 * ms, FailedSteps: 1,
			Spans: []itel.Span{
				{SpanID: "01", Name: "HTTP GET GET /orders/{id}", Dur: 20 * ms},
				{SpanID: "02", ParentID: "01", Name: "SELECT", Offset: 5 * ms, Dur: 10 * ms, Attrs: []itel.Attr{{Key: "db.query.text", Value: "SELECT * FROM orders WHERE id = ?"}}},
				{SpanID: "03", ParentID: "02", Name: "decode", Offset: 6 * ms, Dur: ms, Error: true},
			}},
		{Time: now.Add(-3 * time.Minute), TraceID: failedID, Route: "POST /orders", Path: "/orders", Status: 500, Dur: 8 * ms, Error: true, Err: "card 4411 declined",
			Stack: "app.charge(...)\n\t/app/pay.go:41 +0x2\napp.<b>handler</b>(...)\n\t/app/h.go:9 +0x1\n"},
		{Time: now.Add(-150 * time.Second), TraceID: failedAgainID, Route: "POST /orders", Path: "/orders", Status: 500, Dur: 9 * ms, Error: true, Err: "card 9020 declined"},
		{Time: now.Add(-140 * time.Second), TraceID: otherFailID, Route: "POST /orders", Path: "/orders", Status: 500, Dur: 9 * ms, Error: true, Err: "<b>inventory</b> gone"},
		{Time: now.Add(-2 * time.Minute), TraceID: slowID, Route: "GET /reports", Path: "/reports", Status: 200, Dur: 900 * ms},
		{Time: now.Add(-1 * time.Minute), TraceID: evilID, Route: `GET /<script>alert(1)</script>`, Path: `/"><img src=x>`, Status: 404, Dur: ms},
	}
	logs := []struct {
		at   time.Duration
		line string
	}{
		{-4 * time.Minute, `"level":"INFO","msg":"order.loaded","trace_id":"` + okID + `","span_id":"0000000000000001","order":42,"note":""`},
		{-4*time.Minute + time.Millisecond, `"level":"INFO","msg":"http.access","trace_id":"` + okID + `","span_id":"0000000000000001","method":"GET","path":"/orders/42","status":200,"dur_ms":20`},
		{-3 * time.Minute, `"level":"ERROR","msg":"payment declined","trace_id":"` + failedID + `","span_id":"0000000000000002","err":"card_declined","bytes":10`},
		{-2 * time.Minute, `"level":"WARN","msg":"cache cold","detail":"<b>bold</b>"`},
		{-1 * time.Minute, `"level":"DEBUG","msg":"tick"`},
	}
	write := func(kind string, at time.Time, line []byte) {
		f, err := os.OpenFile(filepath.Join(dir, itel.FileName(kind, at)), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if _, err := f.Write(line); err != nil {
			t.Fatal(err)
		}
	}
	for _, tr := range traces {
		write(itel.KindTraces, tr.Time, itel.AppendTrace(nil, tr))
	}
	for _, l := range logs {
		at := now.Add(l.at)
		write(itel.KindLogs, at, []byte(`{"time":"`+at.UTC().Format(time.RFC3339Nano)+`",`+l.line+"}\n"))
	}
	// The counts of every request, which are more than the traces kept:
	// 40 orders read (one failed), 10 placed (3 failed, 1 slow), one
	// request with markup for a route, and 7 of a route two days ago.
	count := func(at time.Time, route string, n, failed int, dur time.Duration) {
		r := itel.NewRollup()
		for i := range n {
			r.Observe(at, route, dur, i < failed, dur >= 500*ms)
		}
		write(itel.KindStats, at.Add(time.Minute), r.Take(at.Add(time.Minute), true))
	}
	count(now.Add(-50*time.Hour), "GET /old", 7, 0, 4*ms)
	count(now.Add(-4*time.Minute), "GET /orders/{id}", 40, 1, 5*ms)
	count(now.Add(-3*time.Minute), "POST /orders", 9, 3, 8*ms)
	count(now.Add(-3*time.Minute), "POST /orders", 1, 0, 900*ms)
	count(now.Add(-1*time.Minute), `GET /<script>alert(1)</script>`, 1, 0, ms)
	// Background work, which no request total counts: six sends of a
	// queue job (two failed) and one scheduled run.
	work := func(at time.Time, name string, n, failed int, dur time.Duration) {
		r := itel.NewRollup()
		for i := range n {
			r.ObserveJob(at, name, dur, i < failed)
		}
		write(itel.KindStats, at.Add(time.Minute), r.Take(at.Add(time.Minute), true))
	}
	work(now.Add(-3*time.Minute), "process welcome-email", 6, 2, 40*ms)
	work(now.Add(-2*time.Minute), "cron nightly", 1, 0, 3*time.Second)
	// The machine, read twice in the last few minutes and once two
	// days ago, on a disk that is nearly full.
	host := func(at time.Time, rest string) {
		write(itel.KindHost, at, []byte(`{"time":"`+at.UTC().Format(time.RFC3339)+`",`+rest+"}\n"))
	}
	host(now.Add(-50*time.Hour), `"cpu":0.9900,"mem_used":4000000000,"mem_total":4294967296,"disk_used":1073741824,"disk_total":26843545600,"goroutines":5,"heap":1048576,"rss":2097152`)
	host(now.Add(-4*time.Minute), `"cpu":0.1000,"mem_used":1073741824,"mem_total":4294967296,"disk_used":21474836480,"disk_total":26843545600,"goroutines":30,"heap":31457280,"rss":83886080`)
	host(now.Add(-2*time.Minute), `"cpu":0.3700,"mem_used":2254857830,"mem_total":4294967296,"disk_used":24696061952,"disk_total":26843545600,"goroutines":42,"heap":31457280,"rss":83886080`)
}

// newApp serves the screens at /telemetry for a caller who sends
// X-Allow: yes, over a store seeded as above.
func newApp(t *testing.T) http.Handler {
	t.Helper()
	dir := t.TempDir()
	seed(t, dir, time.Now())
	tel := telemetry.New(telemetry.Config{Dir: dir, ExternalTracerProvider: true})
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "test"}))
	app.Mount(uihost.New(appui.NewApp("test")))
	app.RegisterPlugin(tel)
	app.RegisterPlugin(telemetryui.New(tel, telemetryui.Config{Authorize: func(ctx context.Context) bool {
		r := appui.RequestFromContext(ctx)
		return r != nil && r.Header.Get("X-Allow") == "yes"
	}}))
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	t.Cleanup(func() { _ = app.Shutdown(context.Background()) })
	return app.Router()
}

func get(t *testing.T, h http.Handler, url string, allow bool) (int, string) {
	t.Helper()
	// A browser keeps the fragment to itself; so does this.
	url, _, _ = strings.Cut(url, "#")
	req := httptest.NewRequest(http.MethodGet, url, nil)
	if allow {
		req.Header.Set("X-Allow", "yes")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func has(t *testing.T, what, body string, want, not []string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(body, w) {
			t.Errorf("%s: missing %q", what, w)
		}
	}
	for _, n := range not {
		if strings.Contains(body, n) {
			t.Errorf("%s: holds %q", what, n)
		}
	}
}

// The pages hold request paths and log lines: a caller Authorize does
// not admit gets a refusal with none of it, on every page.
func TestScreensRefuseAnUnadmittedCaller(t *testing.T) {
	h := newApp(t)
	for _, url := range []string{"/telemetry/overview", "/telemetry/issues", "/telemetry/requests", "/telemetry/requests?trace=" + okID, "/telemetry/logs"} {
		code, body := get(t, h, url, false)
		if code != http.StatusForbidden {
			t.Errorf("%s: status %d, want 403", url, code)
		}
		has(t, url, body, nil, []string{"/orders", "payment declined", okID, "order.loaded"})
		if code, _ := get(t, h, url, true); code != http.StatusOK {
			t.Errorf("%s admitted: status %d", url, code)
		}
	}
}

func TestRequestsListFilters(t *testing.T) {
	h := newApp(t)
	cases := map[string]struct{ want, not []string }{
		// The default range is a day: the request from two days ago is out.
		"": {[]string{"GET /orders/{id}", "/orders/42", "POST /orders", "GET /reports", ">500<", ">404<", "A step failed", "Slow",
			"trace=" + okID, "trace=" + failedID}, []string{"GET /old"}},
		"?window=7d":            {[]string{"GET /old", "POST /orders"}, nil},
		"?show=failed":          {[]string{"POST /orders"}, []string{"GET /reports", "GET /orders/{id}"}},
		"?show=slow":            {[]string{"GET /reports"}, []string{"POST /orders", "GET /orders/{id}"}},
		"?route=POST+%2Forders": {[]string{"trace=" + failedID, "Route: POST /orders", "Show every route"}, []string{"trace=" + okID, "trace=" + slowID}},
		"?show=failed&window=1h": {[]string{"POST /orders",
			// each range keeps the failed filter, and the chosen one is marked
			`href="/telemetry/requests?show=failed"`, `aria-current="true" class="fui-seglinks__item" data-cui-internal="" href="/telemetry/requests?show=failed&amp;window=1h"`}, nil},
		"?window=nonsense":                  {[]string{"POST /orders"}, []string{"GET /old"}},
		"?show=failed&route=GET+%2Freports": {[]string{"No requests here"}, []string{"trace="}},
	}
	for q, c := range cases {
		code, body := get(t, h, "/telemetry/requests"+q, true)
		if code != http.StatusOK {
			t.Fatalf("%q: status %d", q, code)
		}
		has(t, "requests"+q, body, c.want, c.not)
	}
}

// A route and a path are whatever a caller sent: they reach the page as
// text.
func TestRequestTextIsEscaped(t *testing.T) {
	h := newApp(t)
	for _, url := range []string{"/telemetry/requests", "/telemetry/requests?trace=" + evilID, "/telemetry/logs"} {
		_, body := get(t, h, url, true)
		has(t, url, body, nil, []string{"<script>alert(1)", "<img src=x", "<b>bold</b>"})
	}
	_, body := get(t, h, `/telemetry/requests?route=%22%3E%3Cscript%3Ex%3C%2Fscript%3E`, true)
	has(t, "a route filter", body, nil, []string{"<script>x"})
}

func TestOneRequestShowsItsStepsAndItsLogLines(t *testing.T) {
	h := newApp(t)
	code, body := get(t, h, "/telemetry/requests?trace="+okID, true)
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	has(t, "request", body,
		[]string{"GET /orders/{id}", "/orders/42", "A step failed", okID, "20 ms",
			// the steps, as a tree: the request, its query, the query's step
			`fui-waterfall__step--d0"><span class="fui-waterfall__name">GET /orders/{id}</span>`,
			`fui-waterfall__step--d1"><span class="fui-waterfall__name">SELECT</span><span class="fui-waterfall__detail">SELECT * FROM orders WHERE id = ?</span>`,
			`fui-waterfall__step--d2"><span class="fui-waterfall__name">decode <span class="fui-waterfall__note fui-waterfall__note--danger">Failed</span></span>`,
			`<rect class="fui-waterfall__bar fui-waterfall__bar--danger"`,
			// its own log lines, and only those
			"order.loaded", "order=42", "GET /orders/42 → 200"},
		[]string{"payment declined", "cache cold", "HTTP GET GET", "note="})

	for url, want := range map[string]string{
		"/telemetry/requests?trace=" + strings.Repeat("b", 32): "This request is not in the files",
		"/telemetry/requests?trace=not-an-id":                  "That is not a trace ID",
	} {
		code, body := get(t, h, url, true)
		if code != http.StatusOK || !strings.Contains(body, want) || strings.Contains(body, "fui-waterfall") {
			t.Errorf("%s: status %d, want the message %q and no steps", url, code, want)
		}
	}
	// A route that is already the path gets no second copy of it.
	if _, body := get(t, h, "/telemetry/requests?trace="+failedID, true); strings.Count(body, ">/orders<") != 0 {
		t.Error("POST /orders shows its path again beside the route")
	}
	// An ID is matched whatever its case and spacing: it is pasted.
	if _, body := get(t, h, "/telemetry/requests?trace=+"+strings.ToUpper(okID)+"+", true); !strings.Contains(body, "fui-waterfall") {
		t.Error("an upper-case, padded trace ID did not open its request")
	}
}

func TestLogsFilters(t *testing.T) {
	h := newApp(t)
	cases := map[string]struct{ want, not []string }{
		"": {[]string{"order.loaded", "GET /orders/42 → 200", "payment declined", "err=card_declined bytes=10", "cache cold", ">tick<",
			"trace=" + okID, "trace=" + failedID, ">ERROR<", ">WARN<", ">DEBUG<"}, []string{"http.access", "note="}},
		"?level=warn":         {[]string{"payment declined", "cache cold"}, []string{"order.loaded", ">tick<"}},
		"?level=error":        {[]string{"payment declined"}, []string{"cache cold", "order.loaded"}},
		"?level=info":         {[]string{"order.loaded", "cache cold"}, []string{">tick<"}},
		"?q=card_declined":    {[]string{"payment declined", `value="card_declined"`}, []string{"cache cold", "order.loaded"}},
		"?q=cache&level=warn": {[]string{"cache cold", `name="level" type="hidden" value="warn"`}, []string{"payment declined"}},
		"?q=nothing-matches":  {[]string{"No log lines here"}, []string{"cache cold"}},
	}
	for q, c := range cases {
		code, body := get(t, h, "/telemetry/logs"+q, true)
		if code != http.StatusOK {
			t.Fatalf("%q: status %d", q, code)
		}
		has(t, "logs"+q, body, c.want, c.not)
	}
}

// The bare path lands on the overview: the page that says how things
// are before any list says what happened.
func TestBarePathGoesToTheOverview(t *testing.T) {
	h := newApp(t)
	req := httptest.NewRequest(http.MethodGet, "/telemetry", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/telemetry/overview" {
		t.Fatalf("status %d, Location %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestInitRefusesAnOpenOrHostlessMount(t *testing.T) {
	tel := telemetry.New(telemetry.Config{Dir: t.TempDir(), ExternalTracerProvider: true})
	allow := func(context.Context) bool { return true }
	for name, tc := range map[string]struct {
		want  string
		build func() *framework.App
	}{
		"no Authorize": {"Authorize is required", func() *framework.App {
			app := framework.NewApp()
			app.Mount(uihost.New(appui.NewApp("t")))
			app.RegisterPlugin(telemetryui.New(tel, telemetryui.Config{}))
			return app
		}},
		"no UI host": {"UI host", func() *framework.App {
			app := framework.NewApp()
			app.RegisterPlugin(telemetryui.New(tel, telemetryui.Config{Authorize: allow}))
			return app
		}},
		"relative path": {"must start with /", func() *framework.App {
			app := framework.NewApp()
			app.Mount(uihost.New(appui.NewApp("t")))
			app.RegisterPlugin(telemetryui.New(tel, telemetryui.Config{Authorize: allow, Path: "telemetry"}))
			return app
		}},
		"no telemetry plugin": {"needs the telemetry plugin", func() *framework.App {
			app := framework.NewApp()
			app.Mount(uihost.New(appui.NewApp("t")))
			app.RegisterPlugin(telemetryui.New(nil, telemetryui.Config{Authorize: allow}))
			return app
		}},
	} {
		if err := tc.build().InitPlugins(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: Init returned %v, want an error naming %q", name, err, tc.want)
		}
	}
}

// A screen is served through the UI host's one catch-all, so the host
// tells the tracing middleware which screen route matched: the request
// is listed as "/telemetry/logs", not "unmatched".
func TestScreenRequestsAreTracedByTheirRoute(t *testing.T) {
	prev := otel.GetTracerProvider()
	rec := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	tel := telemetry.New(telemetry.Config{Dir: t.TempDir(), ExternalTracerProvider: true})
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "test"}), framework.WithTracing())
	app.Mount(uihost.New(appui.NewApp("test")))
	app.RegisterPlugin(tel)
	app.RegisterPlugin(telemetryui.New(tel, telemetryui.Config{Authorize: func(context.Context) bool { return true }}))
	if err := app.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Shutdown(context.Background()) })

	if code, _ := get(t, app.Router(), "/telemetry/logs?level=warn", true); code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	spans := rec.Ended()
	if len(spans) == 0 {
		t.Fatal("the request was not traced")
	}
	var route string
	for _, kv := range spans[len(spans)-1].Attributes() {
		if kv.Key == "http.route" {
			route = kv.Value.AsString()
		}
	}
	if route != "/telemetry/logs" {
		t.Fatalf("http.route = %q, want the screen's route", route)
	}
}

var issueLink = regexp.MustCompile(`href="(/telemetry/requests\?[^"]*issue=[0-9a-f]{16}[^"]*)"`)

// Failures that differ only in a number are one issue with a count; a
// different error on the same route is another. Each leads to exactly
// its own requests, and an error's text is text.
func TestIssuesGroupFailuresAndLeadToTheirRequests(t *testing.T) {
	h := newApp(t)
	code, body := get(t, h, "/telemetry/issues", true)
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	has(t, "issues", body,
		[]string{"card N declined", "POST /orders", "inventory", ">500<", "Issues", "What went wrong"},
		[]string{"card 4411", "<b>inventory</b>", "GET /reports", "GET /orders/{id}", "Nothing failed"})
	// Each issue has two links: its name, to every request that failed
	// this way, and "Open", to the newest of them at the top of its page.
	every := issueLink.FindAllStringSubmatch(body, -1)
	var links [][]string
	for _, l := range every {
		if !strings.Contains(l[1], "trace=") {
			links = append(links, l)
		} else if !strings.HasSuffix(l[1], "#top") || !strings.Contains(l[1], "show=failed") {
			t.Errorf("the link to an issue's newest request %q must open at the top and carry its list", l[1])
		}
	}
	if len(every) != 4 {
		t.Fatalf("%d links from 2 issues, want a name and an Open each", len(every))
	}
	if len(links) != 2 {
		t.Fatalf("%d issues link to their requests, want 2", len(links))
	}
	byIssue := map[string][]string{}
	for _, l := range links {
		_, reqs := get(t, h, html.UnescapeString(l[1]), true)
		for _, id := range []string{failedID, failedAgainID, otherFailID, okID, slowID} {
			if strings.Contains(reqs, "trace="+id) {
				byIssue[l[1]] = append(byIssue[l[1]], id)
			}
		}
		if !strings.Contains(reqs, "One issue only") || !strings.Contains(reqs, "Show every request") {
			t.Error("the requests of an issue do not say they are filtered")
		}
	}
	var sizes []int
	for _, ids := range byIssue {
		sizes = append(sizes, len(ids))
	}
	slices.Sort(sizes)
	if !slices.Equal(sizes, []int{1, 2}) {
		t.Fatalf("requests per issue: %v, want one issue of two and one of one", byIssue)
	}
	if code, _ := get(t, h, "/telemetry/issues", false); code != http.StatusForbidden {
		t.Errorf("issues for an unadmitted caller: %d", code)
	}
	// An hour-old window still holds them; a range before them does not
	// exist as a choice, so the empty state shows with a filter that
	// matches nothing: the requests of an issue nobody has.
	_, none := get(t, h, "/telemetry/requests?show=failed&issue="+strings.Repeat("0", 16), true)
	has(t, "an unknown issue", none, []string{"No requests here"}, []string{"trace=" + failedID})
}

// The standalone page starts at the top of the screen. A screen stack
// pins its last child to the bottom for a footer, and this layout has
// one child: a short page (two issues) sat at the foot of the window.
func TestStandalonePageIsNotPinnedToTheBottom(t *testing.T) {
	code, body := get(t, newApp(t), "/telemetry/issues", true)
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	// On an element, not in the style sheet the page carries.
	onElement := func(class string) bool {
		return regexp.MustCompile(`class="[^"]*\b` + regexp.QuoteMeta(class) + `\b`).MatchString(body)
	}
	if onElement("fui-stack--screen") {
		t.Fatal("the page sits in a screen stack, which pushes its only child to the bottom")
	}
	if !onElement("fui-container") {
		t.Fatal("the page lost its container")
	}
}

// The overview counts every request of the range, by route, and leads
// to each route's requests. A range that is not one of its own reads as
// the last 24 hours.
func TestOverviewCountsEveryRequestByRoute(t *testing.T) {
	h := newApp(t)
	code, body := get(t, h, "/telemetry/overview", true)
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	has(t, "the last 24 hours", body, []string{
		"Overview", ">51<", // 40 + 10 + 1
		`href="/telemetry/requests?route=GET+%2Forders%2F%7Bid%7D"`, ">40<", "1 (2.5%)",
		`href="/telemetry/requests?route=POST+%2Forders"`, ">10<", "3 (30%)", "1 (10%)",
		`href="/telemetry/requests?show=failed"`, `href="/telemetry/requests?show=slow"`,
		`id="telemetry-chart-requests-title"`, `aria-labelledby="telemetry-chart-requests-title"`,
		`aria-labelledby="telemetry-chart-failed-title"`, `aria-labelledby="telemetry-chart-p95-title"`,
		"Each bar is 15 minutes.", "&lt;script&gt;alert(1)&lt;/script&gt;",
		`data-cui-poll-src="/telemetry/_rows/overview"`,
	}, []string{"GET /old", "<script>alert(1)</script>", "No requests counted yet"})
	// The busiest route is listed first.
	if strings.Index(body, "GET /orders/{id}") > strings.Index(body, "POST /orders") {
		t.Error("the routes are not busiest first")
	}

	_, week := get(t, h, "/telemetry/overview?window=7d", true)
	has(t, "the last 7 days", week, []string{"GET /old", ">58<", "Each bar is 2 hours.",
		`href="/telemetry/requests?route=GET+%2Fold&amp;window=7d"`},
		// Bars two hours wide do not refresh every ten seconds.
		[]string{"data-cui-poll", "Live"})

	_, hour := get(t, h, "/telemetry/overview?window=1h", true)
	has(t, "the last hour", hour, []string{">51<", "Each bar is one minute.", `data-cui-poll-src="/telemetry/_rows/overview?window=1h"`}, []string{"GET /old"})

	for _, w := range []string{"all", "nonsense", "24h"} {
		_, other := get(t, h, "/telemetry/overview?window="+w, true)
		has(t, "window="+w, other, []string{">51<", "Each bar is 15 minutes."}, []string{"GET /old"})
	}

	// The figures alone, for the timer: no heading, no filter.
	_, part := get(t, h, "/telemetry/_rows/overview", true)
	has(t, "the rows route", part, []string{">51<", "<table"}, []string{"<h1", "Time range"})
}

// With nothing counted the overview says so, and draws no empty charts.
func TestOverviewWithNothingCounted(t *testing.T) {
	tel := telemetry.New(telemetry.Config{Dir: t.TempDir(), ExternalTracerProvider: true})
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "test"}))
	app.Mount(uihost.New(appui.NewApp("test")))
	app.RegisterPlugin(tel)
	app.RegisterPlugin(telemetryui.New(tel, telemetryui.Config{Authorize: func(context.Context) bool { return true }}))
	if err := app.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Shutdown(context.Background()) })
	code, body := get(t, app.Router(), "/telemetry/overview", true)
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	has(t, "an app with no requests yet", body, []string{"No requests counted yet", "Time range"}, []string{"telemetry-chart-requests", "<table"})
}

// A failed request's page says what went wrong in the failure's own
// words, and where when the trace knows. A request that failed without
// a stack has no empty section for one, and one that did not fail has
// neither.
func TestFailedRequestShowsWhatAndWhere(t *testing.T) {
	h := newApp(t)
	_, body := get(t, h, "/telemetry/requests?trace="+failedID, true)
	has(t, "a failure with a stack", body,
		[]string{"What went wrong", "card 4411 declined", "Where it failed", "app.charge(...)", "/app/pay.go:41", "app.&lt;b&gt;handler&lt;/b&gt;(...)"},
		[]string{"app.<b>handler</b>"})
	if strings.Index(body, "What went wrong") > strings.Index(body, "Where it failed") {
		t.Error("where comes before what")
	}

	_, body = get(t, h, "/telemetry/requests?trace="+failedAgainID, true)
	has(t, "a failure with no stack", body, []string{"What went wrong", "card 9020 declined"}, []string{"Where it failed"})

	// A request that succeeded though a step failed has no error of its
	// own to show, and no stack.
	_, body = get(t, h, "/telemetry/requests?trace="+slowID, true)
	has(t, "a request that did not fail", body, []string{"GET /reports"}, []string{"What went wrong", "Where it failed"})

	// A step failed and nothing said why: no empty box in place of the
	// words.
	_, body = get(t, h, "/telemetry/requests?trace="+okID, true)
	has(t, "a failed step with no error text", body, []string{"GET /orders/{id}"}, []string{"What went wrong"})
}

// The overview shows the machine as it was last read: how busy, how
// much memory and disk are used and of how much, and what this app
// holds, with a chart of CPU and of memory over the range. A disk at or
// past ninety percent is drawn as trouble.
func TestOverviewShowsTheMachine(t *testing.T) {
	h := newApp(t)
	_, body := get(t, h, "/telemetry/overview", true)
	has(t, "the machine", body, []string{
		"The machine", "CPU busy", ">37%<", "of all cores",
		"Memory used", ">52%<", "2.1 GB of 4 GB",
		"Disk used", ">92%<", "23 GB of 25 GB",
		"This app", ">80 MB<", "42 goroutines",
		`aria-labelledby="telemetry-chart-cpu-title"`, `aria-labelledby="telemetry-chart-memory-title"`,
	}, []string{"Not known here", ">99%<"})
	// The disk's figure is the one drawn as trouble.
	// Counted on elements: the page's style sheet names the class too.
	if n := len(regexp.MustCompile(`class="[^"]*fui-stat-card__trend--down`).FindAllString(body, -1)); n != 2 {
		t.Errorf("%d figures drawn as trouble; want the failed requests and the disk", n)
	}
	// Both charts are percentages: their scale tops out at 100.
	for _, id := range []string{"telemetry-chart-cpu-title", "telemetry-chart-memory-title"} {
		i := strings.Index(body, `aria-labelledby="`+id+`"`)
		chart := body[i:][:strings.Index(body[i:], "</svg>")]
		if !strings.Contains(chart, ">100</text>") || strings.Contains(chart, ">150</text>") {
			t.Errorf("%s: the scale does not top out at 100", id)
		}
	}
	// The figures alone, for the timer, carry the machine too.
	_, part := get(t, h, "/telemetry/_rows/overview", true)
	has(t, "the rows route", part, []string{"The machine", ">92%<"}, nil)
}

// A machine that says little (no /proc: not Linux) shows what it has and
// a dash for the rest, with no empty chart. An app with no reading yet
// shows no machine at all, and one with readings and no requests shows
// the machine under the note that nothing was counted.
func TestOverviewWithALittleOrNothingFromTheMachine(t *testing.T) {
	serve := func(hostLine string) string {
		dir := t.TempDir()
		if hostLine != "" {
			at := time.Now().Add(-2 * time.Minute)
			line := `{"time":"` + at.UTC().Format(time.RFC3339) + `",` + hostLine + "}\n"
			if err := os.WriteFile(filepath.Join(dir, itel.FileName(itel.KindHost, at)), []byte(line), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		tel := telemetry.New(telemetry.Config{Dir: dir, ExternalTracerProvider: true})
		app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "test"}))
		app.Mount(uihost.New(appui.NewApp("test")))
		app.RegisterPlugin(tel)
		app.RegisterPlugin(telemetryui.New(tel, telemetryui.Config{Authorize: func(context.Context) bool { return true }}))
		if err := app.InitPlugins(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = app.Shutdown(context.Background()) })
		_, body := get(t, app.Router(), "/telemetry/overview", true)
		return body
	}
	has(t, "no reading yet", serve(""), []string{"No requests counted yet"}, []string{"The machine", "Background work"})

	little := serve(`"goroutines":12,"heap":52428800`)
	has(t, "a machine that says little", little,
		[]string{"No requests counted yet", "The machine", "Not known here", ">50 MB<", "12 goroutines"},
		[]string{"telemetry-chart-cpu", "telemetry-chart-memory", "%<"})
	if n := strings.Count(little, "Not known here"); n != 3 {
		t.Errorf("%d figures say they are not known; want CPU, memory and disk", n)
	}
	// A disk with room is a figure like the others, not trouble.
	roomy := serve(`"disk_used":10737418240,"disk_total":26843545600,"goroutines":3,"heap":1048576`)
	has(t, "a disk with room", roomy, []string{">40%<", "10 GB of 25 GB"}, nil)
	if regexp.MustCompile(`class="[^"]*fui-stat-card__trend--down`).MatchString(roomy) {
		t.Error("a disk 40% full is drawn as trouble")
	}
}

// Background work has a table of its own under the routes, with runs
// in place of requests and no "slow", and none of it is in the request
// totals. An app with no background work has no such table.
func TestOverviewListsBackgroundWorkApart(t *testing.T) {
	h := newApp(t)
	_, body := get(t, h, "/telemetry/overview", true)
	has(t, "the overview", body, []string{
		">51<", // the requests, as before
		">Background work</caption>", "process welcome-email", "2 (33%)", "cron nightly", ">3.5 s<",
		`href="/telemetry/requests?route=process+welcome-email"`,
	}, []string{">58<"})
	i := strings.Index(body, ">Background work</caption>")
	table := body[i:][:strings.Index(body[i:], "</table>")]
	has(t, "the background work table", table, []string{">Work<", ">Runs<", ">Failed<", ">Half within<"}, []string{">Slow<", ">Requests<", "GET /orders"})
	if strings.Index(table, "process welcome-email") > strings.Index(table, "cron nightly") {
		t.Error("the work is not busiest first")
	}
	if strings.Index(body, ">Routes</caption>") > i || !strings.Contains(body, ">Routes</caption>") || strings.Index(body, "The machine") < i {
		t.Error("the tables are out of order: routes, background work, the machine")
	}
}

// A process that only works a queue serves no requests: the overview
// still lists its work, under the note that no request was counted.
func TestOverviewOfAWorkerWithNoRequests(t *testing.T) {
	dir := t.TempDir()
	at := time.Now().Add(-3 * time.Minute)
	r := itel.NewRollup()
	for range 5 {
		r.ObserveJob(at, "process resize-image", 80*time.Millisecond, false)
	}
	if err := os.WriteFile(filepath.Join(dir, itel.FileName(itel.KindStats, time.Now())), r.Take(time.Now(), true), 0o600); err != nil {
		t.Fatal(err)
	}
	tel := telemetry.New(telemetry.Config{Dir: dir, ExternalTracerProvider: true})
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "test"}))
	app.Mount(uihost.New(appui.NewApp("test")))
	app.RegisterPlugin(tel)
	app.RegisterPlugin(telemetryui.New(tel, telemetryui.Config{Authorize: func(context.Context) bool { return true }}))
	if err := app.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Shutdown(context.Background()) })
	_, body := get(t, app.Router(), "/telemetry/overview", true)
	has(t, "a worker", body, []string{"No requests counted yet", ">Background work</caption>", "process resize-image", ">5<"}, []string{"Routes, busiest first"})
}
