package headless

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Badge / Tag / Toolbar / Pagination / Steps parts. PartRoot is each
// component's outermost element; Badge and Tag share every part they
// have, because a tag is a badge with a dismiss control and the two
// must not be allowed to drift apart.
const (
	PartBadgeDismiss   Part = "badge-dismiss"
	PartToolbarGroup   Part = "toolbar-group"
	PartToolbarLabel   Part = "toolbar-label"
	PartToolbarSpacer  Part = "toolbar-spacer"
	PartToolbarSearch  Part = "toolbar-search"
	PartPagination     Part = "pagination"
	PartPaginationLink Part = "pagination-link"
	PartPaginationGap  Part = "pagination-gap"
	PartStep           Part = "step"
	PartStepRow        Part = "step-row"
	PartStepText       Part = "step-text"
	PartStepHint       Part = "step-hint"
)

// ─── Badge ──────────────────────────────────────────────────────────

// BadgeTone selects a badge's colour. It is class-map vocabulary: the
// structure does not care, the class map looks it up.
type BadgeTone string

// BadgeProps is a badge: a small status chip, not a fill.
// A badge has no tone of its own and says none: its label IS its
// meaning ("running", "3 unread", "beta"), and the tone a class map
// variant paints it is decoration for the word already there. An Alert prefixes its tone because its title
// may not say it; a badge whose colour means something its label does
// not say has the wrong label.
type BadgeProps struct {
	// Label is the visible text, and the whole of what a screen reader
	// hears. Required.
	Label string
	// Icon renders before the label, and is decorative: the label is
	// the accessible name, so an icon that repeated it would be read
	// twice. Anything the icon means that the label does not say
	// belongs in the label.
	Icon render.HTML
	ID   string

	ExtraAttrs html.Attrs

	// Parts: attrs and binds on the root and the icon.
	Parts Parts
}

// Badge renders a badge.
func Badge(p BadgeProps, s Classes) render.HTML {
	if p.Label == "" {
		panic("headless: Badge requires Label")
	}
	b := p.Parts.Box(s)
	kids := make([]render.HTML, 0, 2)
	if p.Icon != "" {
		kids = append(kids, b.El("span", PartIcon,
			Attrs(map[string]string{"aria-hidden": "true"}), p.Icon))
	}
	kids = append(kids, render.Text(p.Label))
	return b.El("span", PartRoot,
		Merge(Safe(p.ExtraAttrs), Attrs(map[string]string{"id": p.ID})),
		kids...)
}

// ─── Tag ────────────────────────────────────────────────────────────

// TagProps is a tag: a badge-shaped chip standing in for an active
// filter, removable in place.
type TagProps struct {
	// Label is the visible text. Required.
	Label string
	// Icon renders before the label.
	Icon render.HTML
	// DismissHref adds the dismiss control, an × that keeps a real
	// href: removing a filter is the server's decision, and the href
	// is the dismiss that needs no script.
	DismissHref string
	// Island is where the dismiss goes with script: removing a filter
	// is an in-page state change, so the × carries the RPC contract
	// beside its href — the page without script, the region update
	// with it, and the URL written after the swap. Required when
	// DismissHref is set; ignored otherwise.
	Island Island
	// DismissAriaLabel names the × for screen readers. Defaults to
	// "Remove <Label>".
	DismissAriaLabel string

	ID         string
	ExtraAttrs html.Attrs

	// Parts: attrs and binds on the root and the dismiss control.
	// Strings carry the dismiss control's name.
	Parts Parts
	// Strings are the strings this component says. Nil means the English
	// defaults; a layer above sets them from the request's language.
	Strings *Strings
}

// Tag renders a chip, optionally dismissible.
func Tag(p TagProps, s Classes) render.HTML {
	b := p.Parts.Box(s)
	if p.Label == "" {
		panic("headless: Tag requires Label")
	}
	kids := []render.HTML{}
	if p.Icon != "" {
		kids = append(kids, b.El("span", PartIcon,
			Attrs(map[string]string{"aria-hidden": "true"}), p.Icon))
	}
	kids = append(kids, render.Text(p.Label))
	if p.DismissHref != "" {
		aria := p.DismissAriaLabel
		if aria == "" {
			aria = fmt.Sprintf(p.Strings.Resolve().RemoveLabelled, p.Label)
		}
		requireIsland("Tag with DismissHref", p.Island)
		if urlsafe.CleanAnchor(p.DismissHref) == "" {
			panic("headless: Tag DismissHref " + strconv.Quote(p.DismissHref) + " is not a URL the anchor policy allows")
		}
		dismiss := Attrs(map[string]string{
			"href":       p.DismissHref,
			"aria-label": aria,
		})
		// The same element is both destinations: the href is the page
		// without script, the island contract is the region update
		// with it.
		dismiss = Merge(dismiss, p.Island.attrs(p.DismissHref, "GET"))
		kids = append(kids, b.El("a", PartBadgeDismiss, dismiss, render.Text("×")))
	}

	return b.El("span", PartRoot,
		Merge(Safe(p.ExtraAttrs), Attrs(map[string]string{"id": p.ID})),
		kids...)
}

