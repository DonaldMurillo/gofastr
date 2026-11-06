package ui

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func chartBoundsPage(t *testing.T, markup string) context.Context {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<!doctype html><html><head><title>Chart bounds</title></head><body>"+markup+"</body></html>")
	}))
	t.Cleanup(server.Close)
	ctx := chromedptest.Context(t, chromedptest.Timeout(45*time.Second))
	if err := chromedp.Run(ctx, chromedp.Navigate(server.URL)); err != nil {
		t.Fatal(err)
	}
	return ctx
}

func TestBarChartExtremeScaleStaysFinite(t *testing.T) {
	var markup strings.Builder
	for _, value := range []float64{1.4e308, 1.5e308, math.MaxFloat64, 1e-308, math.SmallestNonzeroFloat64} {
		for _, fit := range []bool{false, true} {
			markup.WriteString(string(BarChart(BarChartConfig{
				Bars:     []BarChartBar{{Label: "Peak", Value: value}, {Label: "Half", Value: value / 2}},
				ShowAxis: true, FitHeight: fit,
			})))
		}
	}
	ctx := chartBoundsPage(t, markup.String())
	var failures []string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`Array.from(document.querySelectorAll('svg')).flatMap((svg, index) => {
		if (/\b(?:NaN|Infinity|Inf)\b/.test(svg.outerHTML)) return ['chart '+index+' contains a nonfinite value'];
		const bars = svg.querySelectorAll('rect');
		const height = bars[0].height.baseVal.value;
		if (height <= 2) return ['chart '+index+' collapsed its largest bar to '+height];
		if (index % 2 && Math.abs(height - 96) > 0.1) return ['chart '+index+' fitted bar height = '+height];
		if (svg.viewBox.baseVal.height <= 0) return ['chart '+index+' has a nonpositive viewBox'];
		return [];
	})`, &failures)); err != nil {
		t.Fatal(err)
	}
	if len(failures) > 0 {
		t.Fatal(strings.Join(failures, "; "))
	}
}

func TestLineChartTinyHeightKeepsPlot(t *testing.T) {
	var markup strings.Builder
	for _, height := range []int{30, 1, -7} {
		markup.WriteString(string(LineChart(LineChartConfig{
			Height: height, Labels: []string{"First", "Last"}, ShowLegend: true,
			Series: []LineSeries{{Name: "Readings", Values: []float64{2, 9}, Area: true}},
		})))
	}
	ctx := chartBoundsPage(t, markup.String())
	var failures []string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`Array.from(document.querySelectorAll('svg')).flatMap((svg, index) => {
		const height = svg.viewBox.baseVal.height;
		return Array.from(svg.querySelectorAll('line,path,circle,text')).flatMap(node => {
			const box = node.getBBox();
			return box.y < 0 || box.y + box.height > height
				? ['chart '+index+' clips '+node.tagName+' at '+box.y+' within '+height] : [];
		});
	})`, &failures)); err != nil {
		t.Fatal(err)
	}
	if len(failures) > 0 {
		t.Fatal(strings.Join(failures, "; "))
	}
}

func TestLineChartLegendWrapsByRunes(t *testing.T) {
	var markup strings.Builder
	for index, names := range [][]string{
		{"Northern region", "Southern region", "Eastern markets", "Western markets"},
		{strings.Repeat("é", 12), strings.Repeat("à", 12), strings.Repeat("ü", 12)},
	} {
		cfg := LineChartConfig{ShowLegend: true, Height: 30, Width: 360}
		if index == 1 {
			cfg.Width = 220
		}
		for _, name := range names {
			cfg.Series = append(cfg.Series, LineSeries{Name: name, Values: []float64{2, 8}})
		}
		markup.WriteString(string(LineChart(cfg)))
	}
	ctx := chartBoundsPage(t, markup.String())
	var failures []string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`Array.from(document.querySelectorAll('svg')).flatMap((svg, index) => {
		const labels = Array.from(svg.querySelectorAll('.fui-line-chart__legend'));
		const rows = new Set(labels.map(label => label.getAttribute('y')));
		const failures = rows.size < 2 ? ['chart '+index+' did not wrap'] : [];
		if (index === 1 && labels[0].getAttribute('y') !== labels[1].getAttribute('y')) failures.push('UTF-8 labels were measured in bytes');
		for (const label of labels) {
			const box = label.getBBox();
			if (box.x < 0 || box.x + box.width > svg.viewBox.baseVal.width || box.y < 0 || box.y + box.height > svg.viewBox.baseVal.height) failures.push('chart '+index+' clips '+label.textContent);
		}
		return failures;
	})`, &failures)); err != nil {
		t.Fatal(err)
	}
	if len(failures) > 0 {
		t.Fatal(strings.Join(failures, "; "))
	}
}
