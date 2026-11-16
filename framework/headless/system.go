package headless

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

var systemTones = map[string]bool{
	"info": true, "success": true, "warning": true, "danger": true,
}

// SystemBannerProps is a message about the system rather than the page:
// offline, a deploy in progress, maintenance, a new version.
type SystemBannerProps struct {
	// ID is the message's identity, which the runtime remembers so it is not shown twice. Required.
	ID string
	// Tone is "info" (the default), "warning", "danger" or "success"; any other is refused.
	Tone string
	// Title is the headline. Required.
	Title string
	// Text is the detail, in prose.
	Text string
	// Action is the one control the message offers.
	Action render.HTML
	// Dismiss renders the dismiss button; nil means yes, and an Offline banner must set it false.
	Dismiss *bool
	// DismissLabel names the dismiss control; empty names it after the Title.
	DismissLabel string
	// Shown renders the banner visible; it ships hidden by default.
	Shown bool
	// Icon is decoration beside the title, hidden from assistive technology.
	Icon render.HTML
	// Offline marks the built-in connection message, which the runtime shows and
	// hides; it always ships hidden.
	Offline bool

	ExtraAttrs html.Attrs

	Parts   Parts
	Strings *Strings
}

// SystemBanner renders the message: role=status, or role=alert with
// aria-live=assertive when Offline.
func SystemBanner(p SystemBannerProps, s Classes) render.HTML {
	b := p.Parts.Box(s)
	if p.ID == "" {
		panic("headless: SystemBanner requires ID — it is the message's identity, so the same message is not shown twice")
	}
	if p.Title == "" {
		panic("headless: SystemBanner requires Title")
	}
	tone := orDefault(p.Tone, "info")
	if _, known := systemTones[tone]; !known {
		panic("headless: SystemBanner unknown Tone " + tone)
	}
	word := toneWord(tone, p.Strings.Resolve())
	if p.Offline && p.Shown {
		panic("headless: SystemBanner Offline is the runtime's — it shows the banner when the connection is lost, so it cannot ship shown")
	}
	if p.Offline && (p.Dismiss == nil || *p.Dismiss) {
		panic("headless: an Offline SystemBanner carries no Dismiss — its ending is the reconnect, and a remembered dismissal would hide the next outage")
	}

	own := Merge(Safe(p.ExtraAttrs, "role", "aria-live", "hidden"), Attrs(map[string]string{
		"data-hui-system-id": p.ID,
	}))
	Mark(own, "data-hui-system")
	if p.Offline {
		own["role"] = "alert"
		own["aria-live"] = "assertive"
		Mark(own, "data-hui-system-offline", "hidden")
	} else {
		own["role"] = "status"
		Flag(own, "hidden", !p.Shown)
	}
	if cls := s.Variant(PartRoot, tone); cls != "" {
		own["class"] = cls
	}

	title := []render.HTML{
		b.El("span", PartVisuallyHidden, nil, render.Text(word+": ")),
		render.Text(p.Title),
	}
	kids := []render.HTML{}
	if p.Icon != "" {
		kids = append(kids, b.El("span", PartIcon,
			internalIf(ownedSlot(p.Icon), Attrs(map[string]string{"aria-hidden": "true"})), p.Icon))
	}
	kids = append(kids, b.El("p", PartTitle, Internal(nil), title...))
	if p.Text != "" {
		kids = append(kids, b.El("p", PartText, Internal(nil), render.Text(p.Text)))
	}
	if p.Action != "" {
		kids = append(kids, b.El("div", PartActions, internalIf(ownedSlot(p.Action), nil), p.Action))
	}
	if p.Dismiss == nil || *p.Dismiss {
		w := p.Strings.Resolve()
		dismiss := Internal(Mark(Attrs(map[string]string{
			"type":       "button",
			"aria-label": orDefault(p.DismissLabel, w.fill(w.DismissTitled, "title", p.Title)),
		}), "data-hui-system-dismiss"))
		kids = append(kids, b.El("button", PartDismiss, dismiss, render.Text("×")))
	}
	return b.El("div", PartRoot, own, kids...)
}

func init() {
	Register(Spec{
		Name: "SystemBanner",
		Anatomy: []Part{PartRoot, PartIcon, PartTitle, PartText, PartActions,
			PartDismiss, PartVisuallyHidden},
		Hooks: []string{"data-hui-system", "data-hui-system-id",
			"data-hui-system-dismiss", "data-hui-system-offline"},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return SystemBanner(SystemBannerProps{
				ID: "sys-parts", Title: "Deploy in progress", Shown: true, Parts: parts,
			}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			noDismiss := false
			return []Case{{
				Name: "deploy in progress",
				Why: "a message about the system, shown by the server's own render: role=status so it waits its turn, " +
					"one action, and the tone said in words as well as drawn in colour",
				HTML: SystemBanner(SystemBannerProps{
					ID: "sys-deploy", Shown: true,
					Icon:   SpecimenGlyph,
					Title:  "Deploy in progress",
					Text:   "blog is moving to image 41; the app stays reachable the whole time.",
					Action: Button(ButtonProps{Label: "View the deploy", Variant: "secondary"}, k.For("Button")),
				}, s),
			}, {
				Name: "offline",
				Why: "the built-in connection message, and the one system banner worth interrupting for: assertive, " +
					"marked as the runtime's, shipped hidden because the runtime — not the page — shows it, and " +
					"carrying no dismiss because its ending is the reconnect",
				HTML: SystemBanner(SystemBannerProps{
					ID: "sys-offline", Tone: "warning", Offline: true, Dismiss: &noDismiss,
					Title: "Connection lost",
					Text:  "Changes are paused until the connection comes back.",
				}, s),
			}, {
				Name: "new version",
				Why: "a dismissable message the page did not show: it ships hidden for an island swap or the runtime " +
					"to reveal, and the id is what keeps it from being shown twice",
				HTML: SystemBanner(SystemBannerProps{
					ID: "sys-version", Tone: "success",
					Title: "A new version is ready",
					Text:  "Reload to pick up version 0.4.2.",
				}, s),
			}, {
				Name: "maintenance, and the version is old",
				Why: "the danger tone said as a word before the title, and two banners at once to prove the ids keep " +
					"them from doubling up",
				HTML: group(
					SystemBanner(SystemBannerProps{
						ID: "sys-maint", Tone: "danger", Shown: true,
						Title: "Ending in 4 minutes",
						Text:  "This control plane restarts at 20:00 UTC.",
					}, s),
					SystemBanner(SystemBannerProps{
						ID: "sys-old", Tone: "info",
						Title: "This browser is not supported",
					}, s)),
			}}
		},
	})
}