// ─── Toolbar ────────────────────────────────────────────────────────

// ToolbarProps configures a toolbar. The toolbar carries no role of
// its own: role="toolbar" promises arrow-key roving that this
// script-free package cannot implement, and an unfulfilled promise is
// worse than none. A caller who ships the keyboard handling can add it
// through ExtraAttrs.
type ToolbarProps struct {
	ID         string
	ExtraAttrs html.Attrs
}

// Toolbar renders a row of controls that aligns by construction: every
// child is control-height, so nothing needs aligning to anything else.
// Build children from ToolbarGroup, ToolbarSpacer and ToolbarSearch.
func Toolbar(p ToolbarProps, s Classes, children ...render.HTML) render.HTML {
	return El("div", s, PartRoot,
		Merge(Safe(p.ExtraAttrs), Attrs(map[string]string{"id": p.ID})),
		children...)
}

// ToolbarGroup clusters related controls under a visible label. An
// empty label omits the label span; the cluster remains.
func ToolbarGroup(s Classes, label string, children ...render.HTML) render.HTML {
	kids := make([]render.HTML, 0, len(children)+1)
	if label != "" {
		kids = append(kids, El("span", s, PartToolbarLabel, nil, render.Text(label)))
	}
	kids = append(kids, children...)
	return El("div", s, PartToolbarGroup, nil, kids...)
}

// ToolbarSpacer pushes everything after it to the far end of the row.
func ToolbarSpacer(s Classes) render.HTML {
	return El("div", s, PartToolbarSpacer, nil)
}

// ToolbarSearchProps is the toolbar's search field: the one child
// allowed to take the row's slack, and the GET form that makes a
// search an island update rather than a navigation. The form carries
// no action, so without script it submits to the page it is on —
// which is the search's own URL — and no push-state, because the
// canonical URL is the server's to name in X-Gofastr-Push-State once
// it knows what matched.
type ToolbarSearchProps struct {
	// Island is required: a search refilters the list the toolbar
	// sits on, which is an in-page state change — never a route.
	Island Island
}

// ToolbarSearch wraps the search field, the one child allowed to take
// the row's slack, in the GET form that submits it.
func ToolbarSearch(p ToolbarSearchProps, s Classes, child render.HTML) render.HTML {
	requireIsland("ToolbarSearch", p.Island)
	return El("form", s, PartToolbarSearch,
		Merge(Attrs(map[string]string{"method": "get"}), p.Island.attrs("", "GET")),
		child)
}

// ─── Pagination ─────────────────────────────────────────────────────

// PaginationProps configures a pagination bar.
type PaginationProps struct {
	// AriaLabel names the nav landmark for AT. Required: pagination
	// announced as "pagination" is how AT users find it at all.
	AriaLabel string
	// Page is the current page, 1-based. Must be within 1..Pages.
	Page int
	// Pages is the total number of pages. At least 1.
	Pages int

	// Path is the screen's own path: each page href is it plus the
	// carried query, the page parameter replaced. Empty means the
	// current document — a relative "?query" href. When set it must
	// be same-origin and carry no query or fragment of its own: the
	// carry belongs in Query, where it survives the page turn, and a
	// Path query is silently replaced — a value lost with no error is
	// the defect this component exists to make structural.
	Path string
	// Query is the query the screen's URL already carries — the
	// search, the filters, the sort — and survives a page turn
	// beside the page number.
	Query url.Values
	// PageParam names the page query parameter. It defaults to "p".
	PageParam string

	// Window is the number of pages shown each side of the current
	// one, the first and last always shown. Default 1; a Window
	// large enough shows every page.
	Window int
	// OmitPrevNext drops the Previous and Next anchors entirely.
	OmitPrevNext bool

	// PrevLabel and NextLabel default to "Previous" and "Next".
	PrevLabel string
	NextLabel string

	// Island is where a page change goes when the pager sits inside
	// a region rather than being the page: the page anchors then
	// carry the RPC contract beside their hrefs — the page without
	// script, the region update with it, the URL written after the
	// swap. Optional, the Table posture: the URL is the truth for a
	// list, so a list screen's page anchors are plain navigations
	// the client router intercepts. An Island that looks wired and
	// is not is refused, whichever shape the pager renders.
	Island Island

	ID         string
	ExtraAttrs html.Attrs

	// Parts: attrs and binds on the root, the list and the gaps.
	// Strings carry the two end links.
	Parts Parts
	// Strings are the strings this component says. Nil means the English
	// defaults; a layer above sets them from the request's language.
	Strings *Strings
}

