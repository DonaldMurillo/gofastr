package headless

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ButtonProps is a button, or an anchor that looks like one.
type ButtonProps struct {
	// Label is the visible text. An icon-only button leaves it empty
	// and sets AriaLabel; a button with neither has no accessible name
	// and is refused.
	Label     string
	AriaLabel string
	Icon      render.HTML
	// Suffix renders after the label — a keyboard hint, a count. It is
	// the caller's markup, so whether it is announced is the caller's
	// decision: a keyboard hint hides its glyphs and supplies words, a
	// badge announces itself.
	Suffix render.HTML
	// Variant and Size are class-map vocabulary, passed through so the class map
	// can look up "<part>--<variant>". The structure does not care.
	Variant string
	Size    string
	// Disabled is a real state here. A disabled anchor is not a thing
	// in HTML, so Href + Disabled drops the href and says so with
	// aria-disabled rather than leaving a live link that looks dead.
	Disabled bool
	Type     string
	Href     string
	// PopoverTarget names a popover this button opens. It is also what
	// gives that popover its invoker, which is how the runtime places
	// the panel beside this button.
	PopoverTarget string
	// HasPopup, when set, is the aria-haspopup value ("menu", "dialog").
	HasPopup string

	// Action is what the button DOES when the host framework's runtime
	// is on the page: the data-fui-rpc attributes of one of its
	// actions, as its Attrs() returns them, or one of its local signal
	// mutations (set, increment, toggle) that never leave the browser. It is a seam of its own
	// rather than a use of ExtraAttrs, because ExtraAttrs is for what
	// a page knows and a component cannot — a test id, a title — and
	// Safe drops every data-fui-* key from it so a decoration can never
	// become a request. An action is not decoration. Naming it makes
	// it reviewable: a button that fires a request says so in its
	// props, in one place, and the type refuses anything that is not a
	// request.
	Action html.Attrs

	ID         string
	ExtraAttrs html.Attrs
}

// actionAttrs returns the action's attributes, refusing any that are
// not an action's. The framework's runtime reads many data-fui-*
// families; only what a click DOES belongs here — a request, or a
// local signal mutation — so a caller cannot use this seam to hand a
// button a signal binding, a pane key, or an optimistic lifecycle it
// does not render the markup for.
func actionAttrs(a html.Attrs) html.Attrs {
	out := html.Attrs{}
	for k, v := range a {
		switch {
		case k == "data-fui-rpc":
			if v == "" {
				panic("headless: Action carries an empty data-fui-rpc — a request with no endpoint")
			}
			checkSameOrigin("a Button Action", "data-fui-rpc", v)
			out[k] = v
		case k == "data-fui-rpc-signal":
			checkSignalName(v)
			out[k] = v
		case k == "data-fui-signal-set", k == "data-fui-signal-inc", k == "data-fui-signal-toggle":
			// The value is "signal" or "signal:argument".
			checkSignalName(strings.SplitN(v, ":", 2)[0])
			out[k] = v
		case strings.HasPrefix(k, "data-fui-rpc"),
			k == "data-fui-confirm",
			k == "data-fui-push-state":
			out[k] = v
		default:
			panic("headless: Action carries " + k + ", which is not a request attribute")
		}
	}
	return out
}

// Button renders the control.
func Button(p ButtonProps, s Classes) render.HTML {
	if p.Label == "" && p.AriaLabel == "" {
		panic("headless: Button needs Label, or AriaLabel for an icon-only button")
	}
	if len(p.Action) > 0 && p.Href != "" {
		panic("headless: Button has both Href and Action — a link navigates, a button acts; pick one")
	}
	own := Attrs(map[string]string{
		"id":            p.ID,
		"aria-label":    p.AriaLabel,
		"popovertarget": p.PopoverTarget,
		"aria-haspopup": p.HasPopup,
	})
	own = Merge(Safe(p.ExtraAttrs, "type", "disabled", "href", "popovertarget"), own)
	own = Merge(own, actionAttrs(p.Action))
	// A control that opens something says so, and says whether it is
	// open right now. The runtime keeps it true from then on; this is
	// the starting value, and without it the first state a screen
	// reader reads is no state at all.
	if p.PopoverTarget != "" && p.HasPopup != "" {
		own["aria-expanded"] = "false"
	}
	if cls := s.Variant(PartRoot, p.Variant); cls != "" {
		own["class"] = cls
	}
	if cls := s.Variant(PartRoot, p.Size); cls != "" {
		own["class"] = joinClasses(own["class"], cls)
	}
	if p.Label == "" {
		if cls := s.Variant(PartRoot, "icon"); cls != "" {
			own["class"] = joinClasses(own["class"], cls)
		}
	}

	kids := make([]render.HTML, 0, 2)
	if p.Icon != "" {
		kids = append(kids, El("span", s, PartIcon,
			Attrs(map[string]string{"aria-hidden": "true"}), p.Icon))
	}
	if p.Label != "" {
		kids = append(kids, render.Text(p.Label))
	}
	if p.Suffix != "" {
		kids = append(kids, p.Suffix)
	}

	if p.Href != "" {
		// The href goes through the framework's one anchor policy:
		// http(s), relative, fragment, mailto and tel pass; javascript:
		// and data: do not, and neither does a protocol-relative or
		// backslash spelling. A rejected href renders the same posture
		// as a disabled link — named, visible, out of the tab order —
		// rather than a link that runs something.
		href := urlsafe.CleanAnchor(p.Href)
		if p.Disabled || href == "" {
			own["aria-disabled"] = "true"
			own["tabindex"] = "-1"
			own["role"] = "link"
			return El("a", s, PartRoot, own, kids...)
		}
		own["href"] = href
		return El("a", s, PartRoot, own, kids...)
	}
	own["type"] = orDefault(p.Type, "button")
	Flag(own, "disabled", p.Disabled)
	return El("button", s, PartRoot, own, kids...)
}

func orDefault(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
