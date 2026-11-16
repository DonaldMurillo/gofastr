package headless

import (
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

// Live says whether an alert interrupts: LiveOff (the default, for what the
// page rendered), LivePolite (role=status) or LiveAssertive (role=alert).
type Live string

const (
	LiveOff       Live = ""
	LivePolite    Live = "polite"
	LiveAssertive Live = "assertive"
)

// AlertProps is a message about something that happened, or is true.
type AlertProps struct {
	// Tone names the kind of message; the class map colours the root's "root--<tone>" variant.
	Tone string
	// ToneWord is the tone in words ("Error"), read before the title and hidden
	// visually; empty means the Title already says it.
	ToneWord string
	// Title is the headline.
	Title string
	// Text is the detail, in prose.
	Text string
	// Body is the detail as markup, rendered after Text.
	Body render.HTML
	// Icon is decorative.
	Icon render.HTML
	// Actions are the controls: retry, view logs.
	Actions render.HTML
	// Live says whether this interrupts.
	Live Live
	// Focus moves focus here when the page loads, which is how a message present at load is announced.
	Focus bool
	// DismissHref makes the alert dismissable through a link with a real href.
	// DismissLabel names the control; empty names it after the Title, else the Text.
	DismissHref  string
	DismissLabel string
	// Island is where the dismiss goes with script. Required when DismissHref is set.
	Island Island

	ID         string
	ExtraAttrs html.Attrs

	Parts   Parts
	Strings *Strings
}

// Alert renders the message.
func Alert(p AlertProps, s Classes) render.HTML {
	b := p.Parts.Box(s)
	if p.Title == "" && p.Text == "" && p.Body == "" {
		panic("headless: Alert requires Title, Text or Body — a coloured box that says nothing is decoration pretending to be a message")
	}
	own := Merge(Safe(p.ExtraAttrs, "role"), Attrs(map[string]string{"id": p.ID}))
	if p.Focus {
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

	headOwn, titleOwn := html.Attrs(nil), Internal(nil)
	if p.Icon == "" {
		headOwn, titleOwn = Internal(nil), nil
	}
	head := make([]render.HTML, 0, 3)
	if p.Icon != "" {
		head = append(head, b.El("span", PartIcon,
			Attrs(map[string]string{"aria-hidden": "true"}), p.Icon))
	}
	title := make([]render.HTML, 0, 2)
	if p.ToneWord != "" {
		title = append(title, b.El("span", PartToneWord, nil, render.Text(p.ToneWord+": ")))
	}
	if p.Title != "" || p.ToneWord != "" {
		title = append(title, render.Text(p.Title))
		head = append(head, b.El("p", PartTitle, titleOwn, title...))
	}

	kids := []render.HTML{b.El("div", PartHeader, headOwn, head...)}
	if p.Text != "" {
		kids = append(kids, b.El("p", PartDesc, Internal(nil), render.Text(p.Text)))
	}
	if p.Body != "" {
		kids = append(kids, b.El("div", PartBody, nil, p.Body))
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
			target := orDefault(p.Title, p.Text)
			if target == "" {
				panic("headless: Alert with DismissHref and no Title or Text requires DismissLabel — the dismiss control would be named after nothing")
			}
			w := p.Strings.Resolve()
			label = w.fill(w.DismissTitled, "title", target)
		}
		dismiss := Merge(Attrs(map[string]string{"href": p.DismissHref, "aria-label": label}),
			p.Island.attrs(p.DismissHref, "GET"))
		kids = append(kids, b.El("a", PartDismiss, Internal(dismiss), render.Text("×")))
	}
	return b.El("div", PartRoot, own, kids...)
}

func init() {
	Register(Spec{
		Name:    "Alert",
		Anatomy: []Part{PartRoot, PartHeader, PartIcon, PartToneWord, PartTitle, PartDesc, PartBody, PartFooter, PartDismiss},
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
			}, {
				Name: "with a body",
				Why:  "the detail as markup: a persistent callout's contents are a list or a sample, and putting markup in the prose part would wrap it in a paragraph no validator allows",
				HTML: Alert(AlertProps{Title: "Two apps need attention", Tone: "warning",
					Body: render.HTML("<ul><li>blog: disk at 91%</li><li>wiki: 4 restarts overnight</li></ul>")}, s),
			}, {
				Name: "no headline",
				Why:  "a message with no headline is still a message — the titleless shape is how a status line interrupts, and the refusal is saved for the box that says nothing at all",
				HTML: Alert(AlertProps{Text: "Two apps need attention.", Tone: "warning", Live: LivePolite}, s),
			}}
		},
	})

	Register(Spec{
		Name:    "Button",
		Anatomy: []Part{PartRoot, PartIcon},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return Button(ButtonProps{AriaLabel: "Close", Icon: SpecimenGlyph, Variant: "ghost", Parts: parts}, s)
		},
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
				Name: "off-site link",
				Why:  "a link that leaves the site opens a new tab and severs the opener — target=_blank without the noopener rel hands the destination a window handle back",
				HTML: Button(ButtonProps{Label: "Read the changelog", Variant: "primary", Href: "https://example.com/docs", External: true}, s),
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
						"data-cui-rpc":        "/apps/api/restart",
						"data-cui-rpc-method": "POST",
						"data-cui-rpc-signal": "apps",
					},
				}, s),
			}, {
				Name: "mutating a local signal",
				Why: "a click that never leaves the browser is still what the button DOES, so it travels the same " +
					"seam as a request: the mutation's attribute sits on the button beside its label, where a " +
					"reviewer reads them together",
				HTML: Button(ButtonProps{
					Label: "Add replica", Variant: "ghost",
					Action: html.Attrs{"data-cui-signal-inc": "replicas"},
				}, s),
			}}
		},
	})
}
