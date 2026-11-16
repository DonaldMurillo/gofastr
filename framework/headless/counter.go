package headless

import (
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Counter parts.
const (
	PartCounterDecrement Part = "counter-decrement"
	PartCounterValue     Part = "counter-value"
	PartCounterIncrement Part = "counter-increment"
)

// CounterProps configures a counter whose value lives in a client signal.
type CounterProps struct {
	// Signal is the client signal the count lives in. Required.
	Signal string
	// Name renders the value as a named number input the form submits.
	Name string
	// Value is the first value, rendered as text and, unless Seeded, handed to the signal on arrival.
	Value int
	// Seeded reports that the page seeds Signal itself, so the counter hands the runtime no value.
	Seeded bool
	// Step is the increment size. Default 1; must be positive.
	Step int
	// Label names the group; blank takes Strings.CounterLabel.
	Label string
	// AnimateFrom is the value the count animates from on arrival; nil means no animation.
	AnimateFrom *int
	// DurationMS bounds the animation; zero takes the module's default and negative is refused.
	DurationMS int
	// Display renders the value alone, a figure with no step buttons, group role or live region.
	Display bool

	ID         string
	ExtraAttrs html.Attrs

	Parts   Parts
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
	if p.Display && p.Name != "" {
		panic("headless: Counter Display with Name " + strconv.Quote(p.Name) + " — a display figure is not a form field; drop Name, or Display for a control")
	}
	w := p.Strings.Resolve()
	b := p.Parts.Box(s)
	label := p.Label
	if strings.TrimSpace(label) == "" {
		label = w.CounterLabel
	}
	own := Merge(Safe(p.ExtraAttrs, "role", "aria-label"), Attrs(map[string]string{
		"role":       "group",
		"aria-label": label,
		"id":         p.ID,
	}))
	if p.Display {
		delete(own, "role")
		delete(own, "aria-label")
	}
	if p.AnimateFrom != nil && jsSafeInt(p.Value) && jsSafeInt(*p.AnimateFrom) {
		Mark(own, "data-hui-counter-animate")
		own["data-hui-counter-from"] = strconv.Itoa(*p.AnimateFrom)
		own["data-hui-counter-to"] = strconv.Itoa(p.Value)
		if p.DurationMS > 0 {
			own["data-hui-counter-ms"] = strconv.Itoa(p.DurationMS)
		}
	}
	if !p.Display && !p.Seeded && jsSafeInt(p.Value) {
		own["data-hui-counter-value"] = strconv.Itoa(p.Value)
	}

	inc := "data-cui-signal-inc"
	dec := html.Attrs{inc: p.Signal + ":" + strconv.Itoa(-step)}
	plus := html.Attrs{inc: p.Signal}
	if step != 1 {
		plus[inc] = p.Signal + ":" + strconv.Itoa(step)
	}

	var value render.HTML
	if p.Name != "" {
		value = b.El("input", PartCounterValue, Internal(Merge(html.Attrs{
			"type":       "number",
			"name":       p.Name,
			"step":       strconv.Itoa(step),
			"value":      strconv.Itoa(p.Value),
			"aria-label": label,
		}, Bind{Signal: p.Signal, Mode: "attr", Attr: "value"}.attrs())))
	} else {
		live := html.Attrs{"aria-live": "polite", "data-cui-signal": p.Signal, "data-cui-signal-mode": "number"}
		if p.Display {
			delete(live, "aria-live")
		}
		value = b.El("span", PartCounterValue, Internal(live), render.Text(w.Number(p.Value)))
	}

	if p.Display {
		return b.El("div", PartRoot, own, value)
	}
	return b.El("div", PartRoot, own,
		b.El("button", PartCounterDecrement, Internal(Merge(dec, Attrs(map[string]string{
			"type":       "button",
			"aria-label": w.CounterDecrement,
		}))), render.Text("−")),
		value,
		b.El("button", PartCounterIncrement, Internal(Merge(plus, Attrs(map[string]string{
			"type":       "button",
			"aria-label": w.CounterIncrement,
		}))), render.Text("+")),
	)
}

func init() {
	Register(Spec{
		Name:    "Counter",
		Anatomy: []Part{PartRoot, PartCounterDecrement, PartCounterValue, PartCounterIncrement},
		Hooks:   []string{"data-hui-counter-value", "data-hui-counter-animate", "data-hui-counter-from", "data-hui-counter-to", "data-hui-counter-ms"},
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
				Name: "display only",
				Why:  "a figure that animates in is read, not operated: no step buttons, no group role, and no live region to read out each frame of the tick-up; the page around it names the number",
				HTML: Counter(CounterProps{Signal: "signups", Value: 12483,
					AnimateFrom: &zero, Display: true}, s),
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

func jsSafeInt(n int) bool {
	const max = 1<<53 - 1
	return int64(n) >= -max && int64(n) <= max // int64: a 32-bit int cannot hold max
}
