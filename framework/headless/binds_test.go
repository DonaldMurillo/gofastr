package headless

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// A binding is a seam of its own because an override may not carry a
// data-fui-* key. The binding lands on the named part, on that part
// only, and renders exactly the triple the framework's runtime reads.
func TestBindLandsOnTheNamedPartOnly(t *testing.T) {
	// The header is the card's one fillable part, so it is the one
	// part a text Bind may rewrite; the body would refuse.
	got := Card(CardProps{Title: "CPU", Parts: Parts{Binds: Binds{
		PartCardHeader: {Signal: "cpu"},
	}}}, nil, render.HTML("<p>41%</p>"))
	has(t, got, `data-fui-signal="cpu"`, "the binding never arrived")
	has(t, got, `data-fui-signal-mode="text"`, "text is the default mode and must be stated for the runtime")
	if n := count(got, "data-fui-signal="); n != 1 {
		t.Errorf("the binding landed %d times; it must land on the one part that asked", n)
	}
	hasNot(t, got, "data-fui-signal-attr", "a text binding carries an attribute name")
}

func TestBindAttrModeNamesItsAttribute(t *testing.T) {
	// title, not aria-busy: an attribute Bind may name anything the
	// allow-lists allow, and aria-busy is the runtime's (the action
	// lifecycle writes it), refused below.
	got := Card(CardProps{Title: "CPU", Parts: Parts{Binds: Binds{
		PartCardBody: {Signal: "busy", Mode: "attr", Attr: "title"},
	}}}, nil, render.HTML("<p>x</p>"))
	has(t, got, `data-fui-signal-mode="attr"`, "attr mode is not stated")
	has(t, got, `data-fui-signal-attr="title"`, "the attribute to write is missing")
}

// A text Bind on a part that is not fillable would gut the part's
// content — a Password's input and reveal button — so it is refused
// at render, with the reason. The attr equivalents of the same
// ownership rule follow: an attribute the runtime rewrites as state
// moves is not a slot for a signal's value.
func TestBindRefusesWhatTheRuntimeWouldRefuse(t *testing.T) {
	for _, c := range []struct {
		name string
		bind Bind
		want string
	}{
		{"no signal", Bind{}, "needs a Signal"},
		{"reserved name", Bind{Signal: "__proto__"}, "reserved"},
		{"unknown mode", Bind{Signal: "s", Mode: "innerText"}, "must be text, html or attr"},
		{"attr without name", Bind{Signal: "s", Mode: "attr"}, "needs the Attr"},
		{"text with attr", Bind{Signal: "s", Attr: "title"}, "takes no Attr"},
		{"executable attr", Bind{Signal: "s", Mode: "attr", Attr: "onclick"}, "may not write onclick"},
		{"style attr", Bind{Signal: "s", Mode: "attr", Attr: "style"}, "may not write style"},
		{"privileged attr", Bind{Signal: "s", Mode: "attr", Attr: "data-fui-rpc"}, "may not write data-fui-rpc"},
		{"text on an unfillable part", Bind{Signal: "s"}, "not a fillable part"},
		{"html on an unfillable part", Bind{Signal: "s", Mode: "html"}, "not a fillable part"},
		{"the lifecycle's pressed", Bind{Signal: "s", Mode: "attr", Attr: "aria-pressed"}, "the runtime owns it"},
		{"the lifecycle's busy", Bind{Signal: "s", Mode: "attr", Attr: "aria-busy"}, "the runtime owns it"},
		{"the lifecycle's live region", Bind{Signal: "s", Mode: "attr", Attr: "aria-live"}, "the runtime owns it"},
		{"a field's invalid", Bind{Signal: "s", Mode: "attr", Attr: "aria-invalid"}, "the runtime owns it"},
		{"an expander's expanded", Bind{Signal: "s", Mode: "attr", Attr: "aria-expanded"}, "the runtime owns it"},
		{"a nav's current", Bind{Signal: "s", Mode: "attr", Attr: "aria-current"}, "the runtime owns it"},
		{"hidden", Bind{Signal: "s", Mode: "attr", Attr: "hidden"}, "the runtime owns it"},
		{"disabled", Bind{Signal: "s", Mode: "attr", Attr: "disabled"}, "the runtime owns it"},
		{"data-state", Bind{Signal: "s", Mode: "attr", Attr: "data-state"}, "the runtime owns it"},
		{"a forged hook", Bind{Signal: "s", Mode: "attr", Attr: "data-hui-when"}, "the runtime owns it"},
	} {
		func() {
			defer func() {
				r := recover()
				if r == nil {
					t.Errorf("%s: rendered instead of refusing", c.name)
					return
				}
				if !strings.Contains(r.(string), c.want) {
					t.Errorf("%s: refused for the wrong reason: %v", c.name, r)
				}
			}()
			// card-body is not fillable, so the text/html rows refuse
			// on the fillable rule and the attr rows on their own.
			Card(CardProps{Title: "x", Parts: Parts{Binds: Binds{PartCardBody: c.bind}}}, nil, render.HTML("<p>x</p>"))
		}()
	}
}

// Local mutations are what a click does, so they travel through the
// Action seam like a request does, and nowhere else.
func TestButtonActionAdmitsLocalSignalMutations(t *testing.T) {
	got := Button(ButtonProps{Label: "+", Action: map[string]string{"data-fui-signal-inc": "count:5"}}, nil)
	has(t, got, `data-fui-signal-inc="count:5"`, "the local mutation never arrived")
	smuggled := Button(ButtonProps{Label: "+", ExtraAttrs: map[string]string{"data-fui-signal-inc": "count"}}, nil)
	hasNot(t, smuggled, "data-fui-signal-inc", "a local mutation arrived through ExtraAttrs")
	defer func() {
		if recover() == nil {
			t.Error("a signal BINDING was accepted as an action; a binding is a seam of its own")
		}
	}()
	Button(ButtonProps{Label: "x", Action: map[string]string{"data-fui-signal": "count"}}, nil)
}
