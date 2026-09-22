package headless

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// The stateful form controls: a number input flanked by steppers, a
// labelled range slider with an output, the two-thumb range pair, and
// the rating radio group. Every one of them is a native control first
// — a number input, a range input, a radio — so the no-script page is
// not a degraded page but the same page, and the module that binds
// the data-hui-* hooks (headless-controls) only ever enhances: it
// steps the number, mirrors the slider's output, and cross-clamps the
// pair, and it never repairs contradictory server props — those are
// refused here, at render.

// NumberInput parts. The input is the control the label points at;
// the two buttons are the component's own.
const (
	PartNumberDecrement Part = "number-decrement"
	PartNumberIncrement Part = "number-increment"
)

// NumberInputProps configures a number field with explicit −/+ buttons.
type NumberInputProps struct {
	// Name is the form-field name. Required.
	Name string
	// Label is the visible label. Required: it names the control and
	// the two buttons (through NumberDecrement / NumberIncrement).
	Label string
	// Value is the initial value as text, so a decimal step's value
	// travels unchanged. Empty means no value. A value that is not a
	// number, or violates a supplied bound, is refused.
	Value string
	// Min and Max bound the value. Nil leaves the bound to the
	// server; a Min above a Max is refused.
	Min *int
	Max *int
	// Step is the stepper granularity. Default 1; positive only.
	Step int
	// Required and Disabled are the input's own states; Disabled also
	// takes the buttons out.
	Required bool
	Disabled bool
	// Help is the rule the value obeys; Error is the violation. Both
	// reach the input's description, the error first.
	Help  string
	Error string

	ID         string
	ExtraAttrs html.Attrs

	// Parts: attrs and binds on every part. Strings carry the two
	// buttons' names.
	Parts Parts
	// Strings are the strings this component says. Nil means the English
	// defaults; a layer above sets them from the request's language.
	Strings *Strings
}

// NumberInput renders the number field between its steppers.
func NumberInput(p NumberInputProps, s Classes) render.HTML {
	if p.Name == "" {
		panic("headless: NumberInput requires Name — a control with no name submits nothing")
	}
	// Trimmed: a label of spaces names nothing, exactly like no label.
	if strings.TrimSpace(p.Label) == "" {
		panic("headless: NumberInput requires Label — the control and both buttons are named from it")
	}
	step := p.Step
	if step == 0 {
		step = 1
	}
	if step <= 0 {
		panic("headless: NumberInput Step " + strconv.Itoa(p.Step) + " must be positive — a step of zero is a button that changes nothing")
	}
	if p.Min != nil && p.Max != nil && *p.Min > *p.Max {
		panic("headless: NumberInput Min " + strconv.Itoa(*p.Min) + " is above Max " + strconv.Itoa(*p.Max) + " — an empty range is a configuration error, not a field")
	}
	id := orDefault(p.ID, p.Name)
	// The value is DATA, not configuration: a form re-rendered with
	// what the reader posted renders it back as given (the browser
	// marks it invalid against the bounds; the Error prop is how the
	// server says so). Only the control bytes are scrubbed.
	value := scrubControlBytes(p.Value)

	w := p.Strings.Resolve()
	b := p.Parts.Box(s)
	describedBy, hintID, errID := Describe(id, p.Help, p.Error)

	// The input: a real, named number control. The −/+ hooks ride the
	// buttons; the input is found through data-hui-number-input-for,
	// so an island swap that replaces the row rebinds on arrival.
	input := html.Attrs{
		"type": "number",
		"name": p.Name,
		"id":   id,
		"step": strconv.Itoa(step),
	}
	if value != "" {
		input["value"] = value
	}
	if p.Min != nil {
		input["min"] = strconv.Itoa(*p.Min)
	}
	if p.Max != nil {
		input["max"] = strconv.Itoa(*p.Max)
	}
	attrsSet(input, "aria-describedby", describedBy)
	Flag(input, "required", p.Required)
	Flag(input, "disabled", p.Disabled)
	if p.Error != "" {
		input["aria-invalid"] = "true"
	}

	stepper := func(part Part, hook, label string, glyph string) render.HTML {
		a := html.Attrs{
			"type":                      "button",
			hook:                        "",
			"aria-label":                label,
			"data-hui-number-input-for": id,
		}
		Flag(a, "disabled", p.Disabled)
		return b.El("button", part, a, render.Text(glyph))
	}

	kids := []render.HTML{
		b.El("label", PartLabel, Attrs(map[string]string{"for": id}), render.Text(p.Label)),
		// The three controls draw one bordered row in the sheet, which
		// is why they share a wrapper: a pill border belongs to the
		// trio, not to each element of it.
		b.El("div", PartFieldRow, nil,
			stepper(PartNumberDecrement, "data-hui-number-input-decrement",
				fmt.Sprintf(w.NumberDecrement, p.Label), "−"),
			b.El("input", PartControl, input),
			stepper(PartNumberIncrement, "data-hui-number-input-increment",
				fmt.Sprintf(w.NumberIncrement, p.Label), "+"),
		),
	}
	if p.Error != "" {
		kids = append(kids, b.El("p", PartError,
			Attrs(map[string]string{"id": errID, "role": "alert"}), render.Text(p.Error)))
	}
	if p.Help != "" {
		kids = append(kids, b.El("p", PartHint,
			Attrs(map[string]string{"id": hintID}), render.Text(p.Help)))
	}

	own := Merge(Safe(p.ExtraAttrs, "role", "aria-label", "type", "name", "step",
		"value", "min", "max", "disabled", "required", "aria-invalid", "aria-describedby"),
		Attrs(map[string]string{
			"role":       "group",
			"aria-label": p.Label,
			"id":         p.ID,
		}))
	return b.El("div", PartRoot, own, kids...)
}

