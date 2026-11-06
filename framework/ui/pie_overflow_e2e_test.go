package ui_test

import (
	"math"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/chromedp/chromedp"
)

func TestPieOverflowShowsEmptyState(t *testing.T) {
	for _, values := range [][]float64{{1e308, 1e308}, {math.MaxFloat64, math.MaxFloat64 / 2}, {6e307, 6e307, 6e307}} {
		var slices []ui.PieSlice
		for _, value := range values {
			slices = append(slices, ui.PieSlice{Label: "Share", Value: value})
		}
		markup := string(ui.PieChart(ui.PieChartConfig{Slices: slices, ID: "overflow-pie", LabelledBy: "chart-title", Class: "report-chart", ExtraAttrs: html.Attrs{"data-report": "revenue"}}))
		if !strings.Contains(markup, `data-cui-comp="ui-chart-empty"`) {
			t.Errorf("overflowed sum %v renders a blank chart instead of the empty state", values)
		}
		server := themeTestPageWithHead(t, "", `<h2 id="chart-title">Revenue</h2>`+markup)
		ctx := moduleTestCtxURL(t, server.URL)
		var valid bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => { const chart = document.getElementById('overflow-pie'); return chart.matches('[data-cui-comp="ui-chart-empty"].report-chart') && chart.getAttribute('role') === 'img' && chart.getAttribute('aria-labelledby') === 'chart-title' && chart.dataset.report === 'revenue' && chart.textContent.trim().length > 0 && !chart.querySelector('svg'); })()`, &valid)); err != nil {
			t.Fatal(err)
		}
		if !valid {
			t.Fatal("empty-state content, identity or accessible name was lost")
		}
	}
	markup := string(ui.PieChart(ui.PieChartConfig{Slices: []ui.PieSlice{{Label: "Large", Value: 8e307}, {Label: "Small", Value: 4e307}}}))
	if strings.Contains(markup, `data-cui-comp="ui-chart-empty"`) || !strings.Contains(markup, "<path") {
		t.Fatal("a large finite total must still draw slices")
	}
}
