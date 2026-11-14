package telemetryui

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	itel "github.com/DonaldMurillo/gofastr/internal/telemetry"
)

// A share never rounds a real count to nothing, and gets a decimal
// where the decimal is the news.
func TestShareKeepsSmallCountsVisible(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		part, whole int64
		want        string
	}{
		{0, 0, "0%"}, {0, 500, "0%"}, {5, 0, "0%"},
		{3, 6509, "0.05%"},      // not "0%"
		{1, 5_000_000, "0.01%"}, // the least it says for one
		{67, 23398, "0.3%"}, {57, 2605, "2.2%"}, {99, 1000, "9.9%"},
		{1, 10, "10%"}, {3, 10, "30%"}, {10, 10, "100%"},
	} {
		if got := share(ctx, c.part, c.whole); got != c.want {
			t.Errorf("%d of %d: %q, want %q", c.part, c.whole, got, c.want)
		}
	}
}

// A percentile is shown to two digits, which is as much as the buckets
// it is read from can say, and one at the ceiling says "or more".
func TestPercentilesAreShownAsRoughAsTheyAre(t *testing.T) {
	ms := time.Millisecond
	for in, want := range map[time.Duration]time.Duration{
		0: 0, 7: 7, 99: 99, 100: 100, 104: 100, 105: 110, 995: 1000,
		683500 * time.Microsecond: 680 * ms, 59700 * time.Microsecond: 60 * ms,
		4300 * time.Microsecond: 4300 * time.Microsecond, 10 * time.Second: 10 * time.Second,
	} {
		if got := twoDigits(in); got != want {
			t.Errorf("twoDigits(%v) = %v, want %v", in, got, want)
		}
	}
	ctx := context.Background()
	if got := within(ctx, 683500*time.Microsecond); got != "680 ms" {
		t.Errorf("within(683.5ms) = %q", got)
	}
	if got := within(ctx, itel.StatsCeiling); got != "10 s or more" {
		t.Errorf("at the ceiling: %q", got)
	}
	if got := within(ctx, itel.StatsCeiling-ms); got == "10 s or more" {
		t.Errorf("just under the ceiling reads as the ceiling: %q", got)
	}
}

// Every range's bars are whole minutes and few enough to draw.
func TestSpansFitAChart(t *testing.T) {
	for _, sp := range spans {
		if sp.span == 0 || sp.step < time.Minute || sp.step%time.Minute != 0 {
			t.Errorf("%s: span %v, step %v", sp.key, sp.span, sp.step)
		}
		if bars := sp.span / sp.step; bars < 40 || bars > 120 {
			t.Errorf("%s: %d bars", sp.key, bars)
		}
	}
	if got := pickSpan(url.Values{"window": {"all"}}); got.key != defaultWindow {
		t.Errorf("a range with no width picked %q", got.key)
	}
}

// The table lists the busiest routes and says how many it left out.
// The row that stands for many routes at once leads nowhere: there is
// no one route to list the requests of.
func TestRouteTableStopsAtItsLimit(t *testing.T) {
	s := &Screens{base: "/t"}
	var routes []itel.RouteStats
	for i := range routesShown + 10 {
		routes = append(routes, itel.RouteStats{Route: fmt.Sprintf("GET /r%03d", i), StatsCount: itel.StatsCount{N: int64(1000 - i)}})
	}
	routes[3].Route = itel.OtherRoute
	out := string(s.routeTable(context.Background(), routes, url.Values{}, ""))
	if n := strings.Count(out, "<tr") - 1; n != routesShown {
		t.Fatalf("%d rows, want %d", n, routesShown)
	}
	if !strings.Contains(out, "10 more routes not listed.") {
		t.Error("the table does not say what it left out")
	}
	if !strings.Contains(out, "GET /r049") || strings.Contains(out, "GET /r050") {
		t.Error("the rows are not the first fifty")
	}
	if !strings.Contains(out, itel.OtherRoute) || strings.Contains(out, "route=%28other%29") {
		t.Error("the row for the routes counted together is missing or is a link")
	}
	if all := string(s.routeTable(context.Background(), routes[:routesShown], url.Values{}, "")); strings.Contains(all, "more routes not listed") {
		t.Error("a table that lists every route says it left some out")
	}
}

func TestByteText(t *testing.T) {
	ctx := context.Background()
	for n, want := range map[int64]string{
		0: "0 MB", 1 << 20: "1 MB", 83886080: "80 MB", (1 << 30) - 1: "1,024 MB", 1 << 30: "1 GB",
		2254857830: "2.1 GB", 25 << 30: "25 GB", 252 << 30: "252 GB",
	} {
		if got := byteText(ctx, n); got != want {
			t.Errorf("byteText(%d) = %q, want %q", n, got, want)
		}
	}
}