// ─── Slider ─────────────────────────────────────────────────────────

// Slider parts.
const (
	PartSliderOutput Part = "slider-output"
	PartSliderEdges  Part = "slider-edges"
	PartSliderEdge   Part = "slider-edge"
)

// SliderProps configures a labelled range control.
type SliderProps struct {
	// Name is the form-field name. Required.
	Name string
	// Label is the visible label. Required.
	Label string
	// Min and Max bound the range. Both zero means 0..100; a Min
	// above or equal to a Max is refused.
	Min int
	Max int
	// Step is the granularity. Default 1; positive only.
	Step int
	// Value is the initial value. Outside the range clamps to the
	// nearest bound, the way the browser treats a dragged thumb; a
	// value between steps renders as given and snaps on first
	// interaction — the module mirrors whatever the thumb says, so
	// there is no disagreement to refuse.
	Value int
	// ShowValue renders the output beside the label and the hook the
	// module mirrors the live value through.
	ShowValue bool
	// ShowEdgeLabels renders Min and Max under the track.
	ShowEdgeLabels bool
	// Disabled disables the control.
	Disabled bool

	ID         string
	ExtraAttrs html.Attrs

	// Parts: attrs and binds on every part.
	Parts Parts
	// Strings are the strings this component says. Nil means the English
	// defaults; a layer above sets them from the request's language.
	Strings *Strings
}

