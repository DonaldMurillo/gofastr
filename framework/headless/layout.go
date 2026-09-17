package headless

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Layout parts.
const (
	PartSectionHead Part = "section-head"
	PartSectionBody Part = "section-body"
	PartDividerLine Part = "divider-line"
)

// The layout primitives have almost no accessibility surface, and the
// exceptions are the whole reason they are components rather than
// classes:
//
//   - A <section> is only a landmark when it has an accessible name.
//     Unnamed, it is a div that clutters a screen reader's landmark
//     list with entries called "section", which is worse than not
//     being a landmark at all — so Section renders a plain div until
//     it is given a heading to be named by.
//   - A separator between groups of content is meaningful and gets
//     <hr>; a line drawn for looks inside one group is decoration and
//     must be hidden, or a screen reader announces "separator" at
//     every visual flourish on the page.
//
// Everything else here — gaps, tracks, gutters — is the class map's, and
// these types exist so an app never writes a grid-template by hand.

// StackProps is vertical flow: one thing after another, with one gap.
type StackProps struct {
	// Gap is a name from the scale, passed through to the class map. It is
	// not a length, because a system with arbitrary gaps has no
	// rhythm — and the one the framework ships proves the point: its
	// Grid takes a free-form Min that nothing ever read.
	Gap string
	// Align is cross-axis alignment: "start", "center", "end". Empty
	// stretches, which is what a stack of cards wants.
	Align string
	// Tag overrides the element. A list of things should be a list;
	// this is how a Stack becomes one without a second component.
	Tag string

	ID         string
	ExtraAttrs html.Attrs
}

// Stack renders vertical flow.
func Stack(p StackProps, s Classes, children ...render.HTML) render.HTML {
	own := Merge(Safe(p.ExtraAttrs), Attrs(map[string]string{"id": p.ID}))
	mods(own, s, "gap", p.Gap, "align", p.Align)
	return El(orDefault(p.Tag, "div"), s, PartRoot, own, children...)
}

func init() {
	Register(Spec{
		Name:    "Stack",
		Anatomy: []Part{PartRoot},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "default gap",
				Why:  "no gap named is still a gap: the default is this primitive's own rhythm, so two pages that say nothing agree",
				HTML: Stack(StackProps{}, s,
					render.HTML("<p>CPU is back under 60%.</p>"),
					render.HTML("<p>The worker drained its deploy queue overnight.</p>")),
			}, {
				Name: "named gap",
				Why:  "the step is a name from the scale, never a length the caller brings — the class either exists in the skin or the value is a typo, and both show the first time anyone looks",
				HTML: Stack(StackProps{Gap: "lg"}, s,
					render.HTML("<p>Queued builds: 0.</p>"),
					render.HTML("<p>Running builds: 2.</p>")),
			}, {
				Name: "one child",
				Why:  "a stack of one is still a stack: the element is the seam the second block arrives into, and a wrapper that appears with it moves everything below",
				HTML: Stack(StackProps{}, s, render.HTML("<p>No volumes are attached to this node.</p>")),
			}}
		},
	})
}

// ClusterProps is horizontal flow that wraps: a row of buttons, a row
// of tags, a toolbar.
type ClusterProps struct {
	Gap string
	// Align is cross-axis: "center" by default in the class map, because a
	// row of controls of different heights should line up on their
	// middles.
	Align string
	// Justify is main-axis: "start", "between", "end".
	Justify string
	// NoWrap keeps the row on one line. Use it sparingly: a row that
	// cannot wrap is a row that overflows on a phone, and horizontal
	// page scroll is the failure this whole layer exists to prevent.
	NoWrap bool
	Tag    string

	ID         string
	ExtraAttrs html.Attrs
}

// Cluster renders a wrapping row.
//
// A row of controls is not a Cluster: Toolbar aligns its children by
// construction, every child being control-height, and gives the
// search field the slack and the groups their labels. Cluster is for
// content that wraps — tags, badges, a byline's parts — where the row
// is a layout fact and nothing in it is a control.
func Cluster(p ClusterProps, s Classes, children ...render.HTML) render.HTML {
	own := Merge(Safe(p.ExtraAttrs), Attrs(map[string]string{"id": p.ID}))
	mods(own, s, "gap", p.Gap, "align", p.Align, "justify", p.Justify)
	if p.NoWrap {
		mods(own, s, "wrap", "none")
	}
	return El(orDefault(p.Tag, "div"), s, PartRoot, own, children...)
}