// A count table sorts by the column asked for, most first unless "asc",
// and falls back to the busiest first for a column it does not have.
// Its header links keep the time range and the other table's sort.
func TestCountTableSorts(t *testing.T) {
	s := &Screens{base: "/t"}
	routes := []itel.RouteStats{
		{Route: "GET /busy", StatsCount: itel.StatsCount{N: 50, Failed: 1, Slow: 0, P95: 10 * time.Millisecond}},
		{Route: "GET /failing", StatsCount: itel.StatsCount{N: 30, Failed: 9, Slow: 2, P95: 40 * time.Millisecond}},
		{Route: "GET /slow", StatsCount: itel.StatsCount{N: 10, Failed: 0, Slow: 7, P95: 2 * time.Second}},
	}
	order := func(out string) string {
		type at struct {
			name string
			i    int
		}
		var seen []at
		for _, n := range []string{"busy", "failing", "slow"} {
			seen = append(seen, at{n, strings.Index(out, ">GET /"+n+"<")})
		}
		slices.SortFunc(seen, func(a, b at) int { return a.i - b.i })
		return seen[0].name + "," + seen[1].name + "," + seen[2].name
	}
	for query, want := range map[string]string{
		"":                     "busy,failing,slow",
		"sort=failed":          "failing,busy,slow",
		"sort=slow":            "slow,failing,busy",
		"sort=p95&dir=asc":     "busy,failing,slow",
		"sort=p95":             "slow,failing,busy",
		"sort=route&dir=asc":   "busy,failing,slow",
		"sort=n&dir=asc":       "slow,failing,busy",
		"sort=nonsense":        "busy,failing,slow",
		"sort=failed&dir=ASC":  "failing,busy,slow",
		"jsort=failed":         "busy,failing,slow", // the other table's sort
		"sort=failed&dir=desc": "failing,busy,slow",
	} {
		q, _ := url.ParseQuery(query)
		if got := order(string(s.routeTable(context.Background(), slices.Clone(routes), q, ""))); got != want {
			t.Errorf("?%s: rows %s, want %s", query, got, want)
		}
	}
	// Jobs has no "slow" column: asked to sort by it, it
	// stays busiest first rather than sorting by a figure it hides.
	q, _ := url.ParseQuery("jsort=slow")
	if got := order(string(s.jobTable(context.Background(), slices.Clone(routes), q, ""))); got != "busy,failing,slow" {
		t.Errorf("the work table sorted by a column it does not show: %s", got)
	}
	q, _ = url.ParseQuery("jsort=failed")
	if got := order(string(s.jobTable(context.Background(), slices.Clone(routes), q, ""))); got != "failing,busy,slow" {
		t.Errorf("the work table ignored its own sort: %s", got)
	}

	q, _ = url.ParseQuery("jsort=failed&jdir=asc&sort=p95")
	out := string(s.routeTable(context.Background(), slices.Clone(routes), q, "7d"))
	for _, want := range []string{"jsort=failed", "jdir=asc", "window=7d", "sort=failed"} {
		if !strings.Contains(out, want) {
			t.Errorf("a header link drops %q:\n%s", want, out[:min(len(out), 1500)])
		}
	}
	// Row ids are plain: a route has spaces and braces an id cannot.
	if strings.Contains(out, `id="GET `) || regexp.MustCompile(`id="[^"]* [^"]*"`).MatchString(out) {
		t.Error("a row or cell id holds a space")
	}
}

// A chart says its highest bar in words; of equal bars the latest; and
// nothing when every bar is empty or unknown.
func TestPeakSaysTheHighestBar(t *testing.T) {
	ctx := context.Background()
	whole := func(ctx context.Context, v float64) string { return i18n.Number(ctx, int(v)) }
	bars := []ui.BarChartBar{{Label: "1:00", Value: 3}, {Label: "1:15", Value: 1200}, {Label: "1:30", Value: math.NaN()}, {Label: "1:45", Value: 1200}, {Label: "2:00", Value: 7}}
	if got := peak(ctx, bars, whole); got != "Peak 1,200 at 1:45 UTC." {
		t.Errorf("peak = %q", got)
	}
	for name, empty := range map[string][]ui.BarChartBar{
		"none": nil, "zeros": {{Label: "a"}, {Label: "b"}}, "unknown": {{Label: "a", Value: math.NaN()}},
	} {
		if got := peak(ctx, empty, whole); got != "" {
			t.Errorf("%s: peak = %q, want nothing", name, got)
		}
	}
	if got := percentOf100(ctx, 37.5); got != "37.5%" && got != "38%" {
		t.Errorf("percentOf100(37.5) = %q", got)
	}
}
