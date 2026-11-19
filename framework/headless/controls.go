package headless

import (
	"context"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// NumberInput parts.
const (
	PartNumberDecrement Part = "number-decrement"
	PartNumberIncrement Part = "number-increment"
)

// NumberInputProps configures a number field with explicit −/+ buttons.
type NumberInputProps struct {
	// Name is the form-field name. Required.
	Name string
	// Label names the control and both buttons. Required.
	Label string
	// Value is the initial value as text, rendered as given.
	Value string
	// Min and Max bound the value; nil is unbounded, Min above Max is refused.
	Min *int
	Max *int
	// Step is the stepper granularity. Default 1; positive only.
	Step     int
	Required bool
	// Disabled also disables the buttons.
	Disabled bool
	// Help and Error describe the input, the error first.
	Help  string
	Error string

	ID         string
	ExtraAttrs html.Attrs

	Parts   Parts
	Strings *Strings

	// Ctx scopes the id derived from Name when ID is empty.
	Ctx context.Context
}

// NumberInput renders the number field between its steppers.
func NumberInput(p NumberInputProps, s Classes) render.HTML {
	if p.Name == "" {
		panic("headless: NumberInput requires Name — a control with no name submits nothing")
	}
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
	id := instanceID(p.Ctx, p.ID, p.Name, "-hint", "-error")
	value := scrubControlBytes(p.Value)

	w := p.Strings.Resolve()
	b := p.Parts.Box(s)
	describedBy, hintID, errID := Describe(id, p.Help, p.Error)

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
		b.El("label", PartLabel, Internal(Attrs(map[string]string{"for": id})), render.Text(p.Label)),
		b.El("div", PartFieldRow, Internal(nil),
			stepper(PartNumberDecrement, "data-hui-number-input-decrement",
				w.fill(w.NumberDecrement, "label", p.Label), "−"),
			b.El("input", PartControl, input),
			stepper(PartNumberIncrement, "data-hui-number-input-increment",
				w.fill(w.NumberIncrement, "label", p.Label), "+"),
		),
	}
	if p.Error != "" {
		kids = append(kids, b.El("p", PartError,
			Internal(Attrs(map[string]string{"id": errID, "role": "alert"})), render.Text(p.Error)))
	}
	if p.Help != "" {
		kids = append(kids, b.El("p", PartHint,
			Internal(Attrs(map[string]string{"id": hintID})), render.Text(p.Help)))
	}

	own := Merge(Safe(p.ExtraAttrs, "role", "aria-label", "type", "name", "step",
		"value", "min", "max", "disabled", "required", "aria-invalid", "aria-describedby"),
		Attrs(map[string]string{
			"role":       "group",
			"aria-label": p.Label,
		}))
	return b.El("div", PartRoot, own, kids...)
}

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
	// Min and Max bound the range; both zero means 0..100, Min not below Max is refused.
	Min int
	Max int
	// Step is the granularity. Default 1; positive only.
	Step int
	// Value is the initial value, clamped into Min..Max.
	Value int
	// ShowValue renders a live output beside the label, and speaks the
	// same words on the thumb as aria-valuetext.
	ShowValue bool
	// ShowEdgeLabels renders Min and Max under the track.
	ShowEdgeLabels bool
	Disabled       bool

	ID         string
	ExtraAttrs html.Attrs

	Parts   Parts
	Strings *Strings

	// Ctx scopes the id derived from Name when ID is empty.
	Ctx context.Context
}

// Slider renders the labelled range control.
func Slider(p SliderProps, s Classes) render.HTML {
	if p.Name == "" {
		panic("headless: Slider requires Name — a control with no name submits nothing")
	}
	if strings.TrimSpace(p.Label) == "" {
		panic("headless: Slider requires Label — the control is named by it")
	}
	w := p.Strings.Resolve()
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
	value := p.Value
	if value < min {
		value = min
	}
	if value > max {
		value = max
	}
	id := instanceID(p.Ctx, p.ID, p.Name)

	b := p.Parts.Box(s)
	input := html.Attrs{
		"type":  "range",
		"name":  p.Name,
		"id":    id,
		"min":   strconv.Itoa(min),
		"max":   strconv.Itoa(max),
		"step":  strconv.Itoa(step),
		"value": strconv.Itoa(value),
	}
	if p.ShowValue {
		// The output's words, spoken: headless-controls keeps both live,
		// and it loads only for a slider with an output, so a bare
		// slider speaks its native value instead of a stale text.
		input["aria-valuetext"] = w.Number(value)
	}
	Flag(input, "disabled", p.Disabled)

	var output render.HTML
	if p.ShowValue {
		output = b.El("output", PartSliderOutput, Internal(html.Attrs{
			"for":                    id,
			"data-hui-slider-output": "",
		}), render.Text(w.Number(value)))
	}

	kids := []render.HTML{
		b.El("label", PartLabel, Internal(Attrs(map[string]string{"for": id})), render.Text(p.Label)),
		output,
		b.El("input", PartControl, Internal(input)),
	}
	if p.ShowEdgeLabels {
		kids = append(kids, b.El("div", PartSliderEdges, Internal(nil),
			b.El("span", PartSliderEdge, nil, render.Text(w.Number(min))),
			b.El("span", PartSliderEdge, nil, render.Text(w.Number(max))),
		))
	}

	own := Safe(p.ExtraAttrs, "role", "aria-label", "type", "name",
		"min", "max", "step", "value", "disabled", "aria-label")
	Mark(own, "data-hui-slider")
	if p.Disabled {
		own["data-state"] = "disabled"
	}
	return b.El("div", PartRoot, own, kids...)
}