func init() {
	Register(Spec{
		Name:    "Cluster",
		Anatomy: []Part{PartRoot},
		Cases: func(k Kit) []Case {
			s := k.Classes
			button := func(label, variant string) render.HTML {
				return Button(ButtonProps{Label: label, Variant: variant}, k.For("Button"))
			}
			return []Case{{
				Name: "default",
				Why:  "a row of controls is what the component is for, and it wraps by default — a row that cannot wrap is a row that overflows a phone",
				HTML: Cluster(ClusterProps{}, s,
					button("Restart", "secondary"), button("Stop", "secondary"), button("View logs", "ghost")),
			}, {
				Name: "gap and alignment",
				Why:  "controls of different heights meet on their middles: the gap is a step on the same scale every other primitive uses and the alignment is named on its own axis",
				HTML: Cluster(ClusterProps{Gap: "sm", Align: "center"}, s,
					Button(ButtonProps{Label: "Save", Variant: "primary"}, k.For("Button")),
					Button(ButtonProps{AriaLabel: "Deployment settings", Icon: SpecimenGlyph, Variant: "ghost"}, k.For("Button")),
					button("Cancel", "ghost")),
			}, {
				Name: "space between",
				Why:  "justify-between puts the destructive action at the far end from the safe one — the space is the skin's, not a spacer element somebody wedged between them",
				HTML: Cluster(ClusterProps{Justify: "between"}, s,
					button("Save", "primary"), button("Delete app", "secondary")),
			}}
		},
	})
}

// GridProps is an auto-fitting grid.
type GridProps struct {
	// Min is the narrowest a column may get before the grid drops one,
	// as a name from the scale ("sm", "md", "lg"), not a length.
	//
	// A length is what the framework's Grid takes, and it silently did
	// nothing for every value: the component wrote a data attribute
	// and no stylesheet ever read it. A named step cannot rot that way
	// — the class map either has a rule for the name or the name is a typo
	// that shows up the first time anyone looks.
	Min string
	Gap string
	Tag string

	ID         string
	ExtraAttrs html.Attrs
}

// Grid renders the auto-fitting grid.
func Grid(p GridProps, s Classes, children ...render.HTML) render.HTML {
	own := Merge(Safe(p.ExtraAttrs), Attrs(map[string]string{"id": p.ID}))
	mods(own, s, "gap", p.Gap, "min", p.Min)
	return El(orDefault(p.Tag, "div"), s, PartRoot, own, children...)
}

func init() {
	Register(Spec{
		Name:    "Grid",
		Anatomy: []Part{PartRoot},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "minimum column and gap",
				Why:  "Min is a floor — a column never gets narrower than the named step before the grid gives one up — and the name is what keeps the floor wired: the framework's Grid took a free-form Min that nothing read, so every auto-fit grid was the default width whatever its author asked",
				HTML: Grid(GridProps{Min: "md", Gap: "lg"}, s,
					render.HTML("<p>blog — 2 vCPU</p>"),
					render.HTML("<p>wiki — 1 vCPU</p>"),
					render.HTML("<p>api — 4 vCPU</p>")),
			}}
		},
	})
}

// ContainerProps is the page's measure: a maximum width and the
// gutters that keep content off the edge of the screen.
type ContainerProps struct {
	// Size names the measure — "sm", "md", "lg", "full".
	Size string
	Tag  string

	ID         string
	ExtraAttrs html.Attrs
}

// Container renders the measure.
func Container(p ContainerProps, s Classes, children ...render.HTML) render.HTML {
	own := Merge(Safe(p.ExtraAttrs), Attrs(map[string]string{"id": p.ID}))
	mods(own, s, "size", p.Size)
	return El(orDefault(p.Tag, "div"), s, PartRoot, own, children...)
}

