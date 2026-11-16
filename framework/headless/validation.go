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
	// For is the id of the control at fault, which the message links to.
	For     string
	Message string
}

// ValidationSummaryProps is the list of everything wrong with a form.
type ValidationSummaryProps struct {
	// Title defaults to "There is a problem"; Level, its heading, to 2.
	Title      string
	Level      int
	Errors     []FieldError
	ID         string
	ExtraAttrs html.Attrs

	Parts Parts
	// Strings are the words it says; nil means the English defaults.
	Strings *Strings
}

// ValidationSummary renders the focusable alert listing a form's errors,
// nothing when there are none; it panics without ID or on an empty Message.
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
			Internal(Attrs(map[string]string{"id": titleIDFor(p.ID)})),
			render.Text(orDefault(p.Title, p.Strings.Resolve().ThereIsAProblem))),
		b.El("ul", PartErrorList, Internal(nil), items...),
	)
}

func titleIDFor(id string) string {
	return id + "-title"
}

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
	// Title, or the caller's Lead markup, is what happened; exactly one is set.
	Title  string
	Lead   render.HTML
	Detail string
	// When is the human-readable time; Machine its RFC 3339 form.
	When    string
	Machine string
	// Tone is the marker's class-map variant — "success", "danger".
	Tone string
	Icon render.HTML
	// Meta is the line after the title in its header row — an actor.
	Meta string
	Body render.HTML
}

// TimelineProps is a sequence of events.
type TimelineProps struct {
	Label  string
	Events []Event

	ID         string
	ExtraAttrs html.Attrs

	Parts Parts
}

// Timeline renders the events as an ordered list; it panics on no events,
// an event without one headline, or a Machine that is not RFC 3339.
func Timeline(p TimelineProps, s Classes) render.HTML {
	if len(p.Events) == 0 {
		panic("headless: Timeline requires at least one event")
	}
	b := p.Parts.Box(s)
	items := make([]render.HTML, 0, len(p.Events))
	for _, e := range p.Events {
		if (e.Title == "") == (e.Lead == "") {
			panic("headless: Event requires exactly one of Title and Lead")
		}
		kids := make([]render.HTML, 0, 4)
		lead := e.Lead != ""
		slot := e.Body != "" || e.Icon != "" || lead
		markAttrs := internalIf(slot && e.Icon == "", Attrs(map[string]string{"aria-hidden": "true"}))
		if cls := s.Variant(PartTimelineMark, e.Tone); cls != "" {
			markAttrs["class"] = cls
		}
		kids = append(kids, b.El("span", PartTimelineMark, markAttrs, e.Icon))

		body := make([]render.HTML, 0, 4)
		if e.When != "" {
			if e.Machine != "" {
				if _, err := time.Parse(time.RFC3339, e.Machine); err != nil {
					panic("headless: Event Machine must be an RFC 3339 timestamp, not " + strconv.Quote(e.Machine))
				}
				body = append(body, b.El("time", PartTimelineTime,
					internalIf(slot, Attrs(map[string]string{"datetime": e.Machine})), render.Text(e.When)))
			} else {
				body = append(body, b.El("span", PartTimelineTime, internalIf(slot, nil), render.Text(e.When)))
			}
		}
		title, titleAttrs := e.Lead, html.Attrs{"data-lead": ""}
		if !lead {
			title, titleAttrs = render.Text(e.Title), nil
		}
		body = append(body, b.El("p", PartTitle, internalIf(slot && e.Meta == "" && !lead, titleAttrs), title))
		if e.Meta != "" {
			headRow := body[len(body)-1]
			body[len(body)-1] = b.El("div", PartTimelineHead, internalIf(slot && !lead, nil),
				headRow,
				b.El("span", PartTimelineMeta, internalIf(lead, nil), render.Text(e.Meta)))
		}
		if e.Detail != "" {
			body = append(body, b.El("p", PartDesc, internalIf(slot, nil), render.Text(e.Detail)))
		}
		if e.Body != "" {
			body = append(body, e.Body)
		}
		kids = append(kids, b.El("div", PartTimelineBody, internalIf(e.Icon != "" && e.Body == "" && !lead, nil), body...))
		items = append(items, b.El("li", PartTimelineItem, internalIf(!slot, nil), kids...))
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
			}, {
				Name: "with icons",
				Why:  "an activity feed marks each event with what kind it was, a pencil or a plus, inside the hidden marker; the title still carries the words",
				HTML: Timeline(TimelineProps{Label: "Recent activity", Events: []Event{
					{Title: "Ada created INV-1", Meta: "2h ago", Icon: render.HTML(`<svg aria-hidden="true" viewBox="0 0 24 24"></svg>`)},
				}}, s),
			}}
		},
	})
}