// Slider renders the labelled range control.
func Slider(p SliderProps, s Classes) render.HTML {
	if p.Name == "" {
		panic("headless: Slider requires Name — a control with no name submits nothing")
	}
	// Trimmed: a label of spaces names nothing, exactly like no label.
	if strings.TrimSpace(p.Label) == "" {
		panic("headless: Slider requires Label — the control is named by it")
	}
	min, max := p.Min, p.Max
	if min == 0 && max == 0 {
		max = 100
	}
	if min >= max {
		panic("headless: Slider Min " + strconv.Itoa(min) + " must be below Max " + strconv.Itoa(max))
	}
	step := p.Step
	if step == 0 {
		step = 1
	}
	if step <= 0 {
		panic("headless: Slider Step " + strconv.Itoa(p.Step) + " must be positive")
	}
	// The value is data: a re-rendered form clamps what was posted
	// into the range the props declare, the way the browser would —
	// configuration errors (an empty range, a non-positive step) are
	// refused above, a value between steps is not a configuration
	// error.
	value := p.Value
	if value < min {
		value = min
	}
	if value > max {
		value = max
	}
	id := orDefault(p.ID, p.Name)

	b := p.Parts.Box(s)
	input := html.Attrs{
		"type":       "range",
		"name":       p.Name,
		"id":         id,
		"min":        strconv.Itoa(min),
		"max":        strconv.Itoa(max),
		"step":       strconv.Itoa(step),
		"value":      strconv.Itoa(value),
		"aria-label": p.Label,
	}
	Flag(input, "disabled", p.Disabled)

	var output render.HTML
	if p.ShowValue {
		// A real <output for>: the browser associates it with the
		// input, and the module (through data-hui-slider-output)
		// keeps the text in step while the thumb moves. The SSR text
		// is the true value, so the number is right before script.
		output = b.El("output", PartSliderOutput, html.Attrs{
			"for":                    id,
			"data-hui-slider-output": "",
		}, render.Text(strconv.Itoa(value)))
	}

	kids := []render.HTML{
		b.El("label", PartLabel, Attrs(map[string]string{"for": id}), render.Text(p.Label)),
		output,
		b.El("input", PartControl, input),
	}
	if p.ShowEdgeLabels {
		kids = append(kids, b.El("div", PartSliderEdges, nil,
			b.El("span", PartSliderEdge, nil, render.Text(strconv.Itoa(min))),
			b.El("span", PartSliderEdge, nil, render.Text(strconv.Itoa(max))),
		))
	}

	own := Merge(Safe(p.ExtraAttrs, "role", "aria-label", "type", "name",
		"min", "max", "step", "value", "disabled", "aria-label"), Attrs(map[string]string{"id": p.ID}))
	Mark(own, "data-hui-slider")
	if p.Disabled {
		own["data-state"] = "disabled"
	}
	return b.El("div", PartRoot, own, kids...)
}

// ─── RangeSlider ────────────────────────────────────────────────────

// RangeSlider parts. The two inputs are the component's thumbs; the
// track is the positioning context the sheet overlays them on.
const (
	PartRangeLow    Part = "range-low"
	PartRangeHigh   Part = "range-high"
	PartRangeOutput Part = "range-output"
	PartRangeTrack  Part = "range-track"
)

// RangeSliderProps configures the two-thumb range pair.
type RangeSliderProps struct {
	// Name is the base form-field name. Two inputs ship, Name+"-min"
	// and Name+"-max", so the server receives the pair without
	// parsing a composite string.
	Name string
	// Label is the group's name, read by assistive technology and
	// built into each thumb's name (RangeLow / RangeHigh).
	Label string
	// Min, Max, Step as for Slider.
	Min  int
	Max  int
	Step int
	// ValueLow and ValueHigh are the thumbs' values. Both zero means
	// Min and Max. Values outside the bounds clamp to them, and a
	// crossed pair is ordered low, high — the same repair the module
	// makes of a dragged pair.
	ValueLow  int
	ValueHigh int
	// ShowValue renders the pair's one output sentence, from
	// RangeValue; the module keeps it in step using the same
	// sentence, which travels with the output.
	ShowValue bool
	// Disabled disables both thumbs.
	Disabled bool

	ID         string
	ExtraAttrs html.Attrs

	// Parts: attrs and binds on every part. Strings carry the two
	// thumbs' names and the output sentence.
	Parts Parts
	// Strings are the strings this component says. Nil means the English
	// defaults; a layer above sets them from the request's language.
	Strings *Strings
}