func init() {
	Register(Spec{
		Name:    "Container",
		Anatomy: []Part{PartRoot},
		Cases: func(k Kit) []Case {
			s := k.Classes
			measure := func(size, body string) render.HTML {
				return Container(ContainerProps{Size: size}, s, render.HTML(body))
			}
			return []Case{{
				Name: "reading measure",
				Why:  "the default is the measure a page of prose reads at — line length is readability, decided here once so no page has to remember to ask for it",
				HTML: measure("", "<p>One leader runs the cluster and any number of workers join it.</p>"),
			}, {
				Name: "small",
				Why:  "a focused task wants a short measure — a sign-in card, one setting — where a full-width line would wander",
				HTML: measure("sm", "<p>Sign in to continue.</p>"),
			}, {
				Name: "medium",
				Why:  "a settings form sits between prose and dashboard: wide enough for label and field beside each other, narrow enough that the eye keeps the line",
				HTML: measure("md", "<p>HTTP port 80, HTTPS port 443.</p>"),
			}, {
				Name: "large",
				Why:  "the widest bounded measure — wide content such as a table of apps, still stopped somewhere so a line cannot run the width of the screen",
				HTML: measure("lg", "<p>Eleven apps across three workers.</p>"),
			}, {
				Name: "full",
				Why:  "full drops the maximum and keeps the gutter — a dashboard should use the whole screen, and the gutter is the one thing it may not lose, because the edge of a phone is where text stops being readable",
				HTML: measure("full", "<p>CPU, memory and network for every node.</p>"),
			}}
		},
	})
}

// SectionProps is a titled region of a page.
type SectionProps struct {
	// Title is the heading. It is also what makes this a landmark: a
	// section with a name is navigable, a section without one is noise
	// in the landmark list, so Section renders a plain div until it
	// has a title to be named by.
	Title string
	// Level is the heading level, 2 by default. It is a real decision
	// and not a style: heading levels are how a screen reader user
	// moves through a page, and a section under an <h1> that renders
	// an <h3> leaves a hole in the outline.
	Level int
	// Description is supporting text under the heading.
	Description string
	// Actions sit opposite the heading — a button, a filter.
	Actions render.HTML
	Gap     string

	ID         string
	ExtraAttrs html.Attrs
}

// Section renders the region.
func Section(p SectionProps, s Classes, children ...render.HTML) render.HTML {
	own := Merge(Safe(p.ExtraAttrs), Attrs(map[string]string{"id": p.ID}))
	mods(own, s, "gap", p.Gap)
	body := El("div", s, PartSectionBody, nil, children...)
	if p.Title == "" {
		return El("div", s, PartRoot, own, body)
	}

	titleID := p.ID
	if titleID == "" {
		titleID = slugID(p.Title)
	}
	titleID += "-title"
	own["aria-labelledby"] = titleID

	head := []render.HTML{
		El(headingTag(p.Level), s, PartTitle,
			Attrs(map[string]string{"id": titleID}), render.Text(p.Title)),
	}
	if p.Description != "" {
		head = append(head, El("p", s, PartDesc, nil, render.Text(p.Description)))
	}
	headWrap := El("div", s, PartHeader, nil, head...)
	if p.Actions != "" {
		headWrap = El("div", s, PartSectionHead, nil, headWrap,
			El("div", s, PartFooter, nil, p.Actions))
	}
	return El("section", s, PartRoot, own, headWrap, body)
}

func headingTag(level int) string {
	if level < 1 || level > 6 {
		level = 2
	}
	return "h" + string(rune('0'+level))
}

// slugID derives a stable id from a title, so a section without an
// explicit ID still has one for aria-labelledby to point at. Two
// sections with the same title on one page collide — which is a real
// limit, and the reason ID exists.
//
// The prefix is "section-", not the class map's namespace: this layer
// does not know what anyone calls their classes, and an id that
// borrowed that name would tie the structure to one class map.
func slugID(s string) string {
	out := make([]rune, 0, len(s))
	prevDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out = append(out, r)
			prevDash = false
		case r >= 'A' && r <= 'Z':
			out = append(out, r+32)
			prevDash = false
		default:
			if !prevDash && len(out) > 0 {
				out = append(out, '-')
				prevDash = true
			}
		}
	}
	for len(out) > 0 && out[len(out)-1] == '-' {
		out = out[:len(out)-1]
	}
	return "section-" + string(out)
}

// DividerProps is a line between things.
type DividerProps struct {
	// Label puts text in the break ("or"). A labelled divider is
	// always meaningful, so it is never decorative.
	Label string
	// Vertical draws it along the block axis, for a divider inside a
	// row.
	Vertical bool
	// Decorative says this line groups nothing — it is a flourish, and
	// is hidden from assistive tech. Without it a screen reader
	// announces "separator" at every line on the page.
	Decorative bool

	ID         string
	ExtraAttrs html.Attrs
}