// Pagination renders prev / numbered pages / next. The current page
// carries aria-current="page" and the stylesheet styles the attribute,
// so state and appearance cannot disagree. Prev and next at the ends
// are disabled anchors: visible, named, and out of the tab order
// rather than gone.
//
// The nav landmark itself is PartRoot and carries no class; the list
// inside it is PartPagination, which is where a caller's Class has
// always landed.
func Pagination(p PaginationProps, s Classes) render.HTML {
	b := p.Parts.Box(s)
	if p.AriaLabel == "" {
		panic("headless: Pagination requires AriaLabel")
	}
	if !p.Island.zero() {
		p.Island.check()
	}
	if p.Path != "" {
		checkSameOrigin("Pagination", "Path", p.Path)
		if strings.ContainsAny(p.Path, "?#") {
			panic("headless: Pagination Path " + strconv.Quote(p.Path) +
				" carries its own query or fragment — the carry belongs in Query, where it survives the page turn; a Path query is silently replaced, and a value lost with no error is the defect this component exists to make structural")
		}
	}
	if p.Pages < 1 {
		panic("headless: Pagination requires Pages >= 1")
	}
	if p.Page < 1 || p.Page > p.Pages {
		panic("headless: Pagination Page " + strconv.Itoa(p.Page) + " outside 1.." + strconv.Itoa(p.Pages))
	}
	w := p.Strings.Resolve()
	prev := orDefault(p.PrevLabel, w.Previous)
	next := orDefault(p.NextLabel, w.Next)

	links := make([]render.HTML, 0, 10)
	if !p.OmitPrevNext {
		links = append(links, paginationLink(b, p, p.Page-1, prev, p.Page == 1, false))
	}
	for _, n := range pageNumbers(p.Pages, p.Page, max(p.Window, 1)) {
		if n == 0 {
			links = append(links, b.El("span", PartPaginationGap,
				Attrs(map[string]string{"aria-hidden": "true"}),
				render.Text("…")))
			continue
		}
		links = append(links, paginationLink(b, p, n, strconv.Itoa(n), false, n == p.Page))
	}
	if !p.OmitPrevNext {
		links = append(links, paginationLink(b, p, p.Page+1, next, p.Page == p.Pages, false))
	}

	return b.El("nav", PartRoot,
		Merge(Safe(p.ExtraAttrs), Attrs(map[string]string{
			"aria-label": p.AriaLabel,
			"id":         p.ID,
		})),
		b.El("div", PartPagination, nil, links...))
}

// paginationLink renders one pagination anchor. Disabled end links
// stay anchors that say aria-disabled and keep out of the tab order —
// the same posture as a disabled Button anchor, and better than a
// link that silently navigates to the page you are on. It renders
// through the caller's Box, because the link is a named part: the
// attrs and binds a caller sets on PartPaginationLink land here or
// they land nowhere.
func paginationLink(b Box, p PaginationProps, page int, label string, disabled, current bool) render.HTML {
	attrs := html.Attrs{}
	if current {
		attrs["aria-current"] = "page"
	}
	if disabled {
		attrs["aria-disabled"] = "true"
		attrs["tabindex"] = "-1"
	} else {
		href := pageHref(p, page)
		attrs["href"] = href
		if !p.Island.zero() {
			// The same element is both destinations: the href is the
			// page without script, the island contract is the region
			// update with it, and the query is shared so the two
			// answer one question.
			attrs = Merge(attrs, p.Island.attrs(href, "GET"))
			// The page number the table module records on click and
			// looks for in the swapped-in pager. Island anchors only:
			// a plain pager's page is a navigation the router already
			// focuses, and a hook nothing reads on it is markup
			// carried for no one.
			attrs["data-hui-page"] = strconv.Itoa(page)
		}
	}
	return b.El("a", PartPaginationLink, attrs, render.Text(label))
}