// RangeSlider renders the two-thumb range pair.
func RangeSlider(p RangeSliderProps, s Classes) render.HTML {
	if p.Name == "" {
		panic("headless: RangeSlider requires Name — a control with no name submits nothing")
	}
	// Trimmed: a label of spaces names nothing, exactly like no label.
	if strings.TrimSpace(p.Label) == "" {
		panic("headless: RangeSlider requires Label — the group and both thumbs are named from it")
	}
	min, max := p.Min, p.Max
	if min == 0 && max == 0 {
		max = 100
	}
	if min >= max {
		panic("headless: RangeSlider Min " + strconv.Itoa(min) + " must be below Max " + strconv.Itoa(max))
	}
	step := p.Step
	if step == 0 {
		step = 1
	}
	if step <= 0 {
		panic("headless: RangeSlider Step " + strconv.Itoa(p.Step) + " must be positive")
	}
	// The values are data: both clamp into the declared range and a
	// crossed pair is ordered low, high — the same repair the browser
	// makes of a dragged pair, so a re-rendered form shows the posted
	// pair as a valid range.
	lo, hi := p.ValueLow, p.ValueHigh
	if lo == 0 && hi == 0 {
		lo, hi = min, max
	}
	if lo < min {
		lo = min
	}
	if lo > max {
		lo = max
	}
	if hi < min {
		hi = min
	}
	if hi > max {
		hi = max
	}
	if lo > hi {
		lo, hi = hi, lo
	}
	id := orDefault(p.ID, p.Name)

	w := p.Strings.Resolve()
	b := p.Parts.Box(s)
	thumb := func(part Part, suffix, hook, name string, val int) render.HTML {
		a := html.Attrs{
			"type":       "range",
			"name":       p.Name + "-" + suffix,
			"id":         id + "-" + suffix,
			"min":        strconv.Itoa(min),
			"max":        strconv.Itoa(max),
			"step":       strconv.Itoa(step),
			"value":      strconv.Itoa(val),
			"aria-label": name,
			hook:         "",
		}
		Flag(a, "disabled", p.Disabled)
		return b.El("input", part, a)
	}

	var output render.HTML
	if p.ShowValue {
		// The hook carries the sentence's SHAPE and the text carries
		// the sentence: the module re-formats through the shape as a
		// thumb moves, so a translated page keeps its own words live.
		output = b.El("output", PartRangeOutput, html.Attrs{
			"for":                          id + "-min " + id + "-max",
			"data-hui-range-slider-output": w.RangeValue,
		}, render.Text(fmt.Sprintf(w.RangeValue, strconv.Itoa(lo), strconv.Itoa(hi))))
	}

	own := Merge(Safe(p.ExtraAttrs, "role", "aria-label", "type", "name",
		"min", "max", "step", "value", "disabled"), Attrs(map[string]string{
		"role":       "group",
		"aria-label": p.Label,
		"id":         p.ID,
	}))
	Mark(own, "data-hui-range-slider")
	if p.Disabled {
		own["data-state"] = "disabled"
	}
	return b.El("div", PartRoot, own,
		b.El("span", PartLabel, nil, render.Text(p.Label)),
		output,
		b.El("div", PartRangeTrack, nil,
			thumb(PartRangeLow, "min", "data-hui-range-slider-low",
				fmt.Sprintf(w.RangeLow, p.Label), lo),
			thumb(PartRangeHigh, "max", "data-hui-range-slider-high",
				fmt.Sprintf(w.RangeHigh, p.Label), hi),
		),
	)
}

// ─── Rating ─────────────────────────────────────────────────────────

// Rating parts: each choice is one radio (PartControl) wrapped by its
// label (PartOption), the glyph inside the label aria-hidden.
const (
	PartOptionChoice Part = "option-choice"
)

// RatingProps configures a rating as a radio group.
type RatingProps struct {
	// Name is the form-field name. Required; the group submits the
	// chosen value 1..Max under it.
	Name string
	// Label names the group for assistive technology. Required.
	Label string
	// Max is the ceiling. Default 5; below 1 is refused.
	Max int
	// Value is the current choice, 0 (none) to Max.
	Value int
	// Icon is the glyph cloned into every choice. The default is a
	// star; a caller's icon reaches every choice the same way.
	Icon render.HTML
	// Disabled disables every radio.
	Disabled bool

	ID         string
	ExtraAttrs html.Attrs

	// Parts: attrs and binds on every part. Strings carry each
	// choice's name (RatingChoice).
	Parts Parts
	// Strings are the strings this component says. Nil means the English
	// defaults; a layer above sets them from the request's language.
	Strings *Strings
}

