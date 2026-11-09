package ui_test

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

func segmentedDays(n int) []ui.SegmentedOption {
	days := []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun", "Hol"}
	opts := make([]ui.SegmentedOption, n)
	for i := range n {
		opts[i] = ui.SegmentedOption{Label: days[i], Value: days[i]}
	}
	return opts
}

// The pill sits on the checked segment for any option count, in both
// directions: with seven options and the seventh checked it used to be
// a sixth of the width too wide and sit on segment 1. Checking another
// segment moves it there.
func TestSegmentedPillOnAnySegment(t *testing.T) {
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	th := theme.Default()
	var css strings.Builder
	for _, e := range registry.All() {
		css.WriteString(e.CSSFor(th))
		css.WriteString("\n")
	}
	head := "<style>" + th.CSSCustomProperties() + "\n" + css.String() + "</style>"
	body := `<div style="width:900px;padding:16px">` +
		string(ui.SegmentedControl(ui.SegmentedControlConfig{Name: "a", ID: "seven", Label: "A", Selected: "Sun", Options: segmentedDays(7)})) +
		`<br>` +
		string(ui.SegmentedControl(ui.SegmentedControlConfig{Name: "b", ID: "eight", Label: "B", Selected: "Wed", Options: segmentedDays(8)})) +
		`<div dir="rtl">` +
		string(ui.SegmentedControl(ui.SegmentedControlConfig{Name: "c", ID: "rtl", Label: "C", Selected: "Fri", Options: segmentedDays(7)})) +
		`</div></div>`
	srv := themeTestPageWithHead(t, head, body)
	ctx := moduleTestCtxURL(t, srv.URL, chromedp.EmulateViewport(1000, 400))
	probe := func(id string) map[string]float64 {
		t.Helper()
		var got map[string]float64
		js := fmt.Sprintf(`(() => {
			const root = document.getElementById(%q);
			const pill = root.querySelector(".fui-segmented__indicator").getBoundingClientRect();
			const opt = root.querySelector(".fui-segmented__input:checked").closest(".fui-segmented__option").getBoundingClientRect();
			return {pl: pill.left, pr: pill.right, pt: pill.top, pb: pill.bottom, ol: opt.left, or: opt.right, ot: opt.top, ob: opt.bottom};
		})()`, id)
		if err := chromedp.Run(ctx, chromedp.Evaluate(js, &got)); err != nil {
			t.Fatalf("probe %s: %v", id, err)
		}
		return got
	}
	onChecked := func(id string) {
		t.Helper()
		g := probe(id)
		if math.Abs(g["pl"]-g["ol"]) > 1 || math.Abs(g["pr"]-g["or"]) > 1 {
			t.Errorf("%s: pill spans [%.1f, %.1f], checked segment spans [%.1f, %.1f]", id, g["pl"], g["pr"], g["ol"], g["or"])
		}
		if math.Abs(g["pt"]-g["ot"]) > 1 || math.Abs(g["pb"]-g["ob"]) > 1 {
			t.Errorf("%s: pill rows [%.1f, %.1f], checked segment rows [%.1f, %.1f]", id, g["pt"], g["pb"], g["ot"], g["ob"])
		}
	}
	for _, id := range []string{"seven", "eight", "rtl"} {
		onChecked(id)
	}
	// It slides: check segment 2 and wait out the transition.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('#seven label[for="a--1"]').click()`, nil),
		chromedp.Sleep(600*time.Millisecond),
	); err != nil {
		t.Fatal(err)
	}
	onChecked("seven")
}

// The pill places itself by anchor positioning, so the sheet holds no
// rule per option count or per position.
func TestSegmentedCSSHasNoPerCountRules(t *testing.T) {
	var css string
	for _, e := range registry.All() {
		if e.Name == "ui-segmented" {
			css = e.CSSFor(theme.Default())
		}
	}
	if css == "" {
		t.Fatal("ui-segmented sheet not registered")
	}
	for _, re := range []string{`\[data-count="\d+"\] \.fui-segmented__indicator`, `data-position="\d+"`, `--fui-segmented-dir`, `translateX`} {
		if regexp.MustCompile(re).MatchString(css) {
			t.Errorf("segmented CSS still matches %s", re)
		}
	}
	for _, want := range []string{"anchor-scope", "anchor-name", "@supports not (anchor-name: --x)", "--ui-segmented-min-width-6"} {
		if !strings.Contains(css, want) {
			t.Errorf("segmented CSS lacks %q", want)
		}
	}
}