// Divider renders the line.
//
// A meaningful one is an <hr>: the element already means "a thematic
// break", so no role has to be claimed. A decorative one is a div that
// says nothing, because the alternative — an <hr> with aria-hidden —
// is a semantic element being told to lie.
func Divider(p DividerProps, s Classes) render.HTML {
	own := Merge(Safe(p.ExtraAttrs), Attrs(map[string]string{"id": p.ID}))
	if p.Vertical {
		mods(own, s, "orient", "vertical")
		own["aria-orientation"] = "vertical"
	}
	part := PartRoot
	if p.Decorative {
		delete(own, "aria-orientation")
		own["aria-hidden"] = "true"
		return El("div", s, part, own)
	}
	if p.Label == "" {
		return El("hr", s, part, own)
	}
	// With a label the line cannot be an <hr> — it has no content
	// model — so the role is stated on the element that replaces it.
	own["role"] = "separator"
	return El("div", s, part, own,
		El("span", s, PartDividerLine, Attrs(map[string]string{"aria-hidden": "true"}), render.HTML("")),
		El("span", s, PartText, nil, render.Text(p.Label)),
		El("span", s, PartDividerLine, Attrs(map[string]string{"aria-hidden": "true"}), render.HTML("")),
	)
}

func init() {
	Register(Spec{
		Name:    "Divider",
		Anatomy: []Part{PartRoot, PartDividerLine, PartText},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "horizontal",
				Why:  "the default is a real hr: a break between two groups of content means separator, and the element already says it, so no role is claimed",
				HTML: Divider(DividerProps{}, s),
			}, {
				Name: "vertical",
				Why:  "inside a row the break runs along the block axis, and aria-orientation says so — a vertical rule drawn as a bare line says nothing about which way the groups sit",
				HTML: Divider(DividerProps{Vertical: true}, s),
			}, {
				Name: "labelled",
				Why:  "a label cannot live in an hr — it has no content model — so the separator role is claimed on the div that replaces it and the lines around the words are hidden",
				HTML: Divider(DividerProps{Label: "or"}, s),
			}}
		},
	})
}

// mods looks up one class per modifier and joins them onto the
// element, as Button does with its variant and size.
//
// One lookup per modifier, never a combined key: a class map keyed on
// "root--md--center" would have to enumerate every gap crossed with
// every alignment, and the first value nobody thought to combine
// renders unstyled. Keys are namespaced by axis ("gap-md",
// "align-center") so a gap named "center" could never collide with an
// alignment named "center".
func mods(own html.Attrs, s Classes, pairs ...string) {
	for i := 0; i+1 < len(pairs); i += 2 {
		axis, value := pairs[i], pairs[i+1]
		if value == "" {
			continue
		}
		if cls := s.Variant(PartRoot, axis+"-"+value); cls != "" {
			own["class"] = joinClasses(own["class"], cls)
		}
	}
}

func init() {
	Register(Spec{
		Name:    "Section",
		Anatomy: []Part{PartRoot, PartTitle, PartDesc, PartHeader, PartSectionHead, PartSectionBody, PartFooter},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "named",
				Why:  "a name is what makes it a landmark, and the name comes from the heading itself rather than a second string that can drift from it",
				HTML: Section(SectionProps{Title: "Volumes", Description: "Disks attached to this node.",
					Actions: Button(ButtonProps{Label: "Add", Variant: "secondary"}, k.For("Button"))}, s,
					render.HTML("<p>Two volumes.</p>")),
			}, {
				Name: "unnamed",
				Why:  "no title means a plain div: a section with no name is a row in the landmark list that says nothing, which is worse than not being in it",
				HTML: Section(SectionProps{}, s, render.HTML("<p>Loose content.</p>")),
			}, {
				Name: "deeper in the outline",
				Why:  "the level is the caller's decision — a section under an h2 that renders an h2 leaves a hole a screen reader user has to guess at",
				HTML: Section(SectionProps{Title: "Mounts", Level: 3}, s, render.HTML("<p>One.</p>")),
			}}
		},
	})
}

// ─── Spacer ─────────────────────────────────────────────────────────

