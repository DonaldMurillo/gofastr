package headless

import (
	"strings"
	"testing"
)

// The new control family's own contracts, beside the universal sweeps
// the harness runs: what each control submits, what its buttons say,
// and the broken configurations each refuses — while posted data
// (values past bounds, non-numeric text, crossed pairs) is kept or
// clamped the way the browser would, never refused at render.

// A counter's buttons speak the kernel's increment spelling unchanged:
// name alone is +1, name:n is ±n, and the decrement always spells its
// negative.
func TestCounterRendersTheKernelIncrementContract(t *testing.T) {
	got := Counter(CounterProps{Signal: "qty", Label: "Quantity", Value: 2}, nil)
	has(t, got, `data-fui-signal-inc="qty:-1"`, "the decrement did not spell its negative step")
	has(t, got, `data-fui-signal-inc="qty"`, "a step of one did not render the bare signal name")
	has(t, got, `data-fui-signal="qty"`, "the display did not follow the signal")
	has(t, got, ">2</span>", "the SSR text is not the true count")

	stepped := Counter(CounterProps{Signal: "qty", Label: "Quantity", Step: 5}, nil)
	has(t, stepped, `data-fui-signal-inc="qty:5"`, "a step above one did not spell it")

	named := Counter(CounterProps{Signal: "seats", Label: "Seats", Name: "seats", Value: 4}, nil)
	has(t, named, `data-fui-signal-attr="value"`, "a named counter's input does not follow the signal through its value attribute")
	has(t, named, `name="seats"`, "a named counter did not render a submittable field")
}

// The animation hooks render only when there is an animation, and the
// final value stays the SSR text either way.
func TestCounterAnimationHooksRenderOnlyWhenAnimated(t *testing.T) {
	from := 4000
	animated := Counter(CounterProps{Signal: "deployed", Label: "Deployed", Value: 4820,
		AnimateFrom: &from, DurationMS: 600}, nil)
	has(t, animated, `data-hui-counter-animate=""`, "the animate hook is missing")
	has(t, animated, `data-hui-counter-from="4000"`, "the count the animation starts from is missing")
	has(t, animated, `data-hui-counter-ms="600"`, "the duration bound is missing")
	has(t, animated, ">4820</span>", "the final value is not the SSR text")

	plain := Counter(CounterProps{Signal: "qty", Label: "Qty"}, nil)
	hasNot(t, plain, "data-hui-counter", "an unanimated counter carries the animation hooks for no one")
}

// A reserved signal name is refused: the kernel drops it, so the
// counter would change a value nothing reads.
func TestCounterRefusesAReservedSignalAndBadStep(t *testing.T) {
	refuse(t, "reserved", func() {
		Counter(CounterProps{Signal: "__proto__", Label: "Qty"}, nil)
	})
	refuse(t, "positive", func() {
		Counter(CounterProps{Signal: "qty", Label: "Qty", Step: -2}, nil)
	})
	refuse(t, "negative", func() {
		Counter(CounterProps{Signal: "qty", Label: "Qty", DurationMS: -1}, nil)
	})
	refuse(t, "Signal", func() {
		Counter(CounterProps{Label: "Qty"}, nil)
	})
}

// A label of spaces names nothing: the emptiness judgment trims, so
// the group still takes the Strings default instead of an accessible
// name of whitespace.
func TestCounterWhitespaceLabelTakesTheDefault(t *testing.T) {
	got := Counter(CounterProps{Signal: "qty", Label: "   "}, nil)
	has(t, got, `aria-label="Counter"`, "a whitespace-only Label rendered as the group's name")
}

// The stepper's bounds are on the input, its buttons name the field
// they step, and each button resolves the input by hook.
func TestNumberInputStepsAreNamedAndWired(t *testing.T) {
	min, max := 1, 10
	got := NumberInput(NumberInputProps{Name: "replicas", Label: "Replicas",
		Value: "3", Min: &min, Max: &max}, nil)
	for _, want := range []string{
		`aria-label="Decrement Replicas"`,
		`aria-label="Increment Replicas"`,
		`data-hui-number-input-for="replicas"`,
		`min="1"`, `max="10"`, `step="1"`, `value="3"`,
	} {
		has(t, got, want, "the stepper's wiring is incomplete")
	}
	// The control's description carries the rule it obeys.
	helped := NumberInput(NumberInputProps{Name: "port", Label: "Port", Help: "1024 up"}, nil)
	has(t, helped, `aria-describedby="port-hint"`, "the hint did not reach the control's description")
	errored := NumberInput(NumberInputProps{Name: "port", Label: "Port", Error: "Taken"}, nil)
	has(t, errored, `aria-describedby="port-error"`, "the error did not reach the control's description")
	has(t, errored, `aria-invalid="true"`, "the control does not say it is wrong")
}

