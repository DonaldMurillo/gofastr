package telemetryui_test

import (
	"context"
	"fmt"
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

	"github.com/DonaldMurillo/gofastr/battery/telemetry"
	"github.com/DonaldMurillo/gofastr/battery/telemetry/telemetryui"
	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	itel "github.com/DonaldMurillo/gofastr/internal/telemetry"
)

// longApp serves the screens over 60 requests and 60 log lines, one a
// second, numbered so a page's rows can be named, plus 30 access lines
// for the framework's own scripts.
func longApp(t *testing.T) http.Handler {
	t.Helper()
	dir := t.TempDir()
	now := time.Now()
	var traces, logs []byte
	// Oldest first, as a writer leaves a file. Start well inside today
	// so the lot sits in one day file.
	base := now.Add(-10 * time.Minute)
	if base.UTC().YearDay() != now.UTC().YearDay() {
		base = now.Add(-time.Duration(now.UTC().Hour())*time.Hour - time.Duration(now.UTC().Minute())*time.Minute)
	}
	for i := range 60 {
		at := base.Add(time.Duration(i) * time.Second)
		traces = itel.AppendTrace(traces, itel.Trace{Time: at, TraceID: fmt.Sprintf("%032x", i+1), Route: fmt.Sprintf("GET /item-%02d", i), Status: 200, Dur: time.Millisecond})
		logs = append(logs, fmt.Sprintf(`{"time":%q,"level":"INFO","msg":"line-%02d"}`+"\n", at.UTC().Format(time.RFC3339Nano), i)...)
		if i%2 == 0 {
			logs = append(logs, fmt.Sprintf(`{"time":%q,"level":"INFO","msg":"http.access","method":"GET","path":"/__gofastr/runtime/x%02d.js","status":200}`+"\n",
				at.Add(time.Millisecond).UTC().Format(time.RFC3339Nano), i)...)
		}
	}
	for kind, data := range map[string][]byte{itel.KindTraces: traces, itel.KindLogs: logs} {
		if err := os.WriteFile(filepath.Join(dir, itel.FileName(kind, base)), data, 0o600); err != nil {
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
	return app.Router()
}

var (
	numbered  = regexp.MustCompile(`(?:line|item)-(\d\d)`)
	pagerLink = regexp.MustCompile(`<a[^>]*href="([^"]*(?:before|after)=[^"]*)"[^>]*>(?:<[^>]*>)*\s*(Newer|Older)`)
	newestRe  = regexp.MustCompile(`<a[^>]*href="([^"]*)"[^>]*>(?:<[^>]*>)*\s*Back to newest`)
)

// rowsOf returns the numbers of the rows a page lists, in order.
func rowsOf(body string) []string {
	i := strings.Index(body, "<tbody")
	if i < 0 {
		return nil
	}
	body = body[i:]
	var out []string
	for _, m := range numbered.FindAllStringSubmatch(body[:strings.Index(body, "</tbody>")], -1) {
		if len(out) == 0 || out[len(out)-1] != m[1] {
			out = append(out, m[1])
		}
	}
	return out
}

// links returns a page's Newer and Older hrefs; a missing key is a
// disabled button.
func links(body string) map[string]string {
	out := map[string]string{}
	for _, m := range pagerLink.FindAllStringSubmatch(body, -1) {
		out[m[2]] = html.UnescapeString(m[1])
	}
	return out
}

func seq(from, to int) []string {
	var out []string
	for i := from; i >= to; i-- {
		out = append(out, fmt.Sprintf("%02d", i))
	}
	return out
}

// Sixty rows are three pages of 25, 25 and 10, newest first. Older and
// Newer walk them and come back to the same rows; the ends have no link
// past them; and a filter survives the walk.
func TestListsPageOlderAndNewer(t *testing.T) {
	h := longApp(t)
	for _, start := range []string{"/telemetry/logs", "/telemetry/requests", "/telemetry/logs?level=info"} {
		_, p1 := get(t, h, start, true)
		if got := rowsOf(p1); !slices.Equal(got, seq(59, 35)) {
			t.Fatalf("%s page 1 rows: %v", start, got)
		}
		l1 := links(p1)
		if l1["Older"] == "" || l1["Newer"] != "" || newestRe.MatchString(p1) {
			t.Fatalf("%s page 1 links: %v (newest link: %v); want only Older", start, l1, newestRe.MatchString(p1))
		}
		_, p2 := get(t, h, l1["Older"], true)
		if got := rowsOf(p2); !slices.Equal(got, seq(34, 10)) {
			t.Fatalf("%s page 2 rows: %v", start, got)
		}
		l2 := links(p2)
		if l2["Older"] == "" || l2["Newer"] == "" || !newestRe.MatchString(p2) {
			t.Fatalf("%s page 2 links: %v; want Newer, Older and Back to newest", start, l2)
		}
		if strings.Contains(start, "level=info") && (!strings.Contains(l2["Older"], "level=info") || !strings.Contains(l2["Newer"], "level=info")) {
			t.Fatalf("the level filter did not survive paging: %v", l2)
		}
		_, p3 := get(t, h, l2["Older"], true)
		if got := rowsOf(p3); !slices.Equal(got, seq(9, 0)) {
			t.Fatalf("%s page 3 rows: %v", start, got)
		}
		if l3 := links(p3); l3["Older"] != "" || l3["Newer"] == "" {
			t.Fatalf("%s last page links: %v; want only Newer", start, l3)
		}
		// Back up: the same rows as on the way down.
		_, back2 := get(t, h, links(p3)["Newer"], true)
		if got := rowsOf(back2); !slices.Equal(got, seq(34, 10)) {
			t.Fatalf("%s newer from page 3: %v", start, got)
		}
		_, back1 := get(t, h, links(back2)["Newer"], true)
		if got := rowsOf(back1); !slices.Equal(got, seq(59, 35)) {
			t.Fatalf("%s newer from page 2: %v", start, got)
		}
		// At the top again: nothing newer to offer.
		if l := links(back1); l["Newer"] != "" || l["Older"] == "" {
			t.Fatalf("%s back at the top, links: %v", start, l)
		}
		_, top := get(t, h, html.UnescapeString(newestRe.FindStringSubmatch(p3)[1]), true)
		if got := rowsOf(top); !slices.Equal(got, seq(59, 35)) {
			t.Fatalf("%s back to newest: %v", start, got)
		}
	}
	// A URL carrying both cursors is read as the older page it names.
	_, p1 := get(t, h, "/telemetry/logs", true)
	both := links(p1)["Older"] + "&after=2000-01-01T00:00:00Z"
	if _, body := get(t, h, both, true); !slices.Equal(rowsOf(body), seq(34, 10)) {
		t.Fatalf("before and after together: %v", rowsOf(body))
	}
	// A cursor that is not a time is the newest page, not an error.
	if _, body := get(t, h, "/telemetry/logs?before=yesterday&after=never", true); !slices.Equal(rowsOf(body), seq(59, 35)) {
		t.Fatalf("an unreadable cursor: %v", rowsOf(body))
	}
}

// Fetches of the framework's scripts are left out of the log unless
// asked for, and the choice rides along with every link on the page.
func TestLogsLeaveOutQuietPathsByDefault(t *testing.T) {
	h := longApp(t)
	_, body := get(t, h, "/telemetry/logs", true)
	if strings.Contains(body, "/__gofastr/runtime/x") || !strings.Contains(body, "are left out") {
		t.Fatal("the default page lists script fetches, or does not say they are left out")
	}
	// Only the page's own link value turns it on.
	if _, odd := get(t, h, "/telemetry/logs?all=yes", true); strings.Contains(odd, "/__gofastr/runtime/x") || strings.Contains(odd, `value="yes"`) {
		t.Fatal("all=yes was read as the choice, or echoed into the form")
	}
	_, with := get(t, h, "/telemetry/logs?all=1", true)
	if !strings.Contains(with, "GET /__gofastr/runtime/x58.js → 200") || !strings.Contains(with, "are included") {
		t.Fatal("all=1 does not list script fetches")
	}
	if older := links(with)["Older"]; !strings.Contains(older, "all=1") {
		t.Fatalf("paging drops the choice: %s", older)
	}
	if !strings.Contains(with, `name="all" type="hidden" value="1"`) || !strings.Contains(with, `all=1&amp;level=warn`) {
		t.Fatal("the filter form or the level links drop the choice")
	}
}

var pollSrc = regexp.MustCompile(`data-cui-poll="([^"]*)" data-cui-poll-src="([^"]*)"|data-cui-poll-src="([^"]*)" data-cui-poll="([^"]*)"`)

// The newest page of a list re-fetches its rows on a timer, carrying
// its filters; a page someone paged back on does not move under them.
// The rows route serves the table and pager alone, and only to a caller
// the gate admits.
func TestNewestPageRefetchesItsRows(t *testing.T) {
	h := longApp(t)
	for list, filter := range map[string]string{"logs": "level=info", "requests": "show=failed"} {
		_, body := get(t, h, "/telemetry/"+list+"?"+filter, true)
		m := pollSrc.FindStringSubmatch(body)
		if m == nil {
			t.Fatalf("%s: the newest page has no polled region", list)
		}
		every, src := m[1]+m[4], html.UnescapeString(m[2]+m[3])
		if every != "10s" || !strings.HasPrefix(src, "/telemetry/_rows/"+list+"?") || !strings.Contains(src, filter) {
			t.Fatalf("%s: polls %q every %q; want its rows route with %s", list, src, every, filter)
		}
		if !strings.Contains(body, ">Live<") {
			t.Errorf("%s: the page does not say it is live", list)
		}
	}

	_, p1 := get(t, h, "/telemetry/logs", true)
	_, p2 := get(t, h, links(p1)["Older"], true)
	if pollSrc.MatchString(p2) || strings.Contains(p2, ">Live<") {
		t.Error("a page reached by paging still polls: its rows would move under the reader")
	}

	// The rows alone: the same 25 lines, their pager, and no page chrome.
	src := html.UnescapeString(pollSrc.FindStringSubmatch(p1)[2] + pollSrc.FindStringSubmatch(p1)[3])
	code, rows := get(t, h, src, true)
	if code != http.StatusOK || !slices.Equal(rowsOf(rows), seq(59, 35)) || links(rows)["Older"] == "" {
		t.Fatalf("rows route: status %d, rows %v", code, rowsOf(rows))
	}
	for _, chrome := range []string{"<html", "Everything the app logged", `name="q"`, "data-cui-poll"} {
		if strings.Contains(rows, chrome) {
			t.Errorf("the rows route also returned %q", chrome)
		}
	}
	// A list's filters reach its rows.
	if _, filtered := get(t, h, "/telemetry/_rows/logs?q=line-07", true); !slices.Equal(rowsOf(filtered), []string{"07"}) {
		t.Errorf("rows route with q=line-07: %v", rowsOf(filtered))
	}
	if _, none := get(t, h, "/telemetry/_rows/requests?trace="+fmt.Sprintf("%032x", 5), true); none != "" {
		t.Error("the requests rows route drew a table for a single-request URL")
	}
}

func TestRowsRouteRefusesAnUnadmittedCaller(t *testing.T) {
	h := newApp(t) // admits only X-Allow: yes
	for _, url := range []string{"/telemetry/_rows/logs", "/telemetry/_rows/requests"} {
		req := httptest.NewRequest(http.MethodGet, url, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden || strings.Contains(rec.Body.String(), "order") || strings.Contains(rec.Body.String(), "<table") {
			t.Errorf("SECURITY: %s for an unadmitted caller: status %d", url, rec.Code)
		}
		if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
			t.Errorf("%s refusal: Cache-Control %q", url, cc)
		}
		req.Header.Set("X-Allow", "yes")
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<table") || !strings.Contains(rec.Header().Get("Cache-Control"), "no-store") {
			t.Errorf("%s for an admitted caller: status %d, Cache-Control %q", url, rec.Code, rec.Header().Get("Cache-Control"))
		}
	}
}
