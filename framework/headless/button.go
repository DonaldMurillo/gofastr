package headless

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/internal/uiaction"
)

// ButtonProps is a button, or an anchor that looks like one.
type ButtonProps struct {
	// Label is the visible text; an icon-only button leaves it empty and sets AriaLabel.
	Label     string
	AriaLabel string
	Icon      render.HTML
	// Suffix is caller markup after the label, such as a keyboard hint or a count.
	Suffix  render.HTML
	Variant string
	Size    string
	// Disabled with Href drops the href and renders aria-disabled instead.
	Disabled bool
	Type     string
	Href     string
	// External, with Href, opens a new tab with rel="noopener noreferrer" and owns target and rel.
	External bool
	// PopoverTarget names the popover this button opens.
	PopoverTarget string
	// HasPopup, when set, is the aria-haspopup value ("menu", "dialog").
	HasPopup string

	// Action is the request or wiring attributes a click performs (data-cui-rpc*,
	// signal mutations, open, toast, pane keys); an anchor takes only the wiring keys.
	Action html.Attrs

	ID         string
	ExtraAttrs html.Attrs
	// Parts sets attrs on the root (a class appends) and on the icon.
	Parts Parts
}

func linkLegal(key string) bool {
	switch key {
	case "data-cui-open", "data-cui-push-state", "data-cui-deeplink", "data-cui-prefetch",
		"data-cui-intercept-page", "data-cui-intercept-swap", "data-cui-intercept-panel",
		"data-cui-nav":
		return true
	}
	return false
}

func actionAttrs(a html.Attrs) html.Attrs {
	out := rpcAttrs("Action", "a Button Action", a, func(out html.Attrs, k, v string) {
		switch k {
		case "data-cui-rpc-body":
			if !json.Valid([]byte(v)) {
				panic("headless: Action carries a data-cui-rpc-body that is not JSON — the runtime sends it verbatim and the server refuses it")
			}
			out[k] = v
		case "data-cui-rpc-signal":
			checkSignalName(v)
			out[k] = v
		case "data-cui-rpc-open", "data-cui-rpc-after-text", "data-cui-rpc-scroll-to",
			"data-cui-rpc-refresh", "data-cui-confirm":
			if v == "" {
				panic("headless: Action carries an empty " + k + " — it names or says something, and empty names nothing")
			}
			out[k] = v
		case "data-cui-rpc-error-toast":
			out[k] = v
		case "data-cui-rpc-success-toast":
			out[k] = v
		case "data-cui-rpc-after-disable", "data-cui-intercept-close", "data-cui-intercept-page",
			"data-cui-intercept-swap", "data-cui-intercept-panel":
			out[k] = v
		case "data-cui-nav":
			if v != "off" {
				panic("headless: Action carries data-cui-nav " + strconv.Quote(v) + ", and off is the only value the runtime reads")
			}
			out[k] = v
		case "data-cui-action":
			if v != "close" {
				panic("headless: Action carries data-cui-action " + strconv.Quote(v) + ", and close is the only action the widget runtime reads")
			}
			out[k] = v
		case "data-cui-signal-set", "data-cui-signal-inc", "data-cui-signal-toggle":
			checkSignalName(strings.SplitN(v, ":", 2)[0])
			out[k] = v
		case "data-cui-push-state":
			if v == "" {
				panic("headless: Action carries an empty data-cui-push-state — a URL write with no URL")
			}
			checkSameOrigin("a Button Action", "data-cui-push-state", v)
			out[k] = v
		case "data-cui-open", "data-cui-deeplink", "data-cui-toast":
			if v == "" {
				panic("headless: Action carries an empty " + k + " — it names what opens or fires, and empty names nothing")
			}
			if k == "data-cui-toast" && !json.Valid([]byte(v)) {
				panic("headless: Action carries a data-cui-toast that is not JSON — the runtime parses it at click time and would fail there instead")
			}
			out[k] = v
		case "data-hui-pane-open-control":
			if v != "secondary" && v != "tertiary" {
				panic("headless: Action carries data-hui-pane-open-control " + strconv.Quote(v) + ", which is not a pane a PaneHost renders")
			}
			out[k] = v
		case "data-hui-pane-key":
			if v == "" {
				panic("headless: Action carries an empty data-hui-pane-key — it names the record a keyed pane deep-links, and empty names nothing")
			}
			out[k] = v
		case "data-hui-pane-close":
			if v != "" && v != "secondary" && v != "tertiary" {
				panic("headless: Action carries data-hui-pane-close " + strconv.Quote(v) + ", which is not a pane a PaneHost renders")
			}
			out[k] = v
		case "data-hui-pane-host-target":
			if v == "" {
				panic("headless: Action carries an empty data-hui-pane-host-target — it names the host the trigger drives, and empty names nothing")
			}
			checkNoControlBytes("a Button Action", k, v)
			out[k] = v
		case "data-cui-confirm-part":
			if v != "accept" && v != "accept-danger" && v != "cancel" {
				panic("headless: Action carries data-cui-confirm-part " + strconv.Quote(v) + ", and a confirm dialog's buttons are accept, accept-danger and cancel")
			}
			out[k] = v
		case "data-cui-prefetch":
			for _, name := range strings.Fields(v) {
				for i := range len(name) {
					c := name[i]
					if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' {
						continue
					}
					panic("headless: Action carries data-cui-prefetch " + strconv.Quote(name) + ", which is not a module name shape — the runtime's loader refuses it at click time")
				}
			}
			out[k] = v
		default:
			panic("headless: Action carries " + k + ", which is not a request or wiring attribute")
		}
	})
	if _, close := out["data-cui-action"]; close {
		if _, rpc := out["data-cui-rpc"]; rpc {
			panic("headless: Action carries data-cui-action=\"close\" beside data-cui-rpc — the runtime fires the request and never reaches the close; a close after a request is data-cui-rpc-close")
		}
	}
	checkToastActionRides("Action", out)
	return out
}