// pageHref builds one page anchor's href: the carried query with the
// page parameter replaced, on p.Path. Built through net/url and never
// by substitution; the carried query is scrubbed, not refused, exactly
// as the table's is.
func pageHref(p PaginationProps, page int) string {
	q := scrubbedQuery(p.Query)
	// Set replaces, and that is the contract: the query a screen
	// carries may still hold the last page, and p=2&p=3 is two
	// answers to one question. Add would append; Set does not.
	q.Set(orDefault(p.PageParam, "p"), strconv.Itoa(page))

	href := "?" + q.Encode()
	if p.Path != "" {
		u, err := url.Parse(p.Path)
		if err != nil {
			panic("headless: Pagination Path " + strconv.Quote(p.Path) + " does not parse as a URL: " + err.Error())
		}
		u.RawQuery = q.Encode()
		href = u.String()
	}
	// Every href this package writes goes through the anchor policy;
	// same-origin and the scrub have refused what a refusal is for.
	if urlsafe.CleanAnchor(href) == "" {
		panic("headless: Pagination Path " + strconv.Quote(p.Path) + " is not a URL the anchor policy allows")
	}
	return href
}

// pageNumbers picks which page numbers to render: the first and last
// page always, a window of the given size around the current page, and
// a 0 marking each elision. Pages small enough to show whole are shown
// whole. It is the core pattern's algorithm, moved.
func pageNumbers(pages, page, window int) []int {
	if pages <= 7+2*(window-1) {
		out := make([]int, pages)
		for i := range out {
			out[i] = i + 1
		}
		return out
	}
	left := page - window
	right := page + window
	if left < 2 {
		left = 2
	}
	if right > pages-1 {
		right = pages - 1
	}
	out := []int{1}
	if left > 2 {
		out = append(out, 0)
	}
	for n := left; n <= right; n++ {
		out = append(out, n)
	}
	if right < pages-1 {
		out = append(out, 0)
	}
	out = append(out, pages)
	return out
}

// ─── Steps ──────────────────────────────────────────────────────────

// Step is one entry on a Steps rail.
type Step struct {
	// Label is the step's name. Required.
	Label string
	// Hint is the supporting line under the label.
	Hint string
	// Href makes the step a link — a completed step the reader can go
	// back to. Every href goes through the anchor policy; one the
	// policy refuses is the developer's mistake, said at render like
	// every configured href in this package.
	Href string
	// Marker overrides the glyph in the marker circle — a zero-padded
	// number ("01"), a caller's check icon. It is aria-hidden either
	Marker render.HTML
	// State overrides the state derived from Current: "done",
	// "current" or "todo". Empty derives. An explicit state is how a
	// rail says a later step finished while an earlier one is still
	// open.
	State string
}

// StepsProps configures a step indicator.
type StepsProps struct {
	// Steps are the entries in order. At least one.
	Steps []Step
	// Current is the 1-based step in progress: the steps before it are
	// done, it is current, the rest are todo. 0 means nothing has
	// started yet. Must not exceed len(Steps).
	Current int

	ID         string
	ExtraAttrs html.Attrs
}

