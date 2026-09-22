package headless

import (
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// The counter: a value between a decrement and an increment button,
// all three owned by one group. The value lives in a client signal —
// the kernel's signals module is the one increment path, through the
// data-fui-signal / data-fui-signal-inc contracts this component
// renders — so no module of this package owns the number. What this
// package adds is the animation presentation (AnimateFrom), which
// never mutates the value: the final number is SSR text, and a reader
// without script sees the true count, not a zero.

// Counter parts.
const (
	PartCounterDecrement Part = "counter-decrement"
	PartCounterValue     Part = "counter-value"
	PartCounterIncrement Part = "counter-increment"
)

// CounterProps configures a client-side counter.
type CounterProps struct {
	// Signal is the client signal the count lives in. Required: the
	// buttons' whole contract is that they change a value the page
	// already holds, and the display's contract is that the same
	// signal writes it.
	Signal string
	// Name, when set, renders the value as a named number input the
	// surrounding form submits — the no-script path. The input's
	// value follows the signal the same way the span does, so the
	// two variants are one control.
	Name string
	// Value is the count's first value, seeded into the markup.
	Value int
	// Step is the increment size. Default 1; must be positive.
	Step int
	// Label names the group for assistive technology. Empty takes
	// Strings.CounterLabel: three controls with no group name is a
	// label, a minus, a number and a plus to nobody, so the default
	// names them.
	Label string
	// AnimateFrom, when set, is the value the count animates from on
	// arrival; Value is where it lands, and Value stays the SSR text.
	// Nil means no animation. The hooks it renders are bound by the
	// headless-controls module, which respects reduced motion and
	// never owns the number.
	AnimateFrom *int
	// DurationMS bounds the animation. Zero takes the module's
	// default; negative is refused.
	DurationMS int

	ID         string
	ExtraAttrs html.Attrs

	// Parts: attrs and binds on the root and the value. Strings
	// carry the group's name and the two buttons'.
	Parts Parts
	// Strings are the strings this component says. Nil means the English
	// defaults; a layer above sets them from the request's language.
	Strings *Strings
}

// Counter renders the value between its two buttons.
func Counter(p CounterProps, s Classes) render.HTML {
	if p.Signal == "" {
		panic("headless: Counter requires Signal — the buttons change a value the page holds; without the signal they change nothing")
	}
	checkSignalName(p.Signal)
	step := p.Step
	if step == 0 {
		step = 1
	}
	if step <= 0 {
		panic("headless: Counter Step " + strconv.Itoa(p.Step) + " must be positive — a step of zero is a button that changes nothing and a step below zero swaps the buttons' meanings")
	}
	if p.DurationMS < 0 {
		panic("headless: Counter DurationMS " + strconv.Itoa(p.DurationMS) + " is negative — a countdown is not an animation duration")
	}
	w := p.Strings.Resolve()
	b := p.Parts.Box(s)
	// Trimmed: a label of spaces names nothing, so it falls to the
	// Strings default exactly like an empty one.
	label := p.Label
	if strings.TrimSpace(label) == "" {
		label = w.CounterLabel
	}
	own := Merge(Safe(p.ExtraAttrs, "role", "aria-label"), Attrs(map[string]string{
		"role":       "group",
		"aria-label": label,
		"id":         p.ID,
	}))
	if p.AnimateFrom != nil {
		// The animation is presentation: the module that binds these
		// hooks writes the value from AnimateFrom toward Value and
		// leaves the number the signal owns alone.
		Mark(own, "data-hui-counter-animate")
		own["data-hui-counter-from"] = strconv.Itoa(*p.AnimateFrom)
		if p.DurationMS > 0 {
			own["data-hui-counter-ms"] = strconv.Itoa(p.DurationMS)
		}
	}

	// The kernel's increment spelling, unchanged: name alone means
	// +1, name:n means ±n. The decrement always spells its negative —
	// "name:-0" is how a zero-step button would read.
	inc := "data-fui-signal-inc"
	dec := html.Attrs{inc: p.Signal + ":" + strconv.Itoa(-step)}
	plus := html.Attrs{inc: p.Signal}
	if step != 1 {
		plus[inc] = p.Signal + ":" + strconv.Itoa(step)
	}

	var value render.HTML
	if p.Name != "" {
		// The signal writes the input's value attribute, so a named
		// counter is still one control: the buttons, the display and
		// the submitted field hold one number.
		value = b.El("input", PartCounterValue, html.Attrs{
			"type":                 "number",
			"name":                 p.Name,
			"step":                 strconv.Itoa(step),
			"value":                strconv.Itoa(p.Value),
			"aria-label":           label,
			"data-fui-signal":      p.Signal,
			"data-fui-signal-attr": "value",
		})
	} else {
		value = b.El("span", PartCounterValue, html.Attrs{
			"aria-live":       "polite",
			"data-fui-signal": p.Signal,
		}, render.Text(strconv.Itoa(p.Value)))
	}

	return b.El("div", PartRoot, own,
		b.El("button", PartCounterDecrement, Merge(dec, Attrs(map[string]string{
			"type":       "button",
			"aria-label": w.CounterDecrement,
		})), render.Text("−")),
		value,
		b.El("button", PartCounterIncrement, Merge(plus, Attrs(map[string]string{
			"type":       "button",
			"aria-label": w.CounterIncrement,
		})), render.Text("+")),
	)
}

func init() {
	Register(Spec{
		Name:    "Counter",
		Anatomy: []Part{PartRoot, PartCounterDecrement, PartCounterValue, PartCounterIncrement},
		Hooks:   []string{"data-hui-counter-animate", "data-hui-counter-from", "data-hui-counter-ms"},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return Counter(CounterProps{Signal: "qty", Label: "Quantity", Parts: parts}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			zero := 0
			return []Case{{
				Name: "a signal counter",
				Why:  "the value is on the page before any script runs and the buttons speak the kernel's increment contract — the counter works with no module of this package loaded at all",
				HTML: Counter(CounterProps{Signal: "qty", Label: "Quantity", Value: 2}, s),
			}, {
				Name: "animated",
				Why:  "the final value is the SSR text, so a reader without script sees the true count while the animation is only presentation, and reduced motion keeps the number",
				HTML: Counter(CounterProps{Signal: "deployed", Label: "Deployed", Value: 4820,
					AnimateFrom: &zero, DurationMS: 600}, s),
			}, {
				Name: "the unlabelled group names itself",
				Why:  "a counter with no label of its own still names its group — the default word is the one Strings carries, so a translated page says it in the reader's language",
				HTML: Counter(CounterProps{Signal: "hits", Value: 7}, s),
			}, {
				Name: "named, for a form",
				Why:  "a named number input submits without script, and the signal writes its value attribute so the display and the submitted field are one number",
				HTML: Counter(CounterProps{Signal: "seats", Label: "Seats", Name: "seats", Value: 4}, s),
			}}
		},
	})
}
