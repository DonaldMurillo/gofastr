package ui_test

// A 320px screen is where a component's minimum width shows. Each of
// these overflowed there before it was fixed, on the telemetry screens:
// a page header whose subtitle was one long URL path widened the whole
// page; a segmented strip scrolled its last choice (the current one)
// out of sight; a search field pushed its clear button out of its
// frame; a copyable id pushed its copy button out; a wrapping column's
// phone row cut its text to one line; a waterfall step's detail showed
// a few characters.

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

func TestE2E_ComponentsFitAPhone(t *testing.T) {
	long := "/tenants/acme/projects/web/environments/production/deployments/42/artifacts/7"
	// The harness page carries no component styles of its own: without
	// them every check below passes on unstyled boxes.
	var css strings.Builder
	for _, e := range registry.All() {
		css.WriteString(e.CSSFor(theme.Default()))
	}
	// A sans-serif face: the serif default a bare page falls back to is
	// narrower than any face an app ships, and hides a tight gutter.
	body := `<style>body{margin:0;font-family:sans-serif}` + css.String() + `</style><div id="page" style="padding:20px">` +
		string(ui.PageHeader(ui.PageHeaderConfig{Title: "GET " + strings.ReplaceAll(long, "acme", "{tenant}"), Subtitle: long,
			Actions: ui.LinkButton(ui.LinkButtonConfig{Label: "All requests", Href: "/x", Variant: ui.ButtonSecondary})})) +
		string(ui.SegmentedLinks(ui.SegmentedLinksConfig{Label: "Level", Items: []ui.SegmentLink{
			{Text: "All", Href: "?"}, {Text: "Info and up", Href: "?l=i"}, {Text: "Warnings and up", Href: "?l=w"}, {Text: "Errors", Href: "?l=e", Current: true}}})) +
		// A box narrower than an input's own default width, whatever
		// font the test machine has.
		`<div style="inline-size:180px">` + string(ui.FilterToolbar(ui.FilterToolbarConfig{Action: "/x", Label: "Filters", HideReset: true,
		Search: &ui.FilterSearch{Name: "q", Value: "artifact", Placeholder: "Message, request ID or any text"}})) + `</div>` +
		`<div id="idbox">` + string(ui.ShortID(ui.ShortIDConfig{Value: "7e22a54e248aba13f8000d30851799ba", Length: 32})) + `</div>` +
		string(ui.DataTable(ui.DataTableConfig{Caption: "Log", CaptionHidden: true, Responsive: ui.ResponsiveRows,
			Columns: []ui.Column{{Key: "msg", Header: "Message", Phone: ui.PhoneTitle, Wrap: true}, {Key: "t", Header: "Time", Phone: ui.PhoneMeta}},
			Rows: []ui.Row{{ID: "r1", Cells: map[string]render.HTML{
				"msg": render.Text("payment declined by the issuer after three attempts and a timeout on the fourth"), "t": render.Text("1:15 AM")}}}})) +
		`<div id="cards">` + string(ui.DataTable(ui.DataTableConfig{Caption: "Routes", Responsive: ui.ResponsiveCards,
		Columns: []ui.Column{{Key: "route", Header: "Route", Wrap: true}, {Key: "n", Header: "Requests", Align: "end"}},
		Rows: []ui.Row{{ID: "c1", Cells: map[string]render.HTML{
			"route": render.Text("GET " + strings.ReplaceAll(long, "acme", "{tenant}")), "n": render.Text("127")}}}})) + `</div>` +
		`<div id="chart">` + string(ui.BarChart(ui.BarChartConfig{Width: 340, Height: 160, ShowAxis: true, HideValues: true,
		Bars: []ui.BarChartBar{{Label: "a", Value: 1200000}, {Label: "b", Value: 48000000}}})) + `</div>` +
		// A truncated cell holds its code box to its width and leaves
		// every other child as it was.
		`<div id="trunc">` + string(ui.DataTable(ui.DataTableConfig{Caption: "Jobs",
		Columns: []ui.Column{{Key: "err", Header: "Error", Truncate: true}, {Key: "act", Header: "Actions", Truncate: true}},
		Rows: []ui.Row{{ID: "t1", Cells: map[string]render.HTML{
			"err": ui.InlineCode(strings.Repeat("dial tcp 10.0.0.1:5432: connect: connection refused ", 6)),
			"act": render.HTML(`<span hidden id="gone">secret</span>` + string(ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM}, render.HTML(`<b id="in-cluster">a</b>`), render.HTML(`<b>b</b>`))))}}}})) + `</div>` +
		string(ui.Waterfall(ui.WaterfallConfig{Label: "steps", Steps: []ui.WaterfallStep{
			{Label: "SELECT", Detail: "SELECT sum(total) FROM orders WHERE month = 11", Dur: time.Second}}})) +
		`</div>`
	ctx := moduleTestCtx(t, body, chromedp.EmulateViewport(320, 568))
	if !pollJS(ctx, moduleLoaded("searchinput")) {
		t.Fatal("the page's modules never loaded")
	}
	if got := evalString(ctx, `getComputedStyle(document.querySelector('.fui-seglinks')).display`); got != "inline-flex" {
		t.Fatalf("the components are not styled (strip display %q): the checks would pass on anything", got)
	}
	// Each check is 'what is wrong', empty when nothing is.
	for name, js := range map[string]string{
		"the page's content runs past the screen": `(function(){
			var w=[...document.querySelectorAll('#page *')].filter(function(e){var r=e.getBoundingClientRect(); return r.width > 1 && r.right > innerWidth+1 && getComputedStyle(e).position !== 'absolute' && !e.closest('svg,pre,.fui-data-table__scroll');}).map(function(e){return e.tagName+'.'+e.className+' to '+Math.round(e.getBoundingClientRect().right);});
			// A box can stay inside while its text spills out of it.
			for (const e of document.querySelectorAll('.fui-page-header__title, .fui-page-header__subtitle')) if (e.scrollWidth > e.clientWidth + 1) w.push(e.className+' text spills '+(e.scrollWidth-e.clientWidth)+'px');
			return w.slice(0,4).join(', ');})()`,
		"the segmented strip hides a choice": `(function(){var s=document.querySelector('.fui-seglinks'), r=s.getBoundingClientRect(), bad='';
			if (s.scrollWidth > s.clientWidth + 1) bad='scrolls by '+(s.scrollWidth-s.clientWidth)+'px';
			for (const a of s.querySelectorAll('a')) { var b=a.getBoundingClientRect(); if (b.right > r.right + 1) bad += ' '+a.textContent+' ends at '+b.right+' past '+r.right; }
			return bad;})()`,
		"the search field's clear button is outside its frame": `(function(){var f=document.querySelector('.fui-search').getBoundingClientRect(), c=document.querySelector('.fui-search__clear').getBoundingClientRect();
			return c.width > 0 && c.right <= f.right + 0.5 && c.left >= f.left ? '' : 'button '+c.left+'-'+c.right+' frame '+f.left+'-'+f.right;})()`,
		"the id's copy button is outside its box": `(function(){var f=document.getElementById('idbox').getBoundingClientRect(), c=document.querySelector('#idbox button').getBoundingClientRect();
			return c.width > 0 && c.right <= f.right + 0.5 ? '' : 'button ends at '+c.right+', box at '+f.right;})()`,
		"the phone row cuts a wrapping column to one line": `(function(){var td=document.querySelector('td.is-phone-title');
			return td.scrollWidth <= td.clientWidth + 1 && td.getBoundingClientRect().height > 30 ? '' : 'scrollWidth '+td.scrollWidth+' clientWidth '+td.clientWidth+' height '+td.getBoundingClientRect().height;})()`,
		"a long route makes the card list scroll sideways": `(function(){var sc=document.querySelector('#cards .fui-data-table__scroll');
			return sc.scrollWidth <= sc.clientWidth + 1 ? '' : 'scrollWidth '+sc.scrollWidth+' clientWidth '+sc.clientWidth;})()`,
		// The drawing is scaled to its box: what matters is the size the
		// labels come out at, and that the widest still starts inside.
		"the chart's axis numbers are too small or cut": `(function(){var svg=document.querySelector('#chart svg'), s=svg.getBoundingClientRect(), k=s.width/340, bad='';
			for (const t of svg.querySelectorAll('.fui-bar-chart__axis-label')) { var px=parseFloat(getComputedStyle(t).fontSize)*k, r=t.getBoundingClientRect();
				if (px < 10) bad='drawn at '+px.toFixed(1)+'px'; if (r.left < s.left - 0.5) bad += ' "'+t.textContent+'" starts '+(s.left-r.left).toFixed(1)+'px outside'; }
			return k < 1 ? bad : 'the chart was not scaled down (k='+k+'): nothing tested';})()`,
		"a truncated cell mistreats its children": `(function(){var bad='', code=document.querySelector('#trunc td.is-truncate code'), td=code.closest('td');
			if (code.getBoundingClientRect().right > td.getBoundingClientRect().right + 0.5) bad += 'the code box runs past its cell; ';
			if (getComputedStyle(document.getElementById('gone')).display !== 'none') bad += 'a hidden child is drawn; ';
			if (getComputedStyle(document.getElementById('in-cluster').parentElement).display !== 'flex') bad += 'a row of controls is no longer a flex row';
			return bad;})()`,
		"the step's detail is cut": `(function(){var d=document.querySelector('.fui-waterfall__detail');
			return d.scrollWidth <= d.clientWidth + 1 ? '' : 'scrollWidth '+d.scrollWidth+' clientWidth '+d.clientWidth;})()`,
	} {
		if got := evalString(ctx, js); got != "" {
			t.Errorf("%s: %s", name, got)
		}
	}
}