// Steps renders a progress rail — a list of states, not a set of
// controls. Each step's data-state drives both its marker and the
// connecting line the stylesheet draws between markers, so the rail
// cannot disagree with the states; the current step also carries
// aria-current="step" for AT. Exactly one step may be current: an
// explicit State "current" on a step other than Current's is
// refused, because two current steps tell assistive technology the
// flow is in two places at once.
func Steps(p StepsProps, s Classes) render.HTML {
	if len(p.Steps) == 0 {
		panic("headless: Steps requires Steps")
	}
	if p.Current < 0 || p.Current > len(p.Steps) {
		panic("headless: Steps Current " + strconv.Itoa(p.Current) + " outside 0.." + strconv.Itoa(len(p.Steps)))
	}
	// Exactly one current step: an explicit State "current" names the
	// step it marks, and Current names another, so the two are only
	// allowed to agree.
	explicit := -1
	for i, st := range p.Steps {
		if st.State == "current" {
			if explicit >= 0 {
				panic("headless: Steps marks steps " + strconv.Itoa(explicit+1) + " and " +
					strconv.Itoa(i+1) + " current — exactly one step may be current")
			}
			explicit = i
		}
	}
	if explicit >= 0 && p.Current > 0 && p.Current != explicit+1 {
		panic("headless: Steps Current is " + strconv.Itoa(p.Current) + " and step " +
			strconv.Itoa(explicit+1) + " says State current — exactly one step may be current")
	}
	items := make([]render.HTML, 0, len(p.Steps))
	for i, st := range p.Steps {
		if st.Label == "" {
			panic("headless: Step requires Label")
		}
		state, current := "todo", false
		switch {
		case i+1 < p.Current:
			state = "done"
		case i+1 == p.Current:
			state, current = "current", true
		}
		if st.State != "" {
			switch st.State {
			case "done", "current", "todo":
				state = st.State
			default:
				panic("headless: Step State " + strconv.Quote(st.State) + ` is not one of: "" (derived), done, current, todo`)
			}
		}
		current = state == "current"
		marker := st.Marker
		if marker == "" {
			if state == "done" {
				marker = render.Text("✓")
			} else {
				marker = render.Text(strconv.Itoa(i + 1))
			}
		}
		attrs := Attrs(map[string]string{"data-state": state})
		if current {
			attrs["aria-current"] = "step"
		}
		text := []render.HTML{El("span", s, PartLabel, nil, render.Text(st.Label))}
		if st.Hint != "" {
			text = append(text, El("span", s, PartStepHint, nil, render.Text(st.Hint)))
		}
		rowAttrs := Attrs(nil)
		tag := "span"
		if st.Href != "" {
			href := urlsafe.CleanAnchor(st.Href)
			if href == "" {
				panic("headless: Step Href " + strconv.Quote(st.Href) + " is not a URL the anchor policy allows")
			}
			tag = "a"
			rowAttrs["href"] = href
		}
		items = append(items, El("li", s, PartStep, attrs,
			El(tag, s, PartStepRow, rowAttrs,
				El("span", s, PartMarker,
					Attrs(map[string]string{"aria-hidden": "true"}), marker),
				El("span", s, PartStepText, nil, text...),
			),
		))
	}

	return El("ol", s, PartRoot,
		Merge(Safe(p.ExtraAttrs), Attrs(map[string]string{"id": p.ID})),
		items...)
}

