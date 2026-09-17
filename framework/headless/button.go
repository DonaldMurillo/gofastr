package headless

import (
	"encoding/json"
	"strconv"
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
	// External, with Href, opens the link in a new tab with
	// rel="noopener noreferrer" and owns target and rel: a caller's
	// spelling of either — folded or not — cannot clobber the noopener
	// contract.
	External bool
	// PopoverTarget names a popover this button opens. It is also what
	// gives that popover its invoker, which is how the runtime places
	// the panel beside this button.
	PopoverTarget string
	// HasPopup, when set, is the aria-haspopup value ("menu", "dialog").
	HasPopup string

	// Action is what the button DOES when the host framework's runtime
	// is on the page: the data-fui-rpc attributes of one of its
	// actions, as its Attrs() returns them, one of its local signal
	// mutations (set, increment, toggle), or one of the wiring keys a
	// page can put on any clickable — opening a widget or pane, firing
	// a toast, writing the URL, deep-linking an open, prefetching a
	// runtime module. It is a seam of its own rather than a use of
	// ExtraAttrs, because ExtraAttrs is for what a page knows and a
	// component cannot — a test id, a title — and Safe drops every
	// data-fui-* key from it so a decoration can never become a
	// request. An action is not decoration. Naming it makes it
	// reviewable: a button that fires a request says so in its props,
	// in one place, and the type refuses anything that is not a
	// request or a wiring key.
	//
	// The request family (data-fui-rpc*, data-fui-confirm, the signal
	// mutations) needs a button: an anchor carrying one is refused at
	// render, because a link navigates and a button acts. The wiring
	// keys may ride either tag.
	Action html.Attrs

	ID         string
	ExtraAttrs html.Attrs
	// Parts is the caller's reach into this component's named parts:
	// attributes on the root (a class appends, never replaces) and on
	// the icon. The root is not fillable; the icon is caller markup
	// already.
	Parts Parts
}

// linkLegal reports whether an Action key may ride an anchor: exactly
// the wiring keys that say where a click goes — opening a widget,
// deep-linking its data, writing the URL, prefetching a module. The
// rest of the vocabulary needs a button, because a link navigates and
// a button acts: a request, a pane, a toast or a local mutation on an
// anchor is a click the href and the runtime would both answer.
func linkLegal(key string) bool {
	switch key {
	case "data-fui-open", "data-fui-push-state", "data-fui-deeplink", "data-fui-prefetch":
		return true
	}
	return false
}

