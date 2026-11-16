package headless

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
)

func TestSweepPickersRangeSliderZeroLowIsHonoured(t *testing.T) {
	got := RangeSlider(RangeSliderProps{Name: "t", Label: "Temp", Min: -10, Max: 10,
		ValueLow: new(0), ValueHigh: new(5)}, nil)
	has(t, got, `name="t-min" step="1" type="range" value="0"`, "a set low of zero widened to Min")
}

func TestSweepPickersComboboxPickRefusals(t *testing.T) {
	isl := &Island{Endpoint: "/i", Signal: "s"}
	refuse(t, "Pick requires Island", func() {
		Combobox(ComboboxProps{ID: "c", Name: "q", Label: "C", Pick: &ComboboxPick{Name: "c_id"},
			Options: []ComboboxOption{{Label: "a"}}}, nil)
	})
	refuse(t, "Pick requires Name", func() {
		Combobox(ComboboxProps{ID: "c", Name: "q", Label: "C", Island: isl, Pick: &ComboboxPick{}}, nil)
	})
	refuse(t, "Control.ID", func() {
		Combobox(ComboboxProps{ID: "c", Name: "q", Label: "C", Options: []ComboboxOption{{Label: "a"}},
			Control: &FieldControl{ID: "other"}}, nil)
	})
}

func TestSweepPickersComboboxDisabledOptionIsNoLink(t *testing.T) {
	got := Combobox(ComboboxProps{ID: "c", Name: "q", Label: "C",
		Options: []ComboboxOption{{Label: "Docs", Href: "/docs", Disabled: true}}}, nil)
	hasNot(t, got, `href=`, "a disabled option still navigates")
	hasNot(t, got, `data-cui-push-state`, "a disabled option still carries the navigator")
	has(t, got, `aria-disabled="true"`, "the disabled option is not marked")
}

func TestSweepPickersMultiSelectRequiresDisclosureBehavior(t *testing.T) {
	e, ok := uiregistry.LookupBehavior(MultiSelectBehaviorName)
	if !ok || !slices.Contains(e.Requires, DisclosureBehaviorName) {
		t.Fatalf("%s does not require %s: the multiselect's disclosure would have no Escape or aria mirror", MultiSelectBehaviorName, DisclosureBehaviorName)
	}
}

func TestSweepPickersMultiSelectUnfilledDisclosureIsOwned(t *testing.T) {
	got := MultiSelect(MultiSelectProps{Name: "l", Label: "L", Options: []MultiSelectOption{{Value: "a", Label: "A"}}}, nil)
	has(t, got, `<details data-hui-disclosure="" data-cui-internal="">`, "an unfilled multiselect left its disclosure root unowned")
}

func TestSweepPickersDatePickerMonthNavIsAnIsland(t *testing.T) {
	isl := Island{Endpoint: "/cal", Signal: "cal"}
	got := string(DatePicker(DatePickerProps{ID: "d", Label: "Due", Name: "due", Value: "2026-11-05",
		Month: "2026-11", Min: "2026-11-01", Island: isl}, nil))
	navs := regexp.MustCompile(`<a [^>]*data-hui-datepicker-nav="[^"]*"[^>]*>`).FindAllString(got, -1)
	if len(navs) != 4 {
		t.Fatalf("want 4 nav anchors, got %d", len(navs))
	}
	for _, a := range navs {
		if strings.Contains(a, "aria-label") {
			t.Errorf("a nav anchor is named by aria-label: %s", a)
		}
		if strings.Contains(a, "data-cui-push-state") {
			t.Errorf("a month change writes a history entry: %s", a)
		}
		if strings.Contains(a, "href=") && !strings.Contains(a, `data-cui-rpc-error-toast="`) {
			t.Errorf("a live nav anchor has no error toast: %s", a)
		}
	}
	if !strings.Contains(got, `<a aria-disabled="true" data-hui-datepicker-nav="prev-month">`) {
		t.Errorf("a nav anchor that cannot move is not disabled")
	}
	if !strings.Contains(got, `aria-pressed="true" data-hui-date="2026-11-05" tabindex="0"`) {
		t.Errorf("the selected day's button does not carry aria-pressed and the tab stop")
	}
	later := string(DatePicker(DatePickerProps{ID: "d", Label: "Due", Name: "due",
		Month: "2027-03", Min: "2026-11-01", Island: isl}, nil))
	if !strings.Contains(later, `data-hui-datepicker-nav="prev-year" href="?month=2026-11"`) {
		t.Errorf("a year back past Min did not land on Min's month")
	}
}

