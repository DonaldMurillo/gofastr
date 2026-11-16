package headless

import (
	"context"
	"strconv"
	"unicode"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Layout parts.
const (
	PartSectionHead Part = "section-head"
	PartSectionBody Part = "section-body"
	PartSectionBrow Part = "section-eyebrow"
	PartDividerLine Part = "divider-line"
)

// StackProps is vertical flow: one thing after another, with one gap.
type StackProps struct {
	Gap     string // a step on the gap scale, never a length
	Align   string // cross axis: start, center, end; empty stretches
	Justify string // main axis: start, center, end, between; empty packs to the start
	Tag     string // the element, "div" by default

	ID         string
	ExtraAttrs html.Attrs

	Parts Parts
}

// Stack renders vertical flow.
func Stack(p StackProps, s Classes, children ...render.HTML) render.HTML {
	b := p.Parts.Box(s)
	own := Merge(Safe(p.ExtraAttrs), Attrs(map[string]string{"id": p.ID}))
	mods(own, s, "gap", p.Gap, "align", p.Align, "justify", p.Justify)
	return b.El(orDefault(p.Tag, "div"), PartRoot, own, children...)
}

func init() {
	Register(Spec{
		Name: "Stack",
		WithParts: func(s Classes, parts Parts) render.HTML {
			return Stack(StackProps{Gap: "lg", Parts: parts}, s, render.HTML("<p>One.</p>"))
		},
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

// ClusterProps is horizontal flow that wraps: tags, badges, a byline's parts.
type ClusterProps struct {
	Gap     string
	Align   string // cross axis; the class map centres by default
	Justify string // main axis: start, between, end
	NoWrap  bool   // keeps the row on one line, which overflows a phone
	Tag     string

	ID         string
	ExtraAttrs html.Attrs

	Parts Parts
}

// Cluster renders a wrapping row of content; a row of controls is a Toolbar.
func Cluster(p ClusterProps, s Classes, children ...render.HTML) render.HTML {
	b := p.Parts.Box(s)
	own := Merge(Safe(p.ExtraAttrs), Attrs(map[string]string{"id": p.ID}))
	mods(own, s, "gap", p.Gap, "align", p.Align, "justify", p.Justify)
	if p.NoWrap {
		mods(own, s, "wrap", "none")
	}
	return b.El(orDefault(p.Tag, "div"), PartRoot, own, children...)
}

func init() {
	Register(Spec{
		Name: "Cluster",
		WithParts: func(s Classes, parts Parts) render.HTML {
			return Cluster(ClusterProps{Gap: "sm", Parts: parts}, s, render.HTML("<p>One.</p>"))
		},
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
	Min string // the narrowest column, as a step on the scale ("sm", "md", "lg")
	Gap string
	Tag string

	ID         string
	ExtraAttrs html.Attrs

	Parts Parts
}

// Grid renders the auto-fitting grid.
func Grid(p GridProps, s Classes, children ...render.HTML) render.HTML {
	b := p.Parts.Box(s)
	own := Merge(Safe(p.ExtraAttrs), Attrs(map[string]string{"id": p.ID}))
	mods(own, s, "gap", p.Gap, "min", p.Min)
	return b.El(orDefault(p.Tag, "div"), PartRoot, own, children...)
}

func init() {
	Register(Spec{
		Name: "Grid",
		WithParts: func(s Classes, parts Parts) render.HTML {
			return Grid(GridProps{Gap: "lg", Parts: parts}, s, render.HTML("<p>One.</p>"))
		},
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

// ContainerProps is the page's measure: a maximum width and its gutters.
type ContainerProps struct {
	Size string // "sm", "md", "lg" or "full"; empty is the reading measure
	Tag  string

	ID         string
	ExtraAttrs html.Attrs

	Parts Parts
}

// Container renders the measure.
func Container(p ContainerProps, s Classes, children ...render.HTML) render.HTML {
	b := p.Parts.Box(s)
	own := Merge(Safe(p.ExtraAttrs), Attrs(map[string]string{"id": p.ID}))
	mods(own, s, "size", p.Size)
	return b.El(orDefault(p.Tag, "div"), PartRoot, own, children...)
}

func init() {
	Register(Spec{
		Name: "Container",
		WithParts: func(s Classes, parts Parts) render.HTML {
			return Container(ContainerProps{Size: "sm", Parts: parts}, s, render.HTML("<p>One.</p>"))
		},
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
	Title           string      // the heading, and the region's name
	Eyebrow         string      // a kicker above the title, hidden from assistive tech
	Level           int         // the heading level, 2 by default
	Description     string      // supporting text under the heading
	DescriptionHTML render.HTML // supporting markup; wins over Description
	Label           string      // names the region when there is no Title; Title wins
	Actions         render.HTML // controls opposite the heading
	Gap             string

	ID         string
	ExtraAttrs html.Attrs

	Parts Parts
	Ctx   context.Context // a derived id goes through its id scope; an explicit ID is never changed
}

// SectionIDChildren are the suffixes of the ids a section derives from its id.
var SectionIDChildren = []string{"-title"}

// Section renders the region.
func Section(p SectionProps, s Classes, children ...render.HTML) render.HTML {
	b := p.Parts.Box(s)
	render.ReserveID(p.Ctx, p.ID, SectionIDChildren...)
	own := Merge(Safe(p.ExtraAttrs, "aria-label"), Attrs(map[string]string{"id": p.ID}))
	mods(own, s, "gap", p.Gap)
	bodyOwn := html.Attrs(nil)
	if len(children) == 0 {
		bodyOwn = Internal(nil)
	}
	body := b.El("div", PartSectionBody, bodyOwn, children...)
	if p.Title == "" {
		if p.Label != "" {
			own["aria-label"] = p.Label
			if p.Eyebrow != "" {
				return b.El("section", PartRoot, own,
					b.El("p", PartSectionBrow,
						Internal(Attrs(map[string]string{"aria-hidden": "true"})), render.Text(p.Eyebrow)),
					body)
			}
			return b.El("section", PartRoot, own, body)
		}
		return b.El("div", PartRoot, own, body)
	}
	titleID := p.ID
	if titleID == "" {
		titleID = render.UniqueID(p.Ctx, slugID("section", p.Title), SectionIDChildren...)
	}
	titleID += "-title"
	own["aria-labelledby"] = titleID

	headOwn, partOwn := Internal(nil), html.Attrs(nil)
	if p.DescriptionHTML != "" {
		headOwn, partOwn = nil, Internal(nil)
	}
	head := make([]render.HTML, 0, 3)
	if p.Eyebrow != "" {
		head = append(head, b.El("p", PartSectionBrow,
			Merge(Attrs(map[string]string{"aria-hidden": "true"}), partOwn), render.Text(p.Eyebrow)))
	}
	head = append(head, b.El(headingTag(p.Level), PartTitle,
		Merge(Attrs(map[string]string{"id": titleID}), partOwn), render.Text(p.Title)))
	if p.DescriptionHTML != "" {
		head = append(head, b.El("p", PartDesc, nil, p.DescriptionHTML))
	} else if p.Description != "" {
		head = append(head, b.El("p", PartDesc, nil, render.Text(p.Description)))
	}
	headWrap := b.El("div", PartHeader, headOwn, head...)
	if p.Actions != "" {
		headWrap = b.El("div", PartSectionHead, nil, headWrap,
			b.El("div", PartFooter, nil, p.Actions))
	}
	return b.El("section", PartRoot, own, headWrap, body)
}

func headingTag(level int) string {
	if level < 1 || level > 6 {
		level = 2
	}
	return "h" + string(rune('0'+level))
}

// slugID derives an id from a title, prefixed by the component's own name.
func slugID(prefix, s string) string {
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
		case r > unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			out = append(out, unicode.ToLower(r))
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
	return prefix + "-" + string(out)
}

// DividerProps is a line between things.
type DividerProps struct {
	Label      string // text in the break; a labelled divider is never decorative
	Vertical   bool
	Decorative bool // a flourish, hidden from assistive tech

	ID         string
	ExtraAttrs html.Attrs

	Parts Parts
}

// Divider renders an <hr>, a role=separator div when labelled, or a hidden div when decorative.
func Divider(p DividerProps, s Classes) render.HTML {
	b := p.Parts.Box(s)
	own := Merge(Safe(p.ExtraAttrs), Attrs(map[string]string{"id": p.ID}))
	if p.Vertical {
		mods(own, s, "orient", "vertical")
		own["aria-orientation"] = "vertical"
	}
	if p.Decorative {
		delete(own, "aria-orientation")
		own["aria-hidden"] = "true"
		return b.El("div", PartRoot, own)
	}
	if p.Label == "" {
		return b.El("hr", PartRoot, own)
	}
	own["role"] = "separator"
	return b.El("div", PartRoot, own,
		b.El("span", PartDividerLine, Internal(Attrs(map[string]string{"aria-hidden": "true"})), render.HTML("")),
		b.El("span", PartText, Internal(nil), render.Text(p.Label)),
		b.El("span", PartDividerLine, Internal(Attrs(map[string]string{"aria-hidden": "true"})), render.HTML("")),
	)
}
func init() {
	Register(Spec{
		Name: "Divider",
		WithParts: func(s Classes, parts Parts) render.HTML {
			return Divider(DividerProps{Label: "or", Parts: parts}, s)
		},
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

// mods adds one class-map variant per modifier, keyed "<axis>-<value>".
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
		Name: "Section",
		WithParts: func(s Classes, parts Parts) render.HTML {
			return Section(SectionProps{Title: "Volumes", Parts: parts}, s, render.HTML("<p>Two.</p>"))
		},
		Anatomy: []Part{PartRoot, PartSectionBrow, PartTitle, PartDesc, PartHeader, PartSectionHead, PartSectionBody, PartFooter},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "named",
				Why:  "a name is what makes it a landmark, and the name comes from the heading itself rather than a second string that can drift from it",
				HTML: Section(SectionProps{Title: "Volumes", Description: "Disks attached to this node.",
					Actions: Button(ButtonProps{Label: "Add", Variant: "secondary"}, k.For("Button"))}, s,
					render.HTML("<p>Two volumes.</p>")),
			}, {
				Name: "an eyebrow",
				Why:  "the kicker above the title is hidden from the tree: it decorates a heading the reader has already heard, and reading the section's name twice is the alternative",
				HTML: Section(SectionProps{Eyebrow: "01 / Overview", Title: "The cluster"}, s,
					render.HTML("<p>One leader, two workers.</p>")),
			}, {
				Name: "named without a heading",
				Why:  "a name is what makes it a landmark, and a label the page already carries can name a region whose heading lives somewhere else",
				HTML: Section(SectionProps{Label: "Settings"}, s, render.HTML("<p>Loose but named.</p>")),
			}, {
				Name: "unnamed",
				Why:  "no title and no label means a plain div: a section with no name is a row in the landmark list that says nothing, which is worse than not being in it",
				HTML: Section(SectionProps{}, s, render.HTML("<p>Loose content.</p>")),
			}, {
				Name: "deeper in the outline",
				Why:  "the level is the caller's decision — a section under an h2 that renders an h2 leaves a hole a screen reader user has to guess at",
				HTML: Section(SectionProps{Title: "Mounts", Level: 3}, s, render.HTML("<p>One.</p>")),
			}}
		},
	})
}

// SpacerProps is the flexible space in a row.
type SpacerProps struct {
	Grow   int    // 1 through 4, rendered as data-hui-grow; anything else panics
	Min    string // floor, a step on the gap scale
	Max    string // ceiling, a step on the gap scale
	Leader bool   // a dotted leader across the space
	Rule   bool   // a hairline rule across the space; refused with Leader

	ID         string
	ExtraAttrs html.Attrs
}

// Spacer renders an empty, aria-hidden <span> that takes the row's slack.
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