func init() {
	Register(Spec{
		Name: "Badge",
		WithParts: func(s Classes, parts Parts) render.HTML {
			return Badge(BadgeProps{Label: "running", Parts: parts}, s)
		},
		Anatomy: []Part{PartRoot, PartIcon},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "plain",
				Why:  "a badge is a word, and the word is the accessible name — which is why the icon beside it is hidden rather than read twice",
				HTML: Badge(BadgeProps{Label: "running"}, s),
			}, {
				Name: "with an icon",
				Why:  "the icon adds emphasis and no meaning: anything it means that the label does not say belongs in the label",
				HTML: Badge(BadgeProps{Label: "degraded", Icon: SpecimenGlyph}, s),
			}}
		},
	})

	Register(Spec{
		Name:    "Tag",
		Anatomy: []Part{PartRoot, PartIcon, PartBadgeDismiss},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return Tag(TagProps{Label: "env=prod", Parts: parts}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "removable",
				Why:  "the × names what it removes — twelve controls all called Remove tell a screen reader user nothing — and it is an anchor that keeps its href for no script while carrying the island contract, because dropping a filter is an in-page state change and never a route",
				HTML: Tag(TagProps{Label: "env=prod", DismissHref: "/apps?env=",
					Island: Island{Endpoint: "/island/apps", Signal: "apps"}}, s),
			}, {
				Name: "fixed",
				Why:  "a tag with nothing to navigate to renders no control at all rather than a dead ×, and its icon is hidden from assistive technology exactly as a badge's is, because a tag is a badge with a dismiss control and the two must not drift",
				HTML: Tag(TagProps{Label: "leader", Icon: SpecimenGlyph}, s),
			}}
		},
	})

	Register(Spec{
		Name:    "Pagination",
		Anatomy: []Part{PartRoot, PartPagination, PartPaginationLink, PartPaginationGap},
		Hooks:   []string{"data-hui-page"},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return Pagination(PaginationProps{Page: 1, Pages: 2, Path: "/apps",
				AriaLabel: "Pages", Island: Island{Endpoint: "/island/apps", Signal: "apps"},
				Parts: parts}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "plain, on a list screen",
				Why:  "the URL is the truth for a list and a pager on it is list state: the page anchors are plain navigations the client router intercepts, and no data-hui-page renders without an Island — a hook nothing reads is markup carried for no one",
				HTML: Pagination(PaginationProps{Page: 2, Pages: 3, Path: "/apps",
					AriaLabel: "Application pages, plain"}, s),
			}, {
				Name: "middle of a long run, an island pager",
				Why:  "the gap is a span and not a link, the current page says aria-current=page rather than being told apart by weight, and an embedded pager's anchors carry the RPC contract beside their hrefs — the page without script, the region update with it — plus the data-hui-page the module restores focus through",
				HTML: Pagination(PaginationProps{Page: 5, Pages: 12, Path: "/apps",
					AriaLabel: "Application pages, middle",
					Island:    Island{Endpoint: "/island/apps", Signal: "apps"}}, s),
			}, {
				Name: "at the first page",
				Why:  "Previous stays visible and named with no href — removing it moves every other control one place left — and a disabled end carries no island contract, because a control that goes nowhere fires nothing",
				HTML: Pagination(PaginationProps{Page: 1, Pages: 3, Path: "/apps",
					AriaLabel: "Application pages, first",
					Island:    Island{Endpoint: "/island/apps", Signal: "apps"}}, s),
			}}
		},
	})
	Register(Spec{
		Name:    "Steps",
		Anatomy: []Part{PartRoot, PartStep, PartStepRow, PartMarker, PartStepText, PartLabel, PartStepHint},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "part way",
				Why:  "a rail of states, not a set of controls: each step's data-state drives both its marker and the line drawn between markers, so the picture cannot disagree with the states",
				HTML: Steps(StepsProps{Steps: []Step{
					{Label: "Source"}, {Label: "Build", Hint: "About a minute"}, {Label: "Deploy"},
				}, Current: 2}, s),
			}, {
				Name: "not started",
				Why:  "zero is a real value — nothing has begun — and is not the same as being on the first step",
				HTML: Steps(StepsProps{Steps: []Step{{Label: "Source"}, {Label: "Build"}, {Label: "Deploy"}}}, s),
			}, {
				Name: "a completed step links back",
				Why:  "a step that is done is somewhere the reader may want to return to, and the link keeps its href for a reader with no script",
				HTML: Steps(StepsProps{Steps: []Step{
					{Label: "Source", Href: "/setup/source", Marker: "01"},
					{Label: "Build", Marker: "02"},
				}, Current: 2}, s),
			}}
		},
	})
	Register(Spec{
		Name:    "Toolbar",
		Anatomy: []Part{PartRoot},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "controls in a row",
				Why:  "no role=toolbar: that role promises arrow-key roving with a single tab stop, and a promise a script-free component cannot keep is worse than saying nothing",
				HTML: Toolbar(ToolbarProps{}, s,
					Button(ButtonProps{Label: "New app", Variant: "secondary"}, k.For("Button")),
					Button(ButtonProps{Label: "Refresh", Variant: "secondary"}, k.For("Button"))),
			}}
		},
	})

	Register(Spec{
		Name:    "ToolbarGroup",
		Anatomy: []Part{PartToolbarGroup, PartToolbarLabel},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "labelled group",
				Why:  "the label names what the group of controls acts on, so the controls inside can stay short without becoming ambiguous",
				HTML: ToolbarGroup(s, "Sort", Button(ButtonProps{Label: "Name", Variant: "secondary"}, k.For("Button"))),
			}}
		},
	})

	Register(Spec{
		Name:    "ToolbarSpacer",
		Anatomy: []Part{PartToolbarSpacer},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "gap",
				Why:  "an empty element that pushes what follows to the far end — it renders nothing, says nothing, and is in the markup only because the layout needs a thing to grow",
				HTML: ToolbarSpacer(s),
			}}
		},
	})

	Register(Spec{
		Name:    "ToolbarSearch",
		Anatomy: []Part{PartToolbarSearch},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "search slot",
				Why:  "the search field is the one control in a toolbar that should grow, so its wrapper is the GET form that submits it — an island update with script, the page's own query without",
				HTML: ToolbarSearch(ToolbarSearchProps{Island: Island{Endpoint: "/island/apps", Signal: "apps"}}, s,
					Input(InputProps{Type: "search", Name: "q", AriaLabel: "Search apps"}, k.For("Input"))),
			}}
		},
	})
}
