package ui_test

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	ui "github.com/DonaldMurillo/gofastr/framework/ui"
)

var waterfallBar = regexp.MustCompile(`<rect class="fui-waterfall__bar([^"]*)" x="([^"]*)" y="0" width="([^"]*)" height="10"/>`)

func waterfallSteps() []ui.WaterfallStep {
	return []ui.WaterfallStep{
		{Label: "GET /orders/{id}", Dur: 20 * time.Millisecond},
		{Label: "SELECT", Detail: "SELECT id FROM orders", Start: 5 * time.Millisecond, Dur: 10 * time.Millisecond, Depth: 1},
		{Label: "POST api.example.com", Start: 15 * time.Millisecond, Dur: 5 * time.Millisecond, Depth: 2, Tone: ui.WaterfallDanger},
	}
}

// TestWaterfallIsATableOfSteps: the numbers a sighted reader takes from
// the bars are in cells: a caption, column headers, a row header per
// step, and when it started and how long it took as text.
func TestWaterfallIsATableOfSteps(t *testing.T) {
	h := string(ui.Waterfall(ui.WaterfallConfig{Label: "Request steps", Steps: waterfallSteps()}))
	for _, want := range []string{
		`<caption class="fui-waterfall__caption">Request steps</caption>`,
		`<th scope="col">Step</th>`, `>Starts at</th>`, `>Took</th>`, `>Timeline</span></th>`,
		`<th scope="row" class="fui-waterfall__step fui-waterfall__step--d0"><span class="fui-waterfall__name">GET /orders/{id}</span></th>`,
		`fui-waterfall__step--d1"><span class="fui-waterfall__name">SELECT</span><span class="fui-waterfall__detail">SELECT id FROM orders</span>`,
		`<td class="fui-waterfall__num fui-waterfall__start">5 ms</td><td class="fui-waterfall__num">10 ms</td>`,
		`<td class="fui-waterfall__num fui-waterfall__start">15 ms</td><td class="fui-waterfall__num">5 ms</td>`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %s in\n%s", want, h)
		}
	}
	if n := strings.Count(h, `<svg aria-hidden="true"`); n != 3 {
		t.Errorf("%d decorative bars, want one per step (3)", n)
	}
	if strings.Contains(h, "style=") {
		t.Errorf("an inline style was written: %s", h)
	}
}

// TestWaterfallBarsSitOnTheSharedAxis: a bar starts and ends where its
// step did, as a share of the whole, and takes its tone's class.
func TestWaterfallBarsSitOnTheSharedAxis(t *testing.T) {
	bars := waterfallBar.FindAllStringSubmatch(string(ui.Waterfall(ui.WaterfallConfig{Label: "x", Steps: waterfallSteps()})), -1)
	want := [][3]string{{"", "0", "1000"}, {"", "250", "500"}, {" fui-waterfall__bar--danger", "750", "250"}}
	if len(bars) != len(want) {
		t.Fatalf("%d bars, want %d", len(bars), len(want))
	}
	for i, w := range want {
		if got := [3]string{bars[i][1], bars[i][2], bars[i][3]}; got != w {
			t.Errorf("bar %d: tone/x/width = %q, want %q", i, got, w)
		}
	}

	// An explicit Total widens the axis: the same steps take half of it.
	bars = waterfallBar.FindAllStringSubmatch(string(ui.Waterfall(ui.WaterfallConfig{Label: "x", Total: 40 * time.Millisecond, Steps: waterfallSteps()})), -1)
	if bars[1][2] != "125" || bars[1][3] != "250" {
		t.Errorf("with Total 40ms the second bar is x=%s width=%s, want 125 and 250", bars[1][2], bars[1][3])
	}
}