// Rating renders the radio group.
//
// The fieldset/legend and the radio inputs ARE the contract: native
// arrows move between choices, the form POSTs the chosen value, and
// every choice is named ("3 out of 5") — which is why no behaviour
// module exists for it. A second state owner would add nothing the
// radios do not already guarantee.
//
// The choices render in REVERSE order (Max..1): the sheet's sibling
// selector cascades the highlight backward from the checked or
// hovered choice to every earlier one, with no script at all.
func Rating(p RatingProps, s Classes) render.HTML {
	if p.Name == "" {
		panic("headless: Rating requires Name — a control with no name submits nothing")
	}
	if p.Label == "" {
		panic("headless: Rating requires Label — the group is named by it")
	}
	max := p.Max
	if max == 0 {
		max = 5
	}
	if max < 1 {
		panic("headless: Rating Max " + strconv.Itoa(max) + " is below 1 — a rating with no choices is decoration")
	}
	// The value is data: it clamps into 0..Max rather than refusing,
	// so a re-rendered form with a posted out-of-range value still
	// renders a group.
	value := p.Value
	if value < 0 {
		value = 0
	}
	if value > max {
		value = max
	}

	w := p.Strings.Resolve()
	b := p.Parts.Box(s)
	glyph := p.Icon
	if glyph == "" {
		glyph = render.Text("★")
	}

	own := Merge(Safe(p.ExtraAttrs, "role", "aria-label"), Attrs(map[string]string{
		"role":       "radiogroup",
		"aria-label": p.Label,
		"id":         p.ID,
	}))
	items := make([]render.HTML, 0, max*2)
	for i := max; i >= 1; i-- {
		idV := p.Name + "-" + strconv.Itoa(i)
		radio := html.Attrs{
			"type":       "radio",
			"name":       p.Name,
			"id":         idV,
			"value":      strconv.Itoa(i),
			"aria-label": fmt.Sprintf(w.RatingChoice, i, max),
		}
		if value == i {
			Mark(radio, "checked")
		}
		Flag(radio, "disabled", p.Disabled)
		items = append(items, b.El("input", PartControl, radio))
		label := html.Attrs{"for": idV}
		items = append(items, b.El("label", PartOptionChoice, label,
			b.El("span", PartIcon, Attrs(map[string]string{"aria-hidden": "true"}), glyph)))
	}
	return b.El("fieldset", PartRoot, own, items...)
}

