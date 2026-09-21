package headless

import (
	"strconv"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ValidationSummary parts.
const (
	PartErrorList Part = "error-list"
	PartErrorItem Part = "error-item"
	PartErrorLink Part = "error-link"
)

// FieldError is one thing that went wrong.
type FieldError struct {
	// For is the id of the control at fault. With it the message
	// becomes a link that moves focus to the field; without it the
	// message is text, which is the right fallback and a worse
	// experience.
	For string
	// Message is what is wrong, in words the person can act on.
	// "Invalid" is not one of them.
	Message string
}

// ValidationSummaryProps is the list of everything wrong with a form.
type ValidationSummaryProps struct {
	// Title heads the summary. Defaults to "There is a problem".
	Title string
	// Level is the title's heading level, 2 by default.
	Level  int
	Errors []FieldError
	// ID names the summary's root. Required, like the control ids the
	// errors link to: the title's id is derived from it, and two
	// summaries on one page without ids would share one title id —
	// breaking both labels and both announcements.
	ID         string
	ExtraAttrs html.Attrs

	// Parts: attrs and binds on the root, the title, the list and
	// its items. Strings carry the title a failed submit
	// focuses).
	Parts Parts
	// Strings are the strings this component says. Nil means the English
	// defaults; a layer above sets them from the request's language.
	Strings *Strings
}

// ValidationSummary renders the summary that goes above a form.
//
// This is the single highest-value accessibility component in a form,
// and the reasoning is worth stating in full.
//
// When a form fails validation, a sighted user sees red appear near
// the fields. A screen reader user, unless told, hears nothing: focus
// is wherever it was, the page looks the same to the accessibility
// tree except for text that changed somewhere below. So the summary:
//
//   - is role="alert", which interrupts — this DID just happen, it is
//     the one case where interrupting is correct;
//   - is tabindex="-1", so the server can send focus to it after a
//     failed submit. Not focusable-by-tab, focusable-by-script: it
//     must never become a tab stop for someone filling in the form;
//   - lists each error as a LINK to the field, because the value of
//     the summary is getting to the field, not reading the list. The
//     link moves focus to the control itself, so the next thing the
//     user types goes in the right box.
//
// Rendering it with no errors renders nothing: an empty "there is a
// problem" box that announces itself is a lie that interrupts.
func ValidationSummary(p ValidationSummaryProps, s Classes) render.HTML {
	b := p.Parts.Box(s)
	if p.ID == "" {
		panic("headless: ValidationSummary requires ID — two summaries on one page would share one title id, breaking both labels")
	}
	if len(p.Errors) == 0 {
		return ""
	}
	items := make([]render.HTML, 0, len(p.Errors))
	for _, e := range p.Errors {
		if e.Message == "" {
			panic("headless: FieldError requires Message")
		}
		var inner render.HTML
		// Every href a component writes goes through the anchor
		// policy, this one included: the policy accepts a bare
		// fragment, and a For that is empty or that the policy
		// refuses is rendered as text rather than as a link nowhere
		// should follow.
		jump := ""
		if e.For != "" {
			jump = urlsafe.CleanAnchor("#" + e.For)
		}
		if jump != "" {
			inner = b.El("a", PartErrorLink,
				Attrs(map[string]string{"href": jump}), render.Text(e.Message))
		} else {
			inner = render.Text(e.Message)
		}
		items = append(items, b.El("li", PartErrorItem, nil, inner))
	}
	own := Merge(Safe(p.ExtraAttrs, "role", "tabindex"), Attrs(map[string]string{
		"id": p.ID, "aria-labelledby": titleIDFor(p.ID),
	}))
	own["role"] = "alert"
	own["tabindex"] = "-1"

	return b.El("div", PartRoot, own,
		b.El(headingTag(p.Level), PartTitle,
			Attrs(map[string]string{"id": titleIDFor(p.ID)}),
			render.Text(orDefault(p.Title, p.Strings.Resolve().ThereIsAProblem))),
		b.El("ul", PartErrorList, nil, items...),
	)
}

// titleIDFor names the heading the summary is labelled by. The
// fallback prefix is structural, not the class map's namespace: this
// layer does not know what anyone calls their classes. The ID is
// required, so the name is always the summary's own.
func titleIDFor(id string) string {
	return id + "-title"
}

// ─── Timeline ───────────────────────────────────────────────────────

// Timeline parts.
const (
	PartTimelineItem Part = "timeline-item"
	PartTimelineMark Part = "timeline-mark"
	PartTimelineTime Part = "timeline-time"
	PartTimelineHead Part = "timeline-head"
	PartTimelineMeta Part = "timeline-meta"
	PartTimelineBody Part = "timeline-body"
)

// Event is one thing that happened.
type Event struct {
	// Title is what happened. Required.
	Title string
	// Detail is the supporting line.
	Detail string
	// When is the human-readable time ("3 days ago", "18:22").
	When string
	// Machine is the machine-readable timestamp for <time datetime>,
	// RFC 3339. Without it "3 days ago" is a string no assistive tech,
	// translation layer or scraper can resolve to a moment.
	Machine string
	// Tone lets the class map colour the marker — "success", "danger".
	Tone string
	// Meta is the secondary line beside the title — an actor, a
	// relative time ("by dom", "2h ago") — in the header row, read
	// after the title it qualifies. When is the timestamp contract;
	// Meta is a caption with no machine form.
	Meta string
	// Body is extra markup under the detail: a log excerpt, actions.
	Body render.HTML
}

// TimelineProps is a sequence of events.
type TimelineProps struct {
	// Label names the list for assistive tech — "Deploy history".
	Label  string
	Events []Event

	ID         string
	ExtraAttrs html.Attrs

	// Parts: attrs and binds on the list and every part it draws.
	Parts Parts
}

// Timeline renders the events as an ordered list.
//
// Ordered, because the order is the content: these things happened in
// this sequence, and <ol> is what says so — a screen reader announces
// the count and the position, so "3 of 7" locates you in the history
// without seeing the line down the left.
//
// The dots and the connecting line are aria-hidden. They are a picture
// of the ordering that the list already states, and announcing them
// would mean hearing "bullet" before every entry.
func Timeline(p TimelineProps, s Classes) render.HTML {
	if len(p.Events) == 0 {
		panic("headless: Timeline requires at least one event")
	}
	b := p.Parts.Box(s)
	items := make([]render.HTML, 0, len(p.Events))
	for _, e := range p.Events {
		if e.Title == "" {
			panic("headless: Event requires Title")
		}
		kids := make([]render.HTML, 0, 4)
		markAttrs := Attrs(map[string]string{"aria-hidden": "true"})
		// Tone reaches the class map as the mark's variant, joined to
		// the mark's own class the way Alert's tone joins its root: a
		// tinted dot is still a dot.
		if cls := s.Variant(PartTimelineMark, e.Tone); cls != "" {
			markAttrs["class"] = cls
		}
		kids = append(kids, b.El("span", PartTimelineMark, markAttrs, render.HTML("")))

		body := make([]render.HTML, 0, 4)
		if e.When != "" {
			// <time> promises machine-readable content: with a datetime
			// the text may say anything ("3 days ago"); without one the
			// text itself must be a valid date or time, and relative
			// words are not, so the element becomes a span.
			if e.Machine != "" {
				if _, err := time.Parse(time.RFC3339, e.Machine); err != nil {
					panic("headless: Event Machine must be an RFC 3339 timestamp, not " + strconv.Quote(e.Machine))
				}
				body = append(body, b.El("time", PartTimelineTime,
					Attrs(map[string]string{"datetime": e.Machine}), render.Text(e.When)))
			} else {
				body = append(body, b.El("span", PartTimelineTime, nil, render.Text(e.When)))
			}
		}
		body = append(body, b.El("p", PartTitle, nil, render.Text(e.Title)))
		if e.Meta != "" {
			// The meta line and the title share a header row: the meta
			// qualifies the title, and DOM order keeps the title first
			// for a reader who hears the event before its attribution.
			headRow := body[len(body)-1]
			body[len(body)-1] = b.El("div", PartTimelineHead, nil,
				headRow,
				b.El("span", PartTimelineMeta, nil, render.Text(e.Meta)))
		}
		if e.Detail != "" {
			body = append(body, b.El("p", PartDesc, nil, render.Text(e.Detail)))
		}
		if e.Body != "" {
			body = append(body, e.Body)
		}
		kids = append(kids, b.El("div", PartTimelineBody, nil, body...))
		items = append(items, b.El("li", PartTimelineItem, nil, kids...))
	}
	own := Merge(Safe(p.ExtraAttrs), Attrs(map[string]string{
		"id": p.ID, "aria-label": p.Label,
	}))
	return b.El("ol", PartRoot, own, items...)
}

func init() {
	Register(Spec{
		Name:    "ValidationSummary",
		Anatomy: []Part{PartRoot, PartTitle, PartErrorList, PartErrorItem, PartErrorLink},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return ValidationSummary(ValidationSummaryProps{ID: "errors",
				Errors: []FieldError{{For: "name", Message: "Enter an app name."}},
				Parts:  parts}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "after a failed submit",
				Why:  "it interrupts and can take focus, and every error is a link to the field it is about — a list of complaints you cannot navigate to is a list you have to hunt through",
				HTML: ValidationSummary(ValidationSummaryProps{
					Title: "This form could not be saved", ID: "errors",
					Errors: []FieldError{
						{For: "name", Message: "Enter an app name."},
						{For: "port", Message: "Port 8080 is already in use."},
					}}, s),
			}, {
				Name: "an error with no field",
				Why:  "\"the registry rejected the push\" belongs to no input, and dropping it because it has nowhere to link would lose the only message that explains the failure",
				HTML: ValidationSummary(ValidationSummaryProps{
					Title: "This form could not be saved", ID: "errors2",
					Errors: []FieldError{{Message: "The registry rejected the push."}}}, s),
			}, {
				Name: "nothing wrong",
				Why:  "no errors renders nothing at all, so a page can ask for the summary unconditionally and not get an empty red box on first load",
				HTML: group(ValidationSummary(ValidationSummaryProps{Title: "x", ID: "errors-empty"}, s),
					render.HTML("<p>Nothing to report.</p>")),
			}, {
				Name: "two summaries on one page",
				Why: "two forms that failed on one page label two summaries, and the required ids are what keep the titles from " +
					"sharing one name — the reference gate reads both labels and both resolve to their own heading",
				HTML: group(
					ValidationSummary(ValidationSummaryProps{ID: "errors-left",
						Errors: []FieldError{{For: "left-name", Message: "Enter a name."}}}, s),
					ValidationSummary(ValidationSummaryProps{ID: "errors-right",
						Errors: []FieldError{{For: "right-name", Message: "Enter a name."}}}, s)),
			}}
		},
	})

	Register(Spec{
		Name:    "Timeline",
		Anatomy: []Part{PartRoot, PartTimelineItem, PartTimelineMark, PartTimelineTime, PartTimelineHead, PartTimelineMeta, PartTimelineBody, PartTitle, PartDesc},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return Timeline(TimelineProps{Events: []Event{{Title: "Deployed"}}, Parts: parts}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "history",
				Why:  "an event with no tone is the ordinary case and draws an untinted marker; ordered, because the order is the content: a screen reader announces the count and the position, so \"3 of 7\" locates you in the history without seeing the line down the left",
				HTML: Timeline(TimelineProps{Label: "Deploy history", Events: []Event{
					{Title: "Deployed", Detail: "From main, by Donald.", When: "3 days ago",
						Machine: "2026-09-08T11:04:00Z", Tone: "success"},
					{Title: "Build failed", Detail: "Exit 1 in the test stage.", When: "4 days ago",
						Machine: "2026-09-07T09:12:00Z", Tone: "danger"},
					{Title: "Configuration changed", When: "5 days ago", Machine: "2026-09-06T16:40:00Z"},
				}}, s),
			}, {
				Name: "attributed",
				Why:  "the meta line qualifies the title from the same row — an actor or a relative time — and the title stays first in the tree so a reader hears the event before its attribution",
				HTML: Timeline(TimelineProps{Label: "Audit log", Events: []Event{
					{Title: "Role granted", Meta: "by dom", Detail: "admin, on the api app."},
				}}, s),
			}}
		},
	})
}