// RangeSlider parts.
const (
	PartRangeLow    Part = "range-low"
	PartRangeHigh   Part = "range-high"
	PartRangeOutput Part = "range-output"
	PartRangeTrack  Part = "range-track"
)

// RangeSliderProps configures the two-thumb range pair.
type RangeSliderProps struct {
	// Name is the base form-field name; the thumbs submit as Name-min and Name-max.
	Name string
	// Label names the group and both thumbs. Required.
	Label string
	// Min, Max, Step as for Slider.
	Min  int
	Max  int
	Step int
	// ValueLow and ValueHigh are the thumbs' values; nil sits on Min or Max.
	ValueLow  *int
	ValueHigh *int
	// ShowValue renders the pair's output sentence (Strings.RangeValue).
	ShowValue bool
	Disabled  bool

	ID         string
	ExtraAttrs html.Attrs

	Parts   Parts
	Strings *Strings

	// Ctx scopes the id derived from Name when ID is empty.
	Ctx context.Context
}

// RangeSlider renders the two-thumb range pair.
func RangeSlider(p RangeSliderProps, s Classes) render.HTML {
	if p.Name == "" {
		panic("headless: RangeSlider requires Name — a control with no name submits nothing")
	}
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
	lo, hi := min, max
	if p.ValueLow != nil {
		lo = *p.ValueLow
	}
	if p.ValueHigh != nil {
		hi = *p.ValueHigh
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
	id := instanceID(p.Ctx, p.ID, p.Name, "-min", "-max")

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
		output = b.El("output", PartRangeOutput, Internal(html.Attrs{
			"for":                          id + "-min " + id + "-max",
			"data-hui-range-slider-output": w.RangeValue,
		}), render.Text(w.fill(w.RangeValue, "low", lo, "high", hi)))
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
		b.El("span", PartLabel, Internal(nil), render.Text(p.Label)),
		output,
		b.El("div", PartRangeTrack, Internal(nil),
			thumb(PartRangeLow, "min", "data-hui-range-slider-low",
				w.fill(w.RangeLow, "label", p.Label), lo),
			thumb(PartRangeHigh, "max", "data-hui-range-slider-high",
				w.fill(w.RangeHigh, "label", p.Label), hi),
		),
	)
}

// Rating parts.
const (
	PartOptionChoice Part = "option-choice"
)

// RatingProps configures a rating as a radio group.
type RatingProps struct {
	// Name is the form-field name the chosen value 1..Max submits under. Required.
	Name string
	// Label names the group. Required.
	Label string
	// Max is the ceiling. Default 5; below 1 is refused.
	Max int
	// Value is the current choice, 0 (none) to Max.
	Value int
	// Icon is the glyph in every choice; the default is a star.
	Icon     render.HTML
	Disabled bool

	ID         string
	ExtraAttrs html.Attrs

	Parts   Parts
	Strings *Strings

	// Ctx scopes the choice ids (<ID or Name>-1..-Max).
	Ctx context.Context
}

// Rating renders the rating as a radio group, choices in order 1..Max.
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
	family := make([]string, max)
	for i := range family {
		family[i] = "-" + strconv.Itoa(i+1)
	}
	base := instanceID(p.Ctx, p.ID, p.Name, family...)
	items := make([]render.HTML, 0, max*2)
	for i := 1; i <= max; i++ {
		idV := base + "-" + strconv.Itoa(i)
		radio := html.Attrs{
			"type":       "radio",
			"name":       p.Name,
			"id":         idV,
			"value":      strconv.Itoa(i),
			"aria-label": w.fill(w.RatingChoice, "n", i, "max", max),
		}
		if value == i {
			Mark(radio, "checked")
		}
		Flag(radio, "disabled", p.Disabled)
		items = append(items, b.El("input", PartControl, Internal(radio)))
		label := internalIf(p.Icon == "" || ownedSlot(p.Icon), html.Attrs{"for": idV})
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
					Step: 1, ValueLow: new(25), ValueHigh: new(40), ShowValue: true}, s),
			}, {
				Name: "equal values",
				Why:  "low equal to high is a real pair — a single-point range, rendered as given rather than repaired",
				HTML: RangeSlider(RangeSliderProps{Name: "window", Label: "Window",
					ValueLow: &five, ValueHigh: &five}, s),
			}, {
				Name: "a pair set to zero",
				Why:  "nil is the unset pair, so a set value is honoured even when it is zero — a posted 0 to 0 renders as posted instead of widening back to the whole range",
				HTML: RangeSlider(RangeSliderProps{Name: "delay", Label: "Delay",
					ValueLow: new(0), ValueHigh: new(0), ShowValue: true}, s),
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