// SpacerProps is the flexible space in a row: between a label and a
// value in a contents list, between the safe action and the
// destructive one.
type SpacerProps struct {
	// Grow is how much of the row's slack this spacer takes against
	// its growing siblings. 1 through 4 — 0 is refused because a
	// spacer that cannot grow is a typo rather than a choice (the
	// typed Spacer defaults it to 1), and above 4 is refused because
	// the stylesheet wires exactly those four factors: a number the
	// sheet does not carry would be a factor that renders as 1.
	//
	// It travels as data-hui-grow rather than a style, because an
	// inline style is a rule the CSP drops and a class per factor is
	// a class the class map has to enumerate from a number it cannot see.
	Grow int
	// Min and Max bound the space, as names from the gap scale rather
	// than lengths: a spacer with a floor keeps a contents list legible
	// when the value is missing, and one with a ceiling keeps two
	// distant words from being pushed to the edges of the screen.
	Min string
	Max string
	// Leader draws a dotted line across the space — the leader between
	// a term and its value in a contents list, which is what ties the
	// two together once the row is wider than the pair.
	Leader bool
	// Rule draws a hairline rule across the space, for when the line
	// should read as a divider-in-waiting rather than a tie. A leader
	// and a rule in the same space are two answers to one question,
	// and are refused together.
	Rule bool

	ID         string
	ExtraAttrs html.Attrs
}

// Spacer renders the space.
//
// It is a <span>, not a div, because a contents list is a paragraph
// ("Restarts … 263" is one sentence of a list) and a div inside a <p>
// is a parse error the browser repairs by closing the paragraph —
// which splits the list into pieces nobody styled. It is aria-hidden
// and empty: the space is the whole content, and a screen reader user
// gets the term and the value as neighbours, which is the same fact
// the leader line draws for the eye.
func Spacer(p SpacerProps, s Classes) render.HTML {
	if p.Grow < 1 || p.Grow > 4 {
		panic("headless: Spacer Grow must be 1 through 4 — the stylesheet wires those four factors, and 0 is a spacer that cannot grow")
	}
	if p.Leader && p.Rule {
		panic("headless: Spacer cannot draw a leader and a rule in the same space")
	}
	own := Merge(Safe(p.ExtraAttrs, "data-hui-grow"),
		Attrs(map[string]string{"id": p.ID, "data-hui-grow": strconv.Itoa(p.Grow)}))
	own["aria-hidden"] = "true"
	mods(own, s, "min", p.Min, "max", p.Max)
	if p.Leader || p.Rule {
		kind := "rule"
		if p.Leader {
			kind = "leader"
		}
		if cls := s.Variant(PartRoot, kind); cls != "" {
			own["class"] = joinClasses(own["class"], cls)
		}
	}
	return El("span", s, PartRoot, own)
}

func init() {
	Register(Spec{
		Name:    "Spacer",
		Anatomy: []Part{PartRoot},
		Hooks:   []string{"data-hui-grow"},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "plain",
				Why:  "it renders nothing, says nothing, and is in the markup only because a row needs a thing to grow — the space between two controls is the skin's, not a third control somebody wedged between them",
				HTML: Cluster(ClusterProps{Justify: "between"}, s,
					Button(ButtonProps{Label: "Save", Variant: "primary"}, k.For("Button")),
					Spacer(SpacerProps{Grow: 1}, s),
					Button(ButtonProps{Label: "Delete app", Variant: "secondary"}, k.For("Button"))),
			}, {
				Name: "bounded",
				Why:  "a floor and a ceiling are names from the gap scale, so a spacer that must stay legible at any width says so in the same vocabulary every other gap uses",
				HTML: Cluster(ClusterProps{Gap: "sm", Align: "baseline"}, s,
					render.Text("Queued builds"),
					Spacer(SpacerProps{Grow: 2, Min: "sm", Max: "lg"}, s),
					render.Text("2")),
			}, {
				Name: "leader in a contents list",
				Why:  "the dotted line between a term and its value is what ties the pair together once the row is wider than they are — and the spacer is a span so the list can be one paragraph, which a div would silently split",
				HTML: Cluster(ClusterProps{Tag: "p", Gap: "none", Align: "baseline"}, s,
					render.Text("Restarts"),
					Spacer(SpacerProps{Grow: 1, Leader: true}, s),
					render.Text("263")),
			}, {
				Name: "rule",
				Why:  "a hairline across the space reads as structure rather than as a tie, and the two are one knob each: a line that must be both is a line nobody chose",
				HTML: Cluster(ClusterProps{Tag: "p", Gap: "none", Align: "baseline"}, s,
					render.Text("Deploys today"),
					Spacer(SpacerProps{Grow: 1, Rule: true}, s),
					render.Text("8")),
			}}
		},
	})
}
