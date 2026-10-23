package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// A chart's ticks, values and point titles are text a reader reads, so
// they follow the request's locale like any other number on the page.
func TestChartsFollowLocale(t *testing.T) {
	fr := i18n.WithContext(context.Background(), i18n.Locale{Tag: "fr"})
	for name, tc := range map[string]struct {
		got  string
		want []string
	}{
		"bar": {string(BarChart(BarChartConfig{Ctx: fr, ShowAxis: true,
			Bars: []BarChartBar{{Label: "A", Value: 1234.5}}})),
			[]string{">1 234,5</text>", "A: 1 234,5</title>"}},
		"gauge": {string(Gauge(GaugeConfig{Ctx: fr, Label: "Uptime", Value: 73.25})),
			[]string{">73,25</text>", `aria-valuetext="73,25"`}},
		"scatter": {string(ScatterChart(ScatterChartConfig{Ctx: fr,
			Series: []ScatterSeries{{Name: "S", Points: []ScatterPoint{{X: 1.5, Y: 2}, {X: 3.25, Y: 4}}}}})),
			[]string{"S: (1,5, 2)</title>", "S: (3,25, 4)</title>"}},
		"heatmap": {string(Heatmap(HeatmapConfig{Ctx: fr, Rows: []string{"r"}, Cols: []string{"c", "d"},
			Values: [][]float64{{0.5, 2}}})),
			[]string{"r, c: 0,5</title>"}},
	} {
		for _, w := range tc.want {
			if !strings.Contains(tc.got, w) {
				t.Errorf("%s: want %q in\n%s", name, w, tc.got)
			}
		}
	}
}

// Past a month Ago writes the date the reader's way, and a rating's
// accessible name writes its digits the reader's way.
func TestAgoAndRatingFollowLocale(t *testing.T) {
	fr := i18n.WithContext(context.Background(), i18n.Locale{Tag: "fr"})
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	if got := Ago(fr, now, time.Date(2026, 3, 5, 9, 0, 0, 0, time.UTC)); got != "5 mars 2026" {
		t.Errorf("Ago past a month in fr = %q, want 5 mars 2026", got)
	}
	ar := i18n.WithContext(context.Background(), i18n.Locale{Tag: "ar-EG"})
	if got := string(Rating(RatingDisplayConfig{Ctx: ar, Value: 4})); !strings.Contains(got, "٤") {
		t.Errorf("rating in ar-EG has no Arabic-Indic digit:\n%s", got)
	}
}
