package headless

import (
	"fmt"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Alert parts.
const (
	PartToneWord Part = "tone-word"
	PartDismiss  Part = "dismiss"
)

// Live says how — and whether — an alert interrupts.
//
// This is the decision the component exists to force, because getting
// it wrong is not a small mistake in either direction.
//
//   - LiveOff is the default and the right answer for anything the
//     server rendered as part of the page. An alert that was already
//     on screen when the page loaded has not "happened"; announcing it
//     as an event means a screen reader reads page furniture over the
//     top of whatever the user was doing, on every single page load.
//   - LivePolite queues the message until the user pauses. It is for
//     something that arrived after load and can wait: a background job
//     finished, a value saved.
//   - LiveAssertive interrupts immediately. It is for something the
//     user must hear before they do anything else: the deploy failed,
//     the form did not submit, the connection dropped. Nothing routine
//     belongs here — an assertive region that fires often is one a
//     user learns to resent and cannot turn off.
//
// role="alert" already implies assertive and role="status" already
// implies polite, so only the role is written. Stating both is how a
// message ends up announced twice.
type Live string

const (
	LiveOff       Live = ""
	LivePolite    Live = "polite"
	LiveAssertive Live = "assertive"
)

// AlertProps is a message about something that happened, or is true.
type AlertProps struct {
	// Tone names the kind of message: the class map turns it into colour
	// through the root's "<part>--<tone>" variant. It is passed
	// through, not interpreted; the tone word is what carries it to a
	// reader.
	Tone string
	// ToneWord is the tone in words — "Error", "Warning", "Success".
	// It is rendered for assistive tech and hidden visually, and it is
	// the reason this component satisfies WCAG 1.4.1: colour may not
	// be the only thing carrying meaning, and a red box is exactly
	// that to anyone who cannot see the red.
	//
	// Empty means the Title already says which kind it is — "Deploy
	// failed" needs no "Error:" in front of it — so the word is
	// skipped rather than duplicated.
	ToneWord string
	// Title is the headline. Required: an alert with no headline is a
	// coloured paragraph.
	Title string
	// Text is the detail, in prose.
	Text string
	// Icon is decorative. The tone word carries the meaning.
	Icon render.HTML
	// Actions are the controls: retry, view logs, dismiss.
	Actions render.HTML
	// Live says whether this interrupts. See Live.
	Live Live
	// Focus moves focus here when the page loads.
	//
	// It is how a message that is ALREADY THERE gets announced. A live
	// region announces changes; content present when the document
	// loads is not a change, and role="alert" at load time is reported
	// inconsistently across screen readers. After a form posts and the
	// server redirects — the shape of every action in a
	// server-rendered app — the confirmation is present at load, so
	// the only reliable way to say it is to put the reader on it.
	Focus bool
	// DismissHref makes the alert dismissable with a link that keeps a
	// real href, so dismissing needs no script and survives the page
	// being reloaded. The label names what is being dismissed, because
	// "Dismiss" three times in a row tells a screen reader user
	// nothing.
	DismissHref  string
	DismissLabel string
	// Island is where the dismiss goes with script: dismissing is an
	// in-page state change, so the × carries the RPC contract beside
	// its href. Required when DismissHref is set; ignored otherwise.
	Island Island

	ID         string
	ExtraAttrs html.Attrs

	// Parts: attrs and binds on every part. Nothing here is
	// fillable — Actions already takes the page's own controls, and
	// everything else an alert draws is what a screen reader is given
	// to tell one alert from another.
	Parts Parts
	// Strings are the strings this component says. Nil means the English
	// defaults; a layer above sets them from the request's language.
	Strings *Strings
}

// Alert renders the message.
func Alert(p AlertProps, s Classes) render.HTML {
	b := p.Parts.Box(s)
	if p.Title == "" {
		panic("headless: Alert requires Title")
	}
	own := Merge(Safe(p.ExtraAttrs, "role"), Attrs(map[string]string{"id": p.ID}))
	if p.Focus {
		// Focusable without joining the tab order: focus is put here
		// once, and Tab from it carries on into the page.
		own["tabindex"] = "-1"
		Mark(own, "autofocus")
	}
	switch p.Live {
	case LiveAssertive:
		own["role"] = "alert"
	case LivePolite:
		own["role"] = "status"
	}
	if cls := s.Variant(PartRoot, p.Tone); cls != "" {
		own["class"] = cls
	}

	head := make([]render.HTML, 0, 3)
	if p.Icon != "" {
		head = append(head, b.El("span", PartIcon,
			Attrs(map[string]string{"aria-hidden": "true"}), p.Icon))
	}
	title := make([]render.HTML, 0, 2)
	if p.ToneWord != "" {
		// Read before the title, so the kind of message arrives before
		// the message. The trailing space is inside the hidden span:
		// without it screen readers run the two together ("ErrorDeploy
		// failed").
		title = append(title, b.El("span", PartToneWord, nil, render.Text(p.ToneWord+": ")))
	}
	title = append(title, render.Text(p.Title))
	head = append(head, b.El("p", PartTitle, nil, title...))

	kids := []render.HTML{b.El("div", PartHeader, nil, head...)}
	if p.Text != "" {
		kids = append(kids, b.El("p", PartDesc, nil, render.Text(p.Text)))
	}
	if p.Actions != "" {
		kids = append(kids, b.El("div", PartFooter, nil, p.Actions))
	}
	if p.DismissHref != "" {
		requireIsland("Alert with DismissHref", p.Island)
		if urlsafe.CleanAnchor(p.DismissHref) == "" {
			panic("headless: Alert DismissHref " + strconv.Quote(p.DismissHref) + " is not a URL the anchor policy allows")
		}
		label := p.DismissLabel
		if label == "" {
			label = fmt.Sprintf(p.Strings.Resolve().DismissTitled, p.Title)
		}
		// The same element is both destinations: the href is the page
		// without script, the island contract is the region update
		// with it.
		dismiss := Merge(Attrs(map[string]string{"href": p.DismissHref, "aria-label": label}),
			p.Island.attrs(p.DismissHref, "GET"))
		kids = append(kids, b.El("a", PartDismiss, dismiss, render.Text("×")))
	}
	return b.El("div", PartRoot, own, kids...)
}

func init() {
	Register(Spec{
		Name:    "Alert",
		Anatomy: []Part{PartRoot, PartHeader, PartIcon, PartToneWord, PartTitle, PartDesc, PartFooter, PartDismiss},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return Alert(AlertProps{Title: "Deploy failed", Text: "Exit 1 in the test stage.", Tone: "danger",
				ToneWord: "Error", Icon: SpecimenGlyph, Actions: render.HTML("<a href=\"/logs\">View logs</a>"),
				DismissHref: "/apps?dismiss=1", Island: Island{Endpoint: "/island/alerts", Signal: "alerts"},
				Parts: parts}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "loud",
				Why:  "something went wrong and the reader must be interrupted: assertive, with the tone said in words as well as drawn in colour",
				HTML: Alert(AlertProps{
					Title: "Deploy failed", Text: "The build exited 1 after 42 seconds.",
					ToneWord: "Error", Live: LiveAssertive, Icon: SpecimenGlyph,
					Actions:     Button(ButtonProps{Label: "View logs", Variant: "secondary"}, k.For("Button")),
					DismissHref: "/apps?dismiss=1", DismissLabel: "Dismiss deploy failure",
					Island: Island{Endpoint: "/island/alerts", Signal: "alerts"},
				}, k.Variant("Alert", "danger")),
			}, {
				Name: "quiet",
				Why:  "the default: a standing message that is on screen anyway and must not interrupt whatever is being read",
				HTML: Alert(AlertProps{Title: "3 apps need attention"}, s),
			}, {
				Name: "already there",
				Why:  "the confirmation after a post-and-redirect — present at load, so the only reliable way to announce it is to put the reader on it",
				HTML: Alert(AlertProps{Title: "App restarted", Focus: true, ID: "restarted"},
					k.Variant("Alert", "success")),
			}}
		},
	})

	Register(Spec{
		Name:    "Button",
		Anatomy: []Part{PartRoot, PartIcon},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "labelled",
				Why:  "the ordinary case, and the reason type is always stated: inside a form the default is submit",
				HTML: Button(ButtonProps{Label: "Save changes", Variant: "primary"}, s),
			}, {
				Name: "icon only",
				Why:  "a button with no text still needs a name, or it cannot be operated by anyone who cannot see it",
				HTML: Button(ButtonProps{AriaLabel: "Close", Icon: SpecimenGlyph, Variant: "ghost"}, s),
			}, {
				Name: "link that looks like a button",
				Why:  "it navigates, so it is an anchor — a button that changes the URL is a button a middle click cannot open",
				HTML: Button(ButtonProps{Label: "Read the docs", Variant: "primary", Href: "/docs"}, s),
			}, {
				Name: "disabled link",
				Why:  "a disabled anchor is not a thing in HTML, so the href goes and aria-disabled says why, rather than leaving a live link that looks dead",
				HTML: Button(ButtonProps{Label: "Rollback", Variant: "danger", Href: "/rollback", Disabled: true}, s),
			}, {
				Name: "firing a request",
				Why: "the request a click makes is a prop on the button that fires it, in one reviewable place: " +
					"the rpc triple lands through the Action seam and cannot arrive any other way, so a button " +
					"that fires a request says so in its props",
				HTML: Button(ButtonProps{
					Label: "Restart api", Variant: "secondary",
					Action: html.Attrs{
						"data-fui-rpc":        "/apps/api/restart",
						"data-fui-rpc-method": "POST",
						"data-fui-rpc-signal": "apps",
					},
				}, s),
			}, {
				Name: "mutating a local signal",
				Why: "a click that never leaves the browser is still what the button DOES, so it travels the same " +
					"seam as a request: the mutation's attribute sits on the button beside its label, where a " +
					"reviewer reads them together",
				HTML: Button(ButtonProps{
					Label: "Add replica", Variant: "ghost",
					Action: html.Attrs{"data-fui-signal-inc": "replicas"},
				}, s),
			}}
		},
	})
}