func init() {
	one, five, ten := 1, 5, 10
	Register(Spec{
		Name: "NumberInput",
		Anatomy: []Part{PartRoot, PartLabel, PartFieldRow, PartControl,
			PartNumberDecrement, PartNumberIncrement, PartHint, PartError},
		Hooks: []string{"data-hui-number-input-decrement",
			"data-hui-number-input-increment", "data-hui-number-input-for"},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return NumberInput(NumberInputProps{Name: "port", Label: "Port", Parts: parts}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "unbounded, with help",
				Why:  "a real, named number input works with no script at all, and the rule it obeys rides its description — the buttons are enhancement only",
				HTML: NumberInput(NumberInputProps{Name: "port", Label: "Port", Value: "8080",
					Help: "1024 to 65535"}, s),
			}, {
				Name: "bounded at both edges",
				Why:  "the bounds are on the input (the browser enforces them for typing) and the buttons' names say which field they step — the hook the module resolves the input by rides each button",
				HTML: NumberInput(NumberInputProps{Name: "replicas", Label: "Replicas", Value: "3",
					Min: &one, Max: &ten}, s),
			}, {
				Name: "disabled with an error",
				Why:  "disabled takes the buttons out with the input, and the violation is read before the rule because the correction arrives before the reminder",
				HTML: NumberInput(NumberInputProps{Name: "quota", Label: "Quota", Value: "2",
					Min: &one, Max: &ten, Disabled: true, Error: "Below the minimum"}, s),
			}}
		},
	})

	Register(Spec{
		Name: "Slider",
		Anatomy: []Part{PartRoot, PartLabel, PartControl, PartSliderOutput,
			PartSliderEdges, PartSliderEdge},
		Hooks: []string{"data-hui-slider", "data-hui-slider-output"},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return Slider(SliderProps{Name: "cpu", Label: "CPU share", Parts: parts}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "with a live output",
				Why:  "the output is a real form output associated with the input, its SSR text is the true value, and the hook it carries is how the module keeps it in step while the thumb moves",
				HTML: Slider(SliderProps{Name: "cpu", Label: "CPU share", Value: 40,
					ShowValue: true, ShowEdgeLabels: true}, s),
			}, {
				Name: "bare, no output",
				Why:  "a slider with no output renders no hook at all — markup carried for no one is the defect the hooks gate exists to catch",
				HTML: Slider(SliderProps{Name: "mem", Label: "Memory"}, s),
			}, {
				Name: "bounded custom step",
				Why:  "a step the value lands on: the browser, the server and the module all read the same three attributes, and a value between steps renders as given — data, not a broken prop",
				HTML: Slider(SliderProps{Name: "batch", Label: "Batch size", Min: 10, Max: 100,
					Step: 10, Value: 50, ShowValue: true}, s),
			}}
		},
	})

	Register(Spec{
		Name: "RangeSlider",
		Anatomy: []Part{PartRoot, PartLabel, PartRangeOutput, PartRangeTrack,
			PartRangeLow, PartRangeHigh},
		Hooks: []string{"data-hui-range-slider", "data-hui-range-slider-low",
			"data-hui-range-slider-high", "data-hui-range-slider-output"},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return RangeSlider(RangeSliderProps{Name: "price", Label: "Price", Parts: parts}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "a full default pair",
				Why:  "two named range inputs the form submits as -min and -max, each thumb named (Minimum/Maximum) because two anonymous thumbs in one group tell a screen reader user nothing",
				HTML: RangeSlider(RangeSliderProps{Name: "price", Label: "Price", ShowValue: true}, s),
			}, {
				Name: "a narrowed pair",
				Why:  "the output sentence travels with the output (the module re-formats through it, so a translated page says its own words live) and a crossed pair renders ordered — the same repair the module makes of a drag",
				HTML: RangeSlider(RangeSliderProps{Name: "age", Label: "Age", Min: 18, Max: 65,
					Step: 1, ValueLow: 25, ValueHigh: 40, ShowValue: true}, s),
			}, {
				Name: "equal values",
				Why:  "low equal to high is a real pair — a single-point range, rendered as given rather than repaired",
				HTML: RangeSlider(RangeSliderProps{Name: "window", Label: "Window",
					ValueLow: five, ValueHigh: five}, s),
			}}
		},
	})

	Register(Spec{
		Name:    "Rating",
		Anatomy: []Part{PartRoot, PartControl, PartOptionChoice, PartIcon},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return Rating(RatingProps{Name: "score", Label: "Score", Parts: parts}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "no selection",
				Why:  "zero is a real value — no rating chosen — and the radios still submit nothing rather than a pretend zero",
				HTML: Rating(RatingProps{Name: "score", Label: "Score"}, s),
			}, {
				Name: "a middle choice",
				Why:  "each radio says its own name (\"3 out of 5\") and the checked one is the group's value the form POSTs — the whole contract, with no module at all",
				HTML: Rating(RatingProps{Name: "score", Label: "Score", Value: 3}, s),
			}, {
				Name: "a caller's glyph, disabled",
				Why:  "the glyph is presentation, hidden from assistive technology and swappable, and disabled takes the group out without unravelling the names",
				HTML: Rating(RatingProps{Name: "heat", Label: "Heat", Value: 2, Disabled: true,
					Icon: render.HTML(`<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 2z"/></svg>`)}, s),
			}}
		},
	})
}