// actionAttrs returns the action's attributes, refusing any that are
// not an action's. The framework's runtime reads many data-fui-*
// families; what belongs here is what a click DOES — a request, a
// local signal mutation, or one of the wiring keys core-ui/interactive
// can splice onto a clickable (open, pane open/close, toast, push
// state, deeplink, prefetch) — so a caller cannot use this seam to
// hand a button a signal binding, a pane key, or an optimistic
// lifecycle it does not render the markup for. Every key is checked
// for what it deserves: endpoints and the URLs the history API
// touches must be same-origin, values that name things must be
// non-empty, the toast payload must parse as JSON, and module names
// must keep the shape the runtime's loader checks anyway.
func actionAttrs(a html.Attrs) html.Attrs {
	out := html.Attrs{}
	for k, v := range a {
		// Attribute names are case-insensitive in HTML; fold first so
		// a caller's spelling cannot dodge a check by its case, and
		// store folded so the runtime reads one canonical name. One
		// key under two spellings is refused for the same reason Safe
		// refuses it.
		k = strings.ToLower(k)
		if _, twice := out[k]; twice {
			panic("headless: Action repeats " + k + " under two spellings")
		}
		switch k {
		case "data-fui-rpc":
			if v == "" {
				panic("headless: Action carries an empty data-fui-rpc — a request with no endpoint")
			}
			checkSameOrigin("a Button Action", "data-fui-rpc", v)
			out[k] = v
		case "data-fui-rpc-method":
			switch v {
			case "GET", "POST", "PUT", "PATCH", "DELETE":
			default:
				panic("headless: Action carries data-fui-rpc-method " + strconv.Quote(v) + ", which is not a method the runtime sends")
			}
			out[k] = v
		case "data-fui-rpc-body":
			if !json.Valid([]byte(v)) {
				panic("headless: Action carries a data-fui-rpc-body that is not JSON — the runtime sends it verbatim and the server refuses it")
			}
			out[k] = v
		case "data-fui-rpc-signal":
			checkSignalName(v)
			out[k] = v
		case "data-fui-rpc-navigate":
			if v == "" {
				panic("headless: Action carries an empty data-fui-rpc-navigate — a success with nowhere to go")
			}
			checkSameOrigin("a Button Action", "data-fui-rpc-navigate", v)
			out[k] = v
		case "data-fui-rpc-open", "data-fui-rpc-after-text", "data-fui-rpc-scroll-to", "data-fui-confirm":
			if v == "" {
				panic("headless: Action carries an empty " + k + " — it names or says something, and empty names nothing")
			}
			out[k] = v
		case "data-fui-rpc-close", "data-fui-rpc-reset", "data-fui-rpc-after-disable",
			"data-fui-intercept-close":
			// Presence is the value. intercept-close closes the
			// enclosing intercept overlay — the runtime's own family
			// (fragments.go owns it), carried the same way a page
			// carries pane-close.
			out[k] = v
		case "data-fui-signal-set", "data-fui-signal-inc", "data-fui-signal-toggle":
			// The value is "signal" or "signal:argument".
			checkSignalName(strings.SplitN(v, ":", 2)[0])
			out[k] = v
		case "data-fui-push-state":
			if v == "" {
				panic("headless: Action carries an empty data-fui-push-state — a URL write with no URL")
			}
			checkSameOrigin("a Button Action", "data-fui-push-state", v)
			out[k] = v
		case "data-fui-open", "data-fui-deeplink", "data-fui-toast":
			if v == "" {
				panic("headless: Action carries an empty " + k + " — it names what opens or fires, and empty names nothing")
			}
			if k == "data-fui-toast" && !json.Valid([]byte(v)) {
				panic("headless: Action carries a data-fui-toast that is not JSON — the runtime parses it at click time and would fail there instead")
			}
			out[k] = v
		case "data-fui-pane-open":
			if v != "secondary" && v != "tertiary" {
				panic("headless: Action carries data-fui-pane-open " + strconv.Quote(v) + ", which is not a pane a PaneHost renders")
			}
			out[k] = v
		case "data-fui-pane-close":
			// An empty value closes the topmost pane.
			if v != "" && v != "secondary" && v != "tertiary" {
				panic("headless: Action carries data-fui-pane-close " + strconv.Quote(v) + ", which is not a pane a PaneHost renders")
			}
			out[k] = v
		case "data-fui-prefetch":
			for _, name := range strings.Fields(v) {
				for i := range len(name) {
					c := name[i]
					if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' {
						continue
					}
					panic("headless: Action carries data-fui-prefetch " + strconv.Quote(name) + ", which is not a module name shape — the runtime's loader refuses it at click time")
				}
			}
			out[k] = v
		default:
			panic("headless: Action carries " + k + ", which is not a request or wiring attribute")
		}
	}
	return out
}

// Button renders the control.
func Button(p ButtonProps, s Classes) render.HTML {
	if p.Label == "" && p.AriaLabel == "" {
		panic("headless: Button needs Label, or AriaLabel for an icon-only button")
	}
	action := actionAttrs(p.Action)
	if p.Href != "" {
		for k := range action {
			if !linkLegal(k) {
				panic("headless: Button has both Href and an Action carrying " + k + " — a link navigates, a button acts; pick one")
			}
		}
	}
	own := Attrs(map[string]string{
		"id":            p.ID,
		"aria-label":    p.AriaLabel,
		"popovertarget": p.PopoverTarget,
		"aria-haspopup": p.HasPopup,
	})
	owned := []string{"type", "disabled", "href", "popovertarget"}
	if p.External {
		owned = append(owned, "target", "rel")
	}
	own = Merge(Safe(p.ExtraAttrs, owned...), own)
	own = Merge(own, action)
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

	b := p.Parts.Box(s)
	kids := make([]render.HTML, 0, 2)
	if p.Icon != "" {
		kids = append(kids, b.El("span", PartIcon,
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
			return b.El("a", PartRoot, own, kids...)
		}
		own["href"] = href
		if p.External {
			own["target"] = "_blank"
			own["rel"] = "noopener noreferrer"
		}
		return b.El("a", PartRoot, own, kids...)
	}
	own["type"] = orDefault(p.Type, "button")
	Flag(own, "disabled", p.Disabled)
	return b.El("button", PartRoot, own, kids...)
}

func orDefault(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
