package telemetryui

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

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
	out := string(s.routeTable(context.Background(), routes, ""))
	if n := strings.Count(out, "<tr") - 1; n != routesShown {
		t.Fatalf("%d rows, want %d", n, routesShown)
	}
	if !strings.Contains(out, "10 quieter routes are not listed.") {
		t.Error("the table does not say what it left out")
	}
	if !strings.Contains(out, "GET /r049") || strings.Contains(out, "GET /r050") {
		t.Error("the rows are not the first fifty")
	}
	if !strings.Contains(out, itel.OtherRoute) || strings.Contains(out, "route=%28other%29") {
		t.Error("the row for the routes counted together is missing or is a link")
	}
	if all := string(s.routeTable(context.Background(), routes[:routesShown], "")); strings.Contains(all, "quieter routes") {
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
