package ui

import (
	"strings"
	"testing"
)

func TestSegmentedControlBasic(t *testing.T) {
	out := string(SegmentedControl(SegmentedControlConfig{
		Name:  "view",
		Label: "View mode",
		Options: []SegmentedOption{
			{Label: "Day", Value: "day"},
			{Label: "Week", Value: "week"},
			{Label: "Month", Value: "month"},
		},
	}))
	wants := []string{
		`role="radiogroup"`,
		`aria-label="View mode"`,
		`type="radio"`,
		`name="view"`,
		`value="day"`,
		`value="week"`,
		`value="month"`,
		`checked`,
		`ui-segmented__indicator`,
		`data-position="0"`,
		`data-position="2"`,
		`data-count="3"`, // now on the wrapper, drives equal-width column math
		`for="view--day"`,
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("SegmentedControl missing %q\nout: %s", w, out)
		}
	}
}

// inputTagFor returns the substring spanning a single <input ...> tag
// containing the literal value="<val>", used by tests to assert per-input attrs.
func inputTagFor(html, val string) string {
	for _, seg := range strings.Split(html, "<input") {
		if !strings.Contains(seg, `value="`+val+`"`) {
			continue
		}
		end := strings.Index(seg, ">")
		if end < 0 {
			continue
		}
		return seg[:end]
	}
	return ""
}

func TestSegmentedControlSelected(t *testing.T) {
	out := string(SegmentedControl(SegmentedControlConfig{
		Name:     "view",
		Label:    "View mode",
		Selected: "week",
		Options: []SegmentedOption{
			{Label: "Day", Value: "day"},
			{Label: "Week", Value: "week"},
		},
	}))
	weekTag := inputTagFor(out, "week")
	dayTag := inputTagFor(out, "day")
	if weekTag == "" || dayTag == "" {
		t.Fatalf("missing options, got: %s", out)
	}
	if !strings.Contains(weekTag, "checked") {
		t.Errorf("expected week to be checked, got tag: %s", weekTag)
	}
	if strings.Contains(dayTag, "checked") {
		t.Errorf("did not expect day to be checked, got tag: %s", dayTag)
	}
}

func TestSegmentedControlInvalidSelectedFallsBack(t *testing.T) {
	out := string(SegmentedControl(SegmentedControlConfig{
		Name:     "view",
		Label:    "View mode",
		Selected: "garbage",
		Options: []SegmentedOption{
			{Label: "Day", Value: "day"},
			{Label: "Week", Value: "week"},
		},
	}))
	dayTag := inputTagFor(out, "day")
	weekTag := inputTagFor(out, "week")
	if !strings.Contains(dayTag, "checked") {
		t.Errorf("expected default fallback to Options[0], got tag: %s", dayTag)
	}
	if strings.Contains(weekTag, "checked") {
		t.Errorf("did not expect week to be checked, got tag: %s", weekTag)
	}
}

func TestSegmentedControlRPC(t *testing.T) {
	out := string(SegmentedControl(SegmentedControlConfig{
		Name:      "view",
		Label:     "View mode",
		RPCPath:   "/views/set",
		RPCSignal: "current-view",
		Options: []SegmentedOption{
			{Label: "Day", Value: "day"},
			{Label: "Week", Value: "week"},
		},
	}))
	for _, w := range []string{
		`data-fui-rpc="/views/set"`,
		`data-fui-rpc-method="POST"`,
		`data-fui-rpc-signal="current-view"`,
	} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in: %s", w, out)
		}
	}
}

// TestSegmentedControlRPCCarriesSelectionContract is the Go half of #194:
// when RPCPath is set, EVERY option's <input> must carry name + value +
// data-fui-rpc together. The runtime serializes new FormData(radio.form) for a
// form control (core-ui/runtime/segmented_rpc_e2e_test.go proves that path),
// which only carries the chosen segment if the radio has a name and value to
// submit. A radio missing name/value would post an empty selection even after
// the rpc.js fix: this pins the markup half of the contract.
func TestSegmentedControlRPCCarriesSelectionContract(t *testing.T) {
	out := string(SegmentedControl(SegmentedControlConfig{
		Name:      "view",
		Label:     "View mode",
		RPCPath:   "/views/set",
		RPCSignal: "current-view",
		Options: []SegmentedOption{
			{Label: "Day", Value: "day"},
			{Label: "Week", Value: "week"},
			{Label: "Month", Value: "month"},
		},
	}))
	for _, val := range []string{"day", "week", "month"} {
		tag := inputTagFor(out, val)
		if tag == "" {
			t.Errorf("option value=%q missing its <input> tag", val)
			continue
		}
		for _, want := range []string{
			`name="view"`,               // the form-submit name the handler reads
			`value="` + val + `"`,       // the segment's submit value
			`data-fui-rpc="/views/set"`, // the dispatch trigger
			`data-fui-rpc-signal="current-view"`,
			`data-fui-rpc-method="POST"`,
		} {
			if !strings.Contains(tag, want) {
				t.Errorf("radio value=%q missing %q in tag: %s", val, want, tag)
			}
		}
	}
}

func TestSegmentedControlDisabled(t *testing.T) {
	out := string(SegmentedControl(SegmentedControlConfig{
		Name:  "x",
		Label: "x",
		Options: []SegmentedOption{
			{Label: "A", Value: "a"},
			{Label: "B", Value: "b", Disabled: true},
		},
	}))
	bTag := inputTagFor(out, "b")
	if !strings.Contains(bTag, "disabled") {
		t.Errorf("expected disabled on B option, got tag: %s", bTag)
	}
}

func TestSegmentedControlPanics(t *testing.T) {
	cases := []func(){
		func() {
			SegmentedControl(SegmentedControlConfig{Name: "x", Options: []SegmentedOption{{Label: "A", Value: "a"}}})
		}, // < 2 options
		func() {
			SegmentedControl(SegmentedControlConfig{Options: []SegmentedOption{{Label: "A", Value: "a"}, {Label: "B", Value: "b"}}})
		}, // no Name
		func() {
			SegmentedControl(SegmentedControlConfig{Name: "x", Options: []SegmentedOption{{Label: "", Value: "a"}, {Label: "B", Value: "b"}}})
		}, // empty Label
	}
	for i, fn := range cases {
		t.Run("case-"+itoaSmall(i), func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("expected panic")
				}
			}()
			fn()
		})
	}
}