// Button renders the control.
func Button(p ButtonProps, s Classes) render.HTML {
	if p.Label == "" && p.AriaLabel == "" {
		panic("headless: Button needs Label, or AriaLabel for an icon-only button")
	}
	action := actionAttrs(p.Action)
	for _, k := range []string{"data-cui-intercept-page", "data-cui-intercept-swap", "data-cui-intercept-panel"} {
		if _, ok := action[k]; ok && p.Href == "" {
			panic("headless: Button carries " + k + " with no Href — it acts on a link's target, and a button has no target")
		}
	}
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
		href := urlsafe.CleanAnchor(p.Href)
		if p.Disabled || href == "" {
			for k := range action {
				delete(own, k)
			}
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

func hasFold(a html.Attrs, key string) bool {
	for k := range a {
		if strings.EqualFold(k, key) {
			return true
		}
	}
	return false
}

// rpcAttrs folds a's keys to lower case, refuses one key under two
// spellings and validates the arms a Button Action and a Form Request
// share, writing them to out; a key only one seam admits goes through
// perSeam. seam names the carrier in panics ("Action", "Form Request"),
// component the owner checkSameOrigin names ("a Button Action",
// "a Form Request").
func rpcAttrs(seam, component string, a html.Attrs, perSeam func(out html.Attrs, k, v string)) html.Attrs {
	out := html.Attrs{}
	for k, v := range a {
		k = strings.ToLower(k)
		if _, twice := out[k]; twice {
			panic("headless: " + seam + " repeats " + k + " under two spellings")
		}
		switch k {
		case "data-cui-rpc":
			if v == "" {
				panic("headless: " + seam + " carries an empty data-cui-rpc — a request with no endpoint")
			}
			checkSameOrigin(component, "data-cui-rpc", v)
			out[k] = v
		case "data-cui-rpc-method":
			switch v {
			case "GET", "POST", "PUT", "PATCH", "DELETE":
			default:
				panic("headless: " + seam + " carries data-cui-rpc-method " + strconv.Quote(v) + ", which is not a method the runtime sends")
			}
			out[k] = v
		case "data-cui-rpc-close", "data-cui-rpc-reset":
			out[k] = v
		case "data-cui-rpc-navigate":
			if v == "" {
				panic("headless: " + seam + " carries an empty data-cui-rpc-navigate — a success with nowhere to go")
			}
			checkSameOrigin(component, "data-cui-rpc-navigate", v)
			out[k] = v
		case "data-cui-rpc-success-action":
			checkToastAction(seam, v)
			out[k] = v
		case "data-cui-confirm-title", "data-cui-confirm-accept", "data-cui-confirm-tone":
			checkConfirmWording(seam, a, k, v)
			out[k] = v
		default:
			perSeam(out, k, v)
		}
	}
	return out
}

func checkToastAction(seam, v string) {
	var a uiaction.ToastAction
	if err := json.Unmarshal([]byte(v), &a); err != nil {
		panic("headless: " + seam + " carries an invalid data-cui-rpc-success-action")
	}
	if err := a.Validate(); err != nil {
		panic("headless: " + seam + " data-cui-rpc-success-action: " + err.Error())
	}
	actionAttrs(html.Attrs(a.Attrs))
}

func checkToastActionRides(seam string, out html.Attrs) {
	if uiaction.MissingSuccessToast(out) {
		panic("headless: " + seam + " carries data-cui-rpc-success-action with no data-cui-rpc-success-toast — the action rides the success toast")
	}
}

func checkConfirmWording(seam string, a html.Attrs, k, v string) {
	switch {
	case k == "data-cui-confirm-tone" && v != "danger":
		panic("headless: " + seam + " carries data-cui-confirm-tone " + strconv.Quote(v) + ", and danger is the only tone the confirm dialog draws")
	case v == "":
		panic("headless: " + seam + " carries an empty " + k + " — it words the confirm dialog, and empty words nothing")
	case !hasFold(a, "data-cui-confirm"):
		panic("headless: " + seam + " carries " + k + " with no data-cui-confirm — it words a confirmation nothing asks")
	}
}