// Configuration is refused; data is kept. A value past the bounds or
// not a number at all renders as given — the browser marks it invalid
// and the Error prop is how the server says so — while a Min above a
// Max and a non-positive step stay refused.
func TestNumberInputRefusesBrokenConfigAndKeepsPostedData(t *testing.T) {
	min, max, lowMax := 1, 10, 0
	refuse(t, "Name", func() {
		NumberInput(NumberInputProps{Label: "Port"}, nil)
	})
	refuse(t, "Label", func() {
		NumberInput(NumberInputProps{Name: "port"}, nil)
	})
	// A label of spaces names nothing: the emptiness judgment trims,
	// so it refuses exactly like the empty string.
	refuse(t, "Label", func() {
		NumberInput(NumberInputProps{Name: "port", Label: "   "}, nil)
	})
	refuse(t, "positive", func() {
		NumberInput(NumberInputProps{Name: "port", Label: "Port", Step: -1}, nil)
	})
	refuse(t, "is above Max", func() {
		NumberInput(NumberInputProps{Name: "port", Label: "Port", Min: &max, Max: &lowMax}, nil)
	})
	// A posted value past the bounds renders as given, no panic.
	past := NumberInput(NumberInputProps{Name: "port", Label: "Port", Value: "11", Min: &min, Max: &max}, nil)
	has(t, past, `value="11"`, "a posted value past Max was dropped instead of rendered for the browser to mark invalid")
	notNum := NumberInput(NumberInputProps{Name: "port", Label: "Port", Value: "abc"}, nil)
	has(t, notNum, `value="abc"`, "a posted non-numeric value was dropped instead of rendered as given")
}

// Control bytes never reach the input's value: a CR LF in a posted
// value is scrubbed, the way the table's carried query is.
func TestNumberInputScrubsControlBytes(t *testing.T) {
	got := NumberInput(NumberInputProps{Name: "port", Label: "Port", Value: "80\r\n08"}, nil)
	has(t, got, `value="8008"`, "a CR LF in the value reached the input")
	hasNot(t, got, "\r", "a carriage return reached the markup")
}

// The slider's output is a real form output and the edge labels say
// the bounds; a slider without an output renders no hook at all.
func TestSliderOutputAndEdges(t *testing.T) {
	got := Slider(SliderProps{Name: "cpu", Label: "CPU share", Value: 40,
		ShowValue: true, ShowEdgeLabels: true}, nil)
	has(t, got, `<output data-hui-slider-output="" for="cpu">40</output>`, "the output is not a form output carrying its hook with the true value")
	has(t, got, ">0</span>", "the min edge label is missing")
	has(t, got, ">100</span>", "the max edge label is missing")

	bare := Slider(SliderProps{Name: "mem", Label: "Memory"}, nil)
	hasNot(t, bare, "data-hui-slider-output", "a slider with no output carries the mirror hook for no one")
}

// Configuration is refused (an empty range, a non-positive step); a
// posted value clamps into the declared range the way the browser
// would, and step alignment is not checked — a value between steps is
// data, not a broken prop.
func TestSliderRefusesBrokenConfigAndClampsPostedValues(t *testing.T) {
	refuse(t, "below Max", func() {
		Slider(SliderProps{Name: "cpu", Label: "CPU", Min: 5}, nil)
	})
	refuse(t, "positive", func() {
		Slider(SliderProps{Name: "cpu", Label: "CPU", Min: 0, Max: 100, Step: -1}, nil)
	})
	// A label of spaces names nothing: the emptiness judgment trims.
	refuse(t, "Label", func() {
		Slider(SliderProps{Name: "cpu", Label: "  "}, nil)
	})
	low := Slider(SliderProps{Name: "cpu", Label: "CPU", Min: 10, Max: 20, Value: 5}, nil)
	has(t, low, `value="10"`, "a posted value below Min did not clamp")
	high := Slider(SliderProps{Name: "cpu", Label: "CPU", Min: 10, Max: 20, Value: 99}, nil)
	has(t, high, `value="20"`, "a posted value above Max did not clamp")
	between := Slider(SliderProps{Name: "cpu", Label: "CPU", Min: 0, Max: 100, Step: 10, Value: 5}, nil)
	has(t, between, `value="5"`, "a posted value between steps was repaired instead of kept")
}

