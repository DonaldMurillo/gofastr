package headless

import (
	"fmt"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// systemTones are the tones a banner may be drawn in; the tone drives
// both the class map's root--<tone> class and the word said before the
// title.
var systemTones = map[string]bool{
	"info": true, "success": true, "warning": true, "danger": true,
}

// systemToneWord is the tone said before the title, from Strings: unlike an alert, whose title may already say which kind it is
// ("Deploy failed"), a system banner's title says WHAT is true of the
// system and the tone is the only thing saying how serious it is.
func systemToneWord(w *Strings, tone string) string {
	switch tone {
	case "success":
		return w.ToneSuccess
	case "warning":
		return w.ToneWarning
	case "danger":
		return w.ToneDanger
	default:
		return w.ToneInfo
	}
}

// SystemBannerProps is a message about the system rather than the
// page: offline, a deploy in progress, maintenance, a new version.
//
// The difference from an Alert is who owns it. An alert is part of a
// page's answer; a system banner is placed once at the top of the
// shell, above every page, and what it says is true of the system the
// reader is standing in. That is also why it ships hidden: the slot is
// always there, and a message arrives in it — the runtime shows the
// offline one when the framework reports the connection lost, and
// anything else is a server render or an island swap into the slot.
type SystemBannerProps struct {
	// ID is the message's identity, not just the element's: the
	// runtime remembers it so the same message is not shown twice.
	// Required.
	ID string
	// Tone is "info" (the default), "warning", "danger" or
	// "success". The class map looks it up as root--<tone>, and the word
	// a screen reader hears is derived from it, so a tone nobody
	// spelled is refused rather than silently rendered untinted.
	Tone string
	// Title is the headline. Required: "Connection lost" is the
	// message; the detail is detail.
	Title string
	// Text is the detail, in prose.
	Text string
	// Action is the one control the message offers — "Retry now",
	// "View the deploy". One, because a system banner is a bar
	// across the top of the page, not a form.
	Action render.HTML
	// Dismiss renders the dismiss button. Nil means yes: a system
	// message the reader cannot send away is furniture that outstays
	// its news, and the cases that want it gone — the offline
	// banner, whose ending is the reconnect — say so explicitly:
	// Dismiss on an Offline banner is refused at render.
	Dismiss *bool
	// DismissLabel names the dismiss control. Defaults to
	// "Dismiss: <Title>", because three banners each called
	// "Dismiss" say which nothing.
	DismissLabel string
	// Shown renders the banner visible. The default is hidden: the
	// banner ships hidden and something shows it.
	Shown bool
	// Offline marks this banner as the built-in connection message.
	// The root carries data-hui-system-offline for the module that
	// binds it to show when the framework reports the connection lost
	// and hide on reconnect — so it always ships hidden, and Shown on
	// an Offline banner is refused: that module owns it.
	Offline bool

	ExtraAttrs html.Attrs

	// Parts: attrs only. Nothing here is fillable — Action
	// already takes the page's own control, and everything else a
	// banner draws is what a screen reader is given to tell one
	// message from another.
	Parts Parts
	// Strings are the strings this component says. Nil means the English
	// defaults; a layer above sets them from the request's language.
	Strings *Strings
}

// SystemBanner renders the message.
//
// role="status" rather than alert: a system message is important and
// must not interrupt. It is on screen at the top of the shell, and a
// polite region is read at the next pause — announcing itself over
// whatever the reader was doing would make the top of every page a
// shout. The offline banner is the one exception, and it is the
// exception because losing the connection is the one system message
// worth interrupting for: everything the reader does next will fail
// until it is back. It carries role="alert" and aria-live="assertive"
// both, as the framework banner it replaces did, so either attribute
// alone still says how urgent it is.
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
	word := systemToneWord(p.Strings.Resolve(), tone)
	if p.Offline && p.Shown {
		panic("headless: SystemBanner Offline is the runtime's — it shows the banner when the connection is lost, so it cannot ship shown")
	}
	// The offline banner's ending is the reconnect, so it carries no
	// dismiss: a dismissal remembered for the session would hide the
	// next outage too, and the module skips the dismissed set for it
	// precisely because there is nothing to dismiss.
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

	// The tone in words, read before the title so the kind of message
	// arrives before the message. The trailing space is inside the
	// hidden span: without it screen readers run the two together
	// ("WarningConnection lost"). Same shape as Alert's tone word,
	// derived here because the tone is the banner's own prop.
	title := []render.HTML{
		// Through the Box: the tone word is the PartVisuallyHidden the
		// spec declares, and attrs or binds a caller sets on it land
		// here or they land nowhere.
		b.El("span", PartVisuallyHidden, nil, render.Text(word+": ")),
		render.Text(p.Title),
	}
	kids := []render.HTML{b.El("p", PartTitle, nil, title...)}
	if p.Text != "" {
		kids = append(kids, b.El("p", PartText, nil, render.Text(p.Text)))
	}
	if p.Action != "" {
		kids = append(kids, b.El("div", PartActions, nil, p.Action))
	}
	if p.Dismiss == nil || *p.Dismiss {
		dismiss := Mark(Attrs(map[string]string{
			"type":       "button",
			"aria-label": orDefault(p.DismissLabel, fmt.Sprintf(p.Strings.Resolve().DismissTitled, p.Title)),
		}), "data-hui-system-dismiss")
		kids = append(kids, b.El("button", PartDismiss, dismiss, render.Text("×")))
	}
	return b.El("div", PartRoot, own, kids...)
}

func init() {
	Register(Spec{
		Name: "SystemBanner",
		Anatomy: []Part{PartRoot, PartTitle, PartText, PartActions,
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
