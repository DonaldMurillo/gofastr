package telemetryui_test

import (
	"html"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

var titleRe = regexp.MustCompile(`<title>([^<]*)</title>`)

// Each view of a page has its own title: the browser tab, the history
// list and a screen reader's arrival announcement all read it.
func TestTitlesNameTheView(t *testing.T) {
	h := newApp(t)
	for url, want := range map[string]string{
		"/telemetry/requests":                    "Requests",
		"/telemetry/requests?show=failed":        "Failed requests",
		"/telemetry/requests?show=SLOW":          "Slow requests",
		"/telemetry/requests?show=other":         "Requests",
		"/telemetry/requests?trace=" + okID:      "Request " + okID[:8],
		"/telemetry/requests?trace=ab&show=slow": "Request ab",
		"/telemetry/logs":                        "Logs",
		"/telemetry/logs?q=+card+":               "Logs: card",
		"/telemetry/logs?q=%3Cb%3E":              "Logs: &lt;b&gt;",
		"/telemetry/issues":                      "Issues",
		"/telemetry/overview":                    "Overview",
	} {
		_, body := get(t, h, url, true)
		m := titleRe.FindStringSubmatch(body)
		if m == nil || !strings.HasPrefix(m[1], want+" ") && m[1] != want {
			t.Errorf("%s: title %q, want it to start with %q", url, m, want)
		}
	}
}

// The tabs sit before the main region, so the skip link passes them,
// mark the page being shown, and keep a chosen time range.
func TestTabsSitOutsideMainAndKeepTheRange(t *testing.T) {
	h := newApp(t)
	current := regexp.MustCompile(`<a aria-current="page" class="fui-tab-nav__link"[^>]*>([^<]+)</a>`)
	for url, tab := range map[string]string{
		"/telemetry/overview": "Overview", "/telemetry/requests": "Requests", "/telemetry/issues": "Issues", "/telemetry/logs": "Logs",
		"/telemetry/requests?trace=" + okID: "Requests",
	} {
		_, body := get(t, h, url, true)
		nav, main := strings.Index(body, `class="fui-tab-nav"`), strings.Index(body, "<main")
		if nav < 0 || main < 0 || nav > main {
			t.Fatalf("%s: tabs at %d, main at %d; the tabs must come before main", url, nav, main)
		}
		if n := strings.Count(body, `class="fui-tab-nav"`); n != 1 {
			t.Errorf("%s: %d tab bars", url, n)
		}
		if m := current.FindAllStringSubmatch(body, -1); len(m) != 1 || m[0][1] != tab {
			t.Errorf("%s: current tab %v, want %s", url, m, tab)
		}
		if strings.Contains(body, `href="/telemetry/logs?`) && !strings.Contains(url, "logs") {
			t.Errorf("%s: a tab carries a query with no range chosen", url)
		}
	}
	_, week := get(t, h, "/telemetry/overview?window=7D", true)
	for _, to := range []string{"overview", "requests", "issues", "logs"} {
		if !strings.Contains(week, `class="fui-tab-nav__link" data-cui-internal="" href="/telemetry/`+to+`?window=7d"`) {
			t.Errorf("the %s tab drops the 7-day range", to)
		}
	}
	// The default range and a made-up one are not worth a query.
	for _, w := range []string{"24h", "nonsense"} {
		_, body := get(t, h, "/telemetry/issues?window="+w, true)
		if !strings.Contains(body, `href="/telemetry/logs"`) {
			t.Errorf("window=%s: the tabs carry it", w)
		}
	}
}

var rowLink = regexp.MustCompile(`href="(/telemetry/requests\?[^"]*trace=` + failedID + `[^"]*)"`)
var backLink = regexp.MustCompile(`href="([^"]*)"[^>]*>(?:<[^>]*>)*(Back to the list|All requests)<`)

// A request opened from a filtered list opens at the top of its page
// and leads back to that list; one opened by its ID alone leads to all
// requests.
func TestOpeningARequestKeepsTheWayBack(t *testing.T) {
	h := newApp(t)
	_, list := get(t, h, "/telemetry/requests?show=failed&route=POST+%2Forders&window=7d", true)
	m := rowLink.FindStringSubmatch(list)
	if m == nil {
		t.Fatal("the failed request is not linked from its list")
	}
	open := html.UnescapeString(m[1])
	for _, want := range []string{"show=failed", "route=POST+%2Forders", "window=7d"} {
		if !strings.Contains(open, want) {
			t.Errorf("the row's link %q drops %s", open, want)
		}
	}
	if !strings.HasSuffix(open, "#top") {
		t.Errorf("the row's link %q would open the request scrolled to where its row was", open)
	}
	_, page := get(t, h, open, true)
	b := backLink.FindStringSubmatch(page)
	if b == nil || b[2] != "Back to the list" || html.UnescapeString(b[1]) != "/telemetry/requests?route=POST+%2Forders&show=failed&window=7d" {
		t.Errorf("back link %v, want the list it was opened from", b)
	}
	// Older and Newer start the next page at its top too.
	if strings.Contains(list, "before=") && !strings.Contains(list, "#top") {
		t.Error("a pager link keeps the scroll position at the foot of the list")
	}

	for _, plain := range []string{"/telemetry/requests?trace=" + failedID, "/telemetry/requests?show=zzz&window=24h&trace=" + failedID, "/telemetry/requests?window=bogus&trace=" + failedID} {
		_, page = get(t, h, plain, true)
		b = backLink.FindStringSubmatch(page)
		if b == nil || b[2] != "All requests" || b[1] != "/telemetry/requests" {
			t.Errorf("%s: back link %v, want all requests", plain, b)
		}
	}
}

// The page that did not find a request offers the box to try another
// ID, holding the one that missed.
func TestMissingRequestOffersTheBox(t *testing.T) {
	h := newApp(t)
	for _, id := range []string{"nothex", strings.Repeat("0", 32)} {
		_, body := get(t, h, "/telemetry/requests?trace="+id, true)
		has(t, "trace="+id, body, []string{"Request not found", `name="trace"`, `value="` + id + `"`}, nil)
	}
}

// A query value reaches the page as text that can be shown and sent
// again: no control characters, valid UTF-8, cut on a character. A
// choice among fixed values does not mind its letter case.
func TestQueryValuesAreCleaned(t *testing.T) {
	h := newApp(t)
	for _, url := range []string{
		"/telemetry/logs?q=" + strings.Repeat("%E6%97%A5", 300),
		"/telemetry/requests?route=" + strings.Repeat("%E6%97%A5", 300),
		"/telemetry/requests?route=a%00b%07c%1b%5B31m",
		"/telemetry/requests?issue=%ff%fe",
		"/telemetry/logs?q=%ff%fex%00",
	} {
		_, body := get(t, h, url, true)
		if !utf8.ValidString(body) {
			t.Errorf("%s: the page is not valid UTF-8", url[:40])
		}
		if strings.ContainsAny(body, "\x00\x07\x1b") {
			t.Errorf("%s: a control character reached the page", url[:40])
		}
	}
	_, body := get(t, h, "/telemetry/logs?q="+strings.Repeat("%E6%97%A5", 300), true)
	if !strings.Contains(body, `value="`+strings.Repeat("日", 85)+`"`) {
		t.Error("a long search is not cut to 255 bytes of whole characters")
	}
	_, body = get(t, h, "/telemetry/requests?route=a%00b%07c%1b%5B31m", true)
	has(t, "a route with control characters", body, []string{"Route: abc[31m"}, nil)

	_, failed := get(t, h, "/telemetry/requests?show=+FAILED", true)
	has(t, "show=FAILED", failed, []string{"POST /orders"}, []string{"GET /reports"})
	_, week := get(t, h, "/telemetry/requests?window=7D", true)
	has(t, "window=7D", week, []string{"GET /old"}, nil)
	_, errs := get(t, h, "/telemetry/logs?level=Error", true)
	has(t, "level=Error", errs, []string{"payment declined"}, []string{"cache cold"})
}

// The search box finds what the page shows: a request line the page
// writes as "GET /orders/42 → 200", an attribute it writes as
// key=value, in any letter case, every word required.
func TestLogSearchReadsWhatThePageShows(t *testing.T) {
	h := newApp(t)
	for q, c := range map[string]struct{ want, not []string }{
		"?q=GET+%2Forders%2F42+%E2%86%92+200": {[]string{"GET /orders/42 → 200"}, []string{"No log lines here", "payment declined"}},
		"?q=PAYMENT+Declined":                 {[]string{"payment declined"}, []string{"cache cold"}},
		"?q=err%3Dcard_declined":              {[]string{"payment declined"}, []string{"cache cold"}},
		"?q=declined+cache":                   {[]string{"No log lines here"}, []string{"payment declined", "cache cold"}},
		"?q=%22declined+payment%22":           {[]string{"No log lines here"}, nil},
		"?q=%22payment+declined%22":           {[]string{"payment declined"}, nil},
	} {
		_, body := get(t, h, "/telemetry/logs"+q, true)
		has(t, "logs"+q, body, c.want, c.not)
	}
	_, body := get(t, h, "/telemetry/logs", true)
	has(t, "the search box", body, []string{`placeholder="Search logs"`}, nil)
}

// The bare path keeps a query on its way to the overview.
func TestBarePathKeepsItsQuery(t *testing.T) {
	h := newApp(t)
	for from, to := range map[string]string{
		"/telemetry?window=7d":       "/telemetry/overview?window=7d",
		"/telemetry?a=%0d%0aX&b=1+2": "/telemetry/overview?a=%0D%0AX&b=1+2",
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, from, nil))
		if got := rec.Header().Get("Location"); rec.Code != http.StatusSeeOther || got != to {
			t.Errorf("%s: status %d, Location %q, want %q", from, rec.Code, got, to)
		}
	}
}