func sweepPickersDay(p DatePickerProps) string {
	p.ID, p.Label, p.Island = "d", "Due", Island{Endpoint: "/cal", Signal: "cal"}
	if p.Range == nil {
		p.Name = "due"
	}
	if p.Month == "" {
		p.Month = "2026-11"
	}
	return string(DatePicker(p, nil))
}

func TestSweepPickersDatePickerTabStopOrder(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		p          DatePickerProps
	}{
		{"today in the month", "2026-11-10", DatePickerProps{Today: "2026-11-10"}},
		{"the first pickable day", "2026-11-02", DatePickerProps{Today: "2026-10-05",
			Days: map[string]DateDay{"2026-11-01": {Disabled: true}}}},
		{"the selected day before today", "2026-11-20", DatePickerProps{Value: "2026-11-20", Today: "2026-11-10"}},
	} {
		got := sweepPickersDay(tc.p)
		if !strings.Contains(got, `data-hui-date="`+tc.want+`" tabindex="0"`) {
			t.Errorf("%s: %s is not the tab stop", tc.name, tc.want)
		}
		if n := strings.Count(got, `tabindex="0"`); n != 1 {
			t.Errorf("%s: %d tab stops, want 1", tc.name, n)
		}
	}
}

func TestSweepPickersDatePickerHalfRangeSelectsStartOnly(t *testing.T) {
	got := sweepPickersDay(DatePickerProps{Range: &DateRange{StartName: "a", EndName: "b",
		Start: "2026-11-10", End: "2026-11-05"}})
	if n := strings.Count(got, `aria-selected="true"`); n != 1 {
		t.Errorf("an end before the start selected %d cells, want the start alone", n)
	}
	if !strings.Contains(got, `<td aria-selected="true" data-hui-datepicker-range="start">`) {
		t.Errorf("the start of a half range is not marked")
	}
}

func TestSweepPickersDatePickerYearZeroMonthFallsBack(t *testing.T) {
	got := sweepPickersDay(DatePickerProps{Month: "0000-01", Value: "2026-11-05"})
	if !strings.Contains(got, `data-hui-datepicker-grid="2026-11"`) {
		t.Errorf("a year-zero Month did not fall back to the value's month")
	}
}

func TestSweepPickersDatePickerNoteDescribesItsDay(t *testing.T) {
	got := sweepPickersDay(DatePickerProps{Days: map[string]DateDay{"2026-11-21": {Note: "2 left"}}})
	if !strings.Contains(got, `aria-describedby="d-note-2026-11-21"`) || !strings.Contains(got, `id="d-note-2026-11-21"`) {
		t.Errorf("a day's note does not describe its button")
	}
}

func TestSweepPickersDatePickerReplaysColdClicks(t *testing.T) {
	e, ok := uiregistry.LookupBehavior(DatePickerBehaviorName)
	if !ok {
		t.Fatal("headless-datepicker is not registered")
	}
	for _, sel := range []string{"[data-hui-date]", "[data-hui-datepicker-nav]"} {
		if !slices.ContainsFunc(e.Interactions, func(i uiregistry.Interaction) bool {
			return i.Event == "click" && i.Selector == sel
		}) {
			t.Errorf("a click on %s before the module loads is not replayed", sel)
		}
	}
}

func TestSweepPickersRepeaterActionIsSameOrigin(t *testing.T) {
	refuse(t, "same-origin", func() {
		Repeater(RepeaterProps{Name: "r", Action: "//evil.example/x",
			Island: Island{Endpoint: "/i", Signal: "s"}}, nil)
	})
}

func TestSweepPickersNumbersLoadsOnNumberSignals(t *testing.T) {
	e, ok := uiregistry.LookupBehavior(NumbersBehaviorName)
	if !ok || !slices.Equal(e.Markers, []string{`[data-cui-signal-mode="number"]`}) {
		t.Fatalf("headless-numbers does not load on number-mode signals: %+v", e)
	}
}

func TestSweepPickersRepeaterDefaultControlNames(t *testing.T) {
	got := Repeater(RepeaterProps{Name: "links", Items: []RepeaterItem{{}}}, nil)
	has(t, got, `name="links_add"`, "the add control did not default its name to Name_add")
	has(t, got, `name="links_remove"`, "the remove control did not default its name to Name_remove")
	has(t, got, `name="links_add" type="submit" value="1"`, "the add control did not default its value to 1")
}
