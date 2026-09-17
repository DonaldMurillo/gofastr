package headless

import (
	"fmt"
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
}

// Badge renders a badge.
func Badge(p BadgeProps, s Classes) render.HTML {
	if p.Label == "" {
		panic("headless: Badge requires Label")
	}
	kids := make([]render.HTML, 0, 2)
	if p.Icon != "" {
		kids = append(kids, El("span", s, PartIcon,
			Attrs(map[string]string{"aria-hidden": "true"}), p.Icon))
	}
	kids = append(kids, render.Text(p.Label))
	return El("span", s, PartRoot,
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
	// HrefPattern is the href for every page number: the literal "%d"
	// in it is replaced with the page number. The substitution is a
	// string replacement rather than fmt, so a pattern carrying other
	// text cannot be reinterpreted as a verb.
	HrefPattern string
	// PrevLabel and NextLabel default to "Previous" and "Next".
	PrevLabel string
	NextLabel string

	// Island is where a page change goes: turning a page is an
	// in-page state change, so every page anchor carries the RPC
	// contract beside its href — the page without script, the island
	// update with it, and the URL written after the swap. Required.
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
	if p.HrefPattern == "" {
		panic("headless: Pagination requires HrefPattern")
	}
	// The page number is substituted as text, not formatted, so the
	// token is the literal "%d": without it every anchor is the same
	// URL and no page can be told from another. The pattern passes the
	// anchor policy once, here, so no substituted href needs to.
	if !strings.Contains(p.HrefPattern, "%d") {
		panic("headless: Pagination HrefPattern " + strconv.Quote(p.HrefPattern) + " has no %d for the page number")
	}
	if urlsafe.CleanAnchor(p.HrefPattern) == "" {
		panic("headless: Pagination HrefPattern " + strconv.Quote(p.HrefPattern) + " is not a URL the anchor policy allows")
	}
	if p.Pages < 1 {
		panic("headless: Pagination requires Pages >= 1")
	}
	if p.Page < 1 || p.Page > p.Pages {
		panic("headless: Pagination Page " + strconv.Itoa(p.Page) + " outside 1.." + strconv.Itoa(p.Pages))
	}
	requireIsland("Pagination", p.Island)
	w := p.Strings.Resolve()
	prev := orDefault(p.PrevLabel, w.Previous)
	next := orDefault(p.NextLabel, w.Next)

	links := make([]render.HTML, 0, 10)
	links = append(links, paginationLink(b, p.Island, p.Page-1, prev, p.HrefPattern, p.Page == 1, false))
	for _, n := range pageWindow(p.Page, p.Pages) {
		if n == 0 {
			links = append(links, b.El("span", PartPaginationGap,
				Attrs(map[string]string{"aria-hidden": "true"}),
				render.Text("…")))
			continue
		}
		links = append(links, paginationLink(b, p.Island, n, strconv.Itoa(n), p.HrefPattern, false, n == p.Page))
	}
	links = append(links, paginationLink(b, p.Island, p.Page+1, next, p.HrefPattern, p.Page == p.Pages, false))

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
func paginationLink(b Box, isle Island, page int, label, pattern string, disabled, current bool) render.HTML {
	attrs := html.Attrs{}
	if current {
		attrs["aria-current"] = "page"
	}
	if disabled {
		attrs["aria-disabled"] = "true"
		attrs["tabindex"] = "-1"
	} else {
		href := strings.ReplaceAll(pattern, "%d", strconv.Itoa(page))
		attrs["href"] = href
		// The same element is both destinations: the href is the page
		// without script, the island contract is the region update with
		// it, and the query is shared so the two answer one question.
		attrs = Merge(attrs, isle.attrs(href, "GET"))
	}
	return b.El("a", PartPaginationLink, attrs, render.Text(label))
}

// pageWindow picks which page numbers to render: the first and last
// page always, a window around the current page, and a 0 marking each
// elision. Pages small enough to show whole are shown whole.
func pageWindow(page, pages int) []int {
	if pages <= 7 {
		out := make([]int, 0, pages)
		for p := 1; p <= pages; p++ {
			out = append(out, p)
		}
		return out
	}
	nums := make([]int, 0, 9)
	addRange := func(from, to int) {
		if from < 1 {
			from = 1
		}
		if to > pages {
			to = pages
		}
		if len(nums) > 0 {
			if last := nums[len(nums)-1]; from <= last {
				from = last + 1
			}
			if from > to {
				return
			}
			if from > nums[len(nums)-1]+1 {
				nums = append(nums, 0)
			}
		}
		for p := from; p <= to; p++ {
			nums = append(nums, p)
		}
	}
	addRange(1, 1)
	switch {
	case page <= 4:
		addRange(2, 5)
	case page >= pages-3:
		addRange(pages-4, pages)
	default:
		addRange(page-1, page+1)
	}
	addRange(pages, pages)
	return nums
}

// ─── Steps ──────────────────────────────────────────────────────────

// StepsProps configures a step indicator.
type StepsProps struct {
	// Labels lists the step names in order. At least one.
	Labels []string
	// Current is the 1-based step in progress: the steps before it are
	// done, it is current, the rest are todo. 0 means nothing has
	// started yet. Must not exceed len(Labels).
	Current int

	ID         string
	ExtraAttrs html.Attrs
}

// Steps renders a progress rail — a list of states, not a set of
// controls. Each step's data-state drives both its marker and the
// connecting line the stylesheet draws between markers, so the rail
// cannot disagree with the states; the current step also carries
// aria-current="step" for AT.
func Steps(p StepsProps, s Classes) render.HTML {
	if len(p.Labels) == 0 {
		panic("headless: Steps requires Labels")
	}
	if p.Current < 0 || p.Current > len(p.Labels) {
		panic("headless: Steps Current " + strconv.Itoa(p.Current) + " outside 0.." + strconv.Itoa(len(p.Labels)))
	}

	items := make([]render.HTML, 0, len(p.Labels))
	for i, label := range p.Labels {
		state, marker := "todo", strconv.Itoa(i+1)
		current := false
		switch {
		case i+1 < p.Current:
			state, marker = "done", "✓"
		case i+1 == p.Current:
			state, current = "current", true
		}
		attrs := Attrs(map[string]string{"data-state": state})
		if current {
			attrs["aria-current"] = "step"
		}
		items = append(items, El("li", s, PartStep, attrs,
			El("span", s, PartMarker, nil, render.Text(marker)),
			El("span", s, PartLabel, nil, render.Text(label)),
		))
	}

	return El("ol", s, PartRoot,
		Merge(Safe(p.ExtraAttrs), Attrs(map[string]string{"id": p.ID})),
		items...)
}

func init() {
	Register(Spec{
		Name:    "Badge",
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
		WithParts: func(s Classes, parts Parts) render.HTML {
			return Pagination(PaginationProps{Page: 1, Pages: 2, HrefPattern: "/apps?page=%d",
				AriaLabel: "Pages", Island: Island{Endpoint: "/island/apps", Signal: "apps"},
				Parts: parts}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "middle of a long run",
				Why:  "the gap is a span and not a link, the current page says aria-current=page rather than being told apart by weight, and a page change is an island update rather than a route: every anchor keeps its href for no script and carries the RPC contract beside it",
				HTML: Pagination(PaginationProps{Page: 5, Pages: 12, HrefPattern: "/apps?page=%d",
					AriaLabel: "Application pages, middle",
					Island:    Island{Endpoint: "/island/apps", Signal: "apps"}}, s),
			}, {
				Name: "at the first page",
				Why:  "Previous stays visible and named with no href — removing it moves every other control one place left — and a disabled end carries no island contract, because a control that goes nowhere fires nothing",
				HTML: Pagination(PaginationProps{Page: 1, Pages: 3, HrefPattern: "/apps?page=%d",
					AriaLabel: "Application pages, first",
					Island:    Island{Endpoint: "/island/apps", Signal: "apps"}}, s),
			}}
		},
	})

	Register(Spec{
		Name:    "Steps",
		Anatomy: []Part{PartRoot, PartStep, PartMarker, PartLabel},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "part way",
				Why:  "a rail of states, not a set of controls: each step's data-state drives both its marker and the line drawn between markers, so the picture cannot disagree with the states",
				HTML: Steps(StepsProps{Labels: []string{"Source", "Build", "Deploy"}, Current: 2}, s),
			}, {
				Name: "not started",
				Why:  "zero is a real value — nothing has begun — and is not the same as being on the first step",
				HTML: Steps(StepsProps{Labels: []string{"Source", "Build", "Deploy"}}, s),
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