// A list narrowed to a route or an issue that finds nothing says so,
// rather than that traffic is awaited; and each list's empty heading
// sits one level under the page title.
func TestNarrowedEmptyListSaysWhy(t *testing.T) {
	h := newApp(t)
	for _, url := range []string{"/telemetry/requests?route=GET+%2Fnope", "/telemetry/requests?issue=0123456789abcdef"} {
		_, body := get(t, h, url, true)
		has(t, url, body, []string{"Nothing kept for this route or issue", "<h2"}, []string{"Try a wider time range"})
	}
}

// The page says in words what colour alone would: a failed step, a
// stack named for what it is, notices that do not interrupt.
func TestRequestPageSaysItInWords(t *testing.T) {
	h := newApp(t)
	_, body := get(t, h, "/telemetry/requests?trace="+okID, true)
	has(t, "a request with a failed step", body,
		[]string{`fui-waterfall__note--danger">Failed</span>`, "Requests to this route", `href="/telemetry/requests?route=GET+%2Forders%2F%7Bid%7D"`}, nil)
	if n := strings.Count(body, `class="fui-waterfall__note`); n != 1 {
		t.Errorf("%d steps are marked failed, want the one that failed", n)
	}
	if strings.Contains(body, `role="alert"`) {
		t.Error("a request's page interrupts a screen reader with what it shows from the start")
	}
}