// The range pair submits two named fields, each thumb is named, and
// the output sentence travels with the output for the module to
// re-format.
func TestRangeSliderPairContract(t *testing.T) {
	got := RangeSlider(RangeSliderProps{Name: "price", Label: "Price",
		ValueLow: 20, ValueHigh: 80, ShowValue: true}, nil)
	for _, want := range []string{
		`name="price-min"`, `name="price-max"`,
		`aria-label="Minimum Price"`, `aria-label="Maximum Price"`,
		`data-hui-range-slider-output="%s to %s"`,
		`>20 to 80</output>`,
	} {
		has(t, got, want, "the pair's wiring is incomplete")
	}
}

// Configuration is refused; posted data is clamped and ordered: a
// crossed pair renders as the ordered pair, both values clamp into
// the range.
func TestRangeSliderOrdersAndClampsPostedPairs(t *testing.T) {
	refuse(t, "below Max", func() {
		RangeSlider(RangeSliderProps{Name: "price", Label: "Price", Min: 100, Max: 0}, nil)
	})
	refuse(t, "positive", func() {
		RangeSlider(RangeSliderProps{Name: "price", Label: "Price", Min: 0, Max: 100, Step: -1}, nil)
	})
	// A label of spaces names nothing: the emptiness judgment trims.
	refuse(t, "Label", func() {
		RangeSlider(RangeSliderProps{Name: "price", Label: " "}, nil)
	})
	crossed := RangeSlider(RangeSliderProps{Name: "price", Label: "Price", Min: 0, Max: 100, ValueLow: 80, ValueHigh: 20}, nil)
	has(t, crossed, `data-hui-range-slider-low="" id="price-min" max="100" min="0" name="price-min" step="1" type="range" value="20"`,
		"a crossed pair's low thumb was not ordered to the smaller value")
	has(t, crossed, `value="80"`, "a crossed pair's high thumb was not ordered to the larger value")
	outside := RangeSlider(RangeSliderProps{Name: "price", Label: "Price", Min: 0, Max: 50, ValueLow: 60, ValueHigh: 90}, nil)
	has(t, outside, `name="price-min" step="1" type="range" value="50"`, "a posted low above Max did not clamp")
	has(t, outside, `name="price-max" step="1" type="range" value="50"`, "a posted high above Max did not clamp")
	// Equal values are a single-point range, not a crossed pair.
	RangeSlider(RangeSliderProps{Name: "w", Label: "W", ValueLow: 5, ValueHigh: 5}, nil)
}

// The rating is a radio group whose every choice names itself; the
// checked one is the group's value.
func TestRatingRadioContract(t *testing.T) {
	got := Rating(RatingProps{Name: "score", Label: "Score", Value: 3}, nil)
	has(t, got, `role="radiogroup"`, "the group does not say it is one")
	has(t, got, `aria-label="3 out of 5"`, "a choice does not name itself")
	has(t, got, `aria-label="3 out of 5" checked=""`, "the chosen choice is not checked")
	// Reverse order: the highest choice is first in the markup, the
	// sheet's sibling cascade depends on it.
	if i := strings.Index(string(got), `value="5"`); i == -1 || strings.Index(string(got), `value="1"`) < i {
		t.Error("the choices do not render in reverse order — the stylesheet's cascade is built on it")
	}
}

func TestRatingRefusesBrokenGroupsAndClampsPostedValues(t *testing.T) {
	refuse(t, "Name", func() { Rating(RatingProps{Label: "Score"}, nil) })
	refuse(t, "Label", func() { Rating(RatingProps{Name: "score"}, nil) })
	refuse(t, "below 1", func() { Rating(RatingProps{Name: "score", Label: "Score", Max: -2}, nil) })
	// Posted values clamp into 0..Max rather than refusing the render.
	over := Rating(RatingProps{Name: "score", Label: "Score", Max: 3, Value: 4}, nil)
	has(t, over, `aria-label="3 out of 3" checked=""`, "a posted value above Max did not clamp to the ceiling")
	neg := Rating(RatingProps{Name: "score", Label: "Score", Value: -1}, nil)
	hasNot(t, neg, `checked=""`, "a posted negative value checked something")
}