// TestWaterfallKeepsEveryBarInsideTheAxis: a step too short to see
// still draws, a step past Total stops at the edge, and times that
// make no sense (negative, all zero) draw at zero instead of dividing
// by nothing.
func TestWaterfallKeepsEveryBarInsideTheAxis(t *testing.T) {
	cases := map[string][]ui.WaterfallStep{
		"tiny":     {{Label: "whole", Dur: time.Second}, {Label: "blip", Start: 999*time.Millisecond + 999*time.Microsecond, Dur: time.Microsecond}},
		"past":     {{Label: "late", Start: 2 * time.Second, Dur: time.Second}},
		"negative": {{Label: "odd", Start: -time.Second, Dur: -time.Second}},
		"all zero": {{Label: "a"}, {Label: "b"}},
		"deep":     {{Label: "deep", Dur: time.Second, Depth: 40}, {Label: "under", Dur: time.Second, Depth: -3}},
	}
	for name, steps := range cases {
		total := time.Duration(0)
		if name == "past" {
			total = time.Second
		}
		h := string(ui.Waterfall(ui.WaterfallConfig{Label: "x", Total: total, Steps: steps}))
		bars := waterfallBar.FindAllStringSubmatch(h, -1)
		if len(bars) != len(steps) {
			t.Fatalf("%s: %d bars for %d steps in %s", name, len(bars), len(steps), h)
		}
		for i, b := range bars {
			x, w := atof(t, b[2]), atof(t, b[3])
			if x < 0 || w < 4 || x+w > 1000.001 {
				t.Errorf("%s: bar %d at x=%v width=%v leaves the 0..1000 axis or is too thin to see", name, i, x, w)
			}
		}
		if strings.Contains(h, "NaN") || strings.Contains(h, "Inf") {
			t.Errorf("%s: a non-finite number was drawn: %s", name, h)
		}
	}
	h := string(ui.Waterfall(ui.WaterfallConfig{Label: "x", Steps: cases["deep"]}))
	if !strings.Contains(h, "fui-waterfall__step--d6") || !strings.Contains(h, "fui-waterfall__step--d0") || strings.Contains(h, "--d40") || strings.Contains(h, "--d-3") {
		t.Errorf("depth was not held to 0..6: %s", h)
	}
}

func atof(t *testing.T, s string) float64 {
	t.Helper()
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		t.Fatalf("not a number: %q", s)
	}
	return f
}

// TestWaterfallEmptyAndRefusals: no steps is the shared empty state,
// and a config that would draw an unlabeled table panics at the call.
func TestWaterfallEmptyAndRefusals(t *testing.T) {
	h := string(ui.Waterfall(ui.WaterfallConfig{Label: "Request steps"}))
	if !strings.Contains(h, "No steps recorded") || strings.Contains(h, "<table") {
		t.Errorf("empty waterfall: %s", h)
	}
	for name, cfg := range map[string]ui.WaterfallConfig{
		"no label":      {Steps: waterfallSteps()},
		"no step label": {Label: "x", Steps: []ui.WaterfallStep{{Dur: time.Second}}},
		"unknown tone":  {Label: "x", Steps: []ui.WaterfallStep{{Label: "a", Tone: "pink"}}},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: no panic", name)
				}
			}()
			ui.Waterfall(cfg)
		}()
	}
}

// TestWaterfallEscapesText: a step name is data, a span name an
// attacker can shape through a request path.
func TestWaterfallEscapesText(t *testing.T) {
	h := string(ui.Waterfall(ui.WaterfallConfig{Label: `a"<b>`, Steps: []ui.WaterfallStep{{Label: `<script>x</script>`, Detail: `"><img src=x>`, Dur: time.Second}}}))
	if strings.Contains(h, "<script>") || strings.Contains(h, "<img") || strings.Contains(h, "<b>") {
		t.Errorf("markup in a label reached the page: %s", h)
	}
}

func TestDurationText(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0:                       "0 µs",
		412 * time.Microsecond:  "412 µs",
		3100 * time.Microsecond: "3.1 ms",
		999 * time.Millisecond:  "999 ms",
		time.Second:             "1 s",
		1240 * time.Millisecond: "1.24 s",
		90 * time.Second:        "90 s",
		-time.Second:            "0 µs",
	} {
		if got := ui.DurationText(context.Background(), d); got != want {
			t.Errorf("DurationText(%v) = %q, want %q", d, got, want)
		}
	}
}