// A request opened from an older page of the list leads back to that
// page, and one found by its ID from a filtered list leads back to
// that list.
func TestBackGoesToThePageItCameFrom(t *testing.T) {
	h := longApp(t)
	_, p1 := get(t, h, "/telemetry/requests", true)
	older := html.UnescapeString(links(p1)["Older"])
	if !strings.Contains(older, "before=") {
		t.Fatalf("setup: no older page (%q)", older)
	}
	_, p2 := get(t, h, older, true)
	row := regexp.MustCompile(`href="(/telemetry/requests\?[^"]*trace=[0-9a-f]{32}[^"]*)"`).FindStringSubmatch(p2)
	if row == nil {
		t.Fatal("setup: page 2 links no request")
	}
	open := html.UnescapeString(row[1])
	before := regexp.MustCompile(`before=[^&#]+`).FindString(older)
	if !strings.Contains(open, before) {
		t.Fatalf("a row on an older page links %q, without the page it is on (%s)", open, before)
	}
	_, page := get(t, h, open, true)
	b := backLink.FindStringSubmatch(page)
	if b == nil || !strings.Contains(html.UnescapeString(b[1]), before) || b[2] != "Back to the list" {
		t.Errorf("back link %v, want the older page (%s)", b, before)
	}
	// The first page has no cursor to carry, and a made-up one is not
	// carried at all.
	first := regexp.MustCompile(`href="(/telemetry/requests\?[^"]*trace=[0-9a-f]{32}[^"]*)"`).FindStringSubmatch(p1)
	if first == nil || strings.Contains(first[1], "before=") || strings.Contains(first[1], "after=") {
		t.Errorf("a row on the newest page carries a cursor: %v", first)
	}
	_, odd := get(t, h, "/telemetry/requests?before=%3Cscript%3E&after=yesterday&trace="+strings.Repeat("0", 31)+"1", true)
	if b := backLink.FindStringSubmatch(odd); b == nil || b[1] != "/telemetry/requests" {
		t.Errorf("a made-up cursor reached the back link: %v", b)
	}

	// The trace box sends the list's filters with the ID.
	app := newApp(t)
	_, list := get(t, app, "/telemetry/requests?show=failed&route=POST+%2Forders&window=7d", true)
	for _, want := range []string{`name="show" type="hidden" value="failed"`, `name="route" type="hidden" value="POST /orders"`, `name="window" type="hidden" value="7d"`} {
		if !strings.Contains(list, want) {
			t.Errorf("the trace box does not carry %s", want)
		}
	}
	_, plain := get(t, app, "/telemetry/requests", true)
	if strings.Contains(plain, `type="hidden"`) {
		t.Error("the trace box of an unfiltered list carries empty fields")
	}
}

// A list refreshes itself only when that is a short read: the newest
// page of a day or less with nothing to search for. A week, everything
// kept, one route, one issue or a text search are each the whole range
// read again every ten seconds, and are loaded once.
func TestOnlyAShortReadRefreshesItself(t *testing.T) {
	h := newApp(t)
	for url, live := range map[string]bool{
		"/telemetry/requests":                        true,
		"/telemetry/requests?window=1h&show=failed":  true,
		"/telemetry/requests?window=24h":             true,
		"/telemetry/requests?window=7d":              false,
		"/telemetry/requests?window=all":             false,
		"/telemetry/requests?route=POST+%2Forders":   false,
		"/telemetry/requests?issue=0123456789abcdef": false,
		"/telemetry/logs":                            true,
		"/telemetry/logs?level=error&window=1h":      true,
		"/telemetry/logs?q=card":                     false,
		"/telemetry/logs?window=7d":                  false,
		"/telemetry/logs?window=all":                 false,
	} {
		_, body := get(t, h, url, true)
		polls, says := strings.Contains(body, "data-cui-poll-src="), strings.Contains(body, ">Live<")
		if polls != live || says != live {
			t.Errorf("%s: refreshes itself %v, says Live %v; want both %v", url, polls, says, live)
		}
	}
}
