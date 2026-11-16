package headless

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/render"
)

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

// BadgeTone selects a badge's colour; only the class map reads it.
type BadgeTone string

// BadgeProps is a badge: a status chip whose label is its whole meaning.
type BadgeProps struct {
	// Label is the visible text and the accessible name. Required.
	Label string
	// Icon renders before the label, aria-hidden.
	Icon render.HTML
	ID   string

	ExtraAttrs html.Attrs

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

// TagProps is a badge-shaped chip standing in for an active filter, removable in place.
type TagProps struct {
	// Label is the visible text. Required.
	Label string
	// Icon renders before the label, aria-hidden.
	Icon render.HTML
	// DismissHref adds the × dismiss anchor, the removal that needs no script.
	DismissHref string
	// Island carries the dismiss with script. Required with DismissHref.
	Island Island
	// DismissMethod is the dismiss request's method: GET (empty), POST, PUT, PATCH or DELETE.
	DismissMethod string
	// DismissBody is a static JSON body sent with a DismissMethod other than GET.
	DismissBody string
	// DismissAriaLabel names the ×; empty says "Remove <Label>".
	DismissAriaLabel string
	// Href makes the whole chip an anchor, held to the anchor policy.
	Href string

	ID         string
	ExtraAttrs html.Attrs

	Parts   Parts
	Strings *Strings
}

// Tag renders a chip, optionally dismissible.
func Tag(p TagProps, s Classes) render.HTML {
	b := p.Parts.Box(s)
	if p.Label == "" {
		panic("headless: Tag requires Label")
	}
	if p.Href != "" {
		if urlsafe.CleanAnchor(p.Href) == "" {
			panic("headless: Tag Href " + strconv.Quote(p.Href) + " is not a URL the anchor policy allows")
		}
	}
	kids := []render.HTML{}
	if p.Icon != "" {
		kids = append(kids, b.El("span", PartIcon,
			Attrs(map[string]string{"aria-hidden": "true"}), p.Icon))
	}
	kids = append(kids, b.El("span", PartLabel, Internal(nil), render.Text(p.Label)))
	if p.DismissHref != "" {
		aria := p.DismissAriaLabel
		if aria == "" {
			w := p.Strings.Resolve()
			aria = w.fill(w.RemoveLabelled, "label", p.Label)
		}
		requireIsland("Tag with DismissHref", p.Island)
		if urlsafe.CleanAnchor(p.DismissHref) == "" {
			panic("headless: Tag DismissHref " + strconv.Quote(p.DismissHref) + " is not a URL the anchor policy allows")
		}
		dismiss := Attrs(map[string]string{
			"href":       p.DismissHref,
			"aria-label": aria,
		})
		method := strings.ToUpper(p.DismissMethod)
		switch method {
		case "":
			method = "GET"
		case "GET", "POST", "PUT", "PATCH", "DELETE":
		default:
			panic("headless: Tag DismissMethod " + strconv.Quote(p.DismissMethod) + " is not a method the runtime sends")
		}
		dismiss = Merge(dismiss, p.Island.attrs(p.DismissHref, method))
		if p.DismissBody != "" {
			if method == "GET" {
				panic("headless: Tag DismissBody needs a DismissMethod other than GET — a GET sends no body")
			}
			if !json.Valid([]byte(p.DismissBody)) {
				panic("headless: Tag DismissBody is not JSON — the runtime sends it verbatim and the server refuses it")
			}
			dismiss["data-cui-rpc-body"] = p.DismissBody
		}
		kids = append(kids, b.El("a", PartBadgeDismiss, Internal(dismiss), render.Text("×")))
	}

	rootAttrs := Merge(Safe(p.ExtraAttrs), Attrs(map[string]string{"id": p.ID}))
	tag := "span"
	if p.Href != "" {
		tag = "a"
		rootAttrs["href"] = urlsafe.CleanAnchor(p.Href)
	}
	return b.El(tag, PartRoot, rootAttrs, kids...)
}

// ToolbarProps configures a toolbar, which claims no role of its own.
type ToolbarProps struct {
	ID         string
	ExtraAttrs html.Attrs
}

// Toolbar renders a row of control-height children: ToolbarGroup, ToolbarSpacer and ToolbarSearch.
func Toolbar(p ToolbarProps, s Classes, children ...render.HTML) render.HTML {
	return El("div", s, PartRoot,
		Merge(Safe(p.ExtraAttrs), Attrs(map[string]string{"id": p.ID})),
		children...)
}

// ToolbarGroup clusters controls under a visible label; an empty label renders none.
func ToolbarGroup(s Classes, label string, children ...render.HTML) render.HTML {
	kids := make([]render.HTML, 0, len(children)+1)
	if label != "" {
		kids = append(kids, El("span", s, PartToolbarLabel, Internal(nil), render.Text(label)))
	}
	kids = append(kids, children...)
	return El("div", s, PartToolbarGroup, nil, kids...)
}

// ToolbarSpacer pushes everything after it to the far end of the row.
func ToolbarSpacer(s Classes) render.HTML {
	return El("div", s, PartToolbarSpacer, nil)
}

// ToolbarSearchProps configures the toolbar's search form.
type ToolbarSearchProps struct {
	// Island is where the search goes. Required.
	Island Island
}

// ToolbarSearch wraps the search field in the GET form that submits it to the Island.
func ToolbarSearch(p ToolbarSearchProps, s Classes, child render.HTML) render.HTML {
	requireIsland("ToolbarSearch", p.Island)
	return El("form", s, PartToolbarSearch,
		Merge(Attrs(map[string]string{"method": "get"}), p.Island.attrs("", "GET")),
		child)
}

// PaginationProps configures a pagination bar.
type PaginationProps struct {
	// AriaLabel names the nav landmark. Required.
	AriaLabel string
	// Page is the current page, 1-based, within 1..Pages.
	Page int
	// Pages is the total number of pages, at least 1.
	Pages int

	// Path is the screen's own same-origin path with no query or fragment; empty is the current document.
	Path string
	// Query is the query the screen's URL carries; it survives a page turn.
	Query url.Values
	// PageParam names the page query parameter; empty is "p".
	PageParam string

	// Window is the number of pages shown each side of the current one; default 1.
	Window int
	// OmitPrevNext drops the Previous and Next anchors entirely.
	OmitPrevNext bool

	// PrevLabel and NextLabel default to "Previous" and "Next".
	PrevLabel string
	NextLabel string

	// Island, optional, makes the page anchors carry the RPC contract beside their hrefs.
	Island Island

	ID         string
	ExtraAttrs html.Attrs

	Parts   Parts
	Strings *Strings
}

// Pagination renders Previous, the numbered pages and Next inside a nav landmark.
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
		links = append(links, paginationLink(b, p, n, w.Number(n), false, n == p.Page))
	}
	if !p.OmitPrevNext {
		links = append(links, paginationLink(b, p, p.Page+1, next, p.Page == p.Pages, false))
	}

	return b.El("nav", PartRoot,
		Merge(Safe(p.ExtraAttrs), Attrs(map[string]string{
			"aria-label": p.AriaLabel,
			"id":         p.ID,
		})),
		b.El("div", PartPagination, Internal(nil), links...))
}

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
			attrs = Merge(attrs, p.Island.attrs(href, "GET"))
			attrs["data-hui-page"] = strconv.Itoa(page)
		}
	}
	return b.El("a", PartPaginationLink, attrs, render.Text(label))
}

func pageHref(p PaginationProps, page int) string {
	q := scrubbedQuery(p.Query)
	q.Set(orDefault(p.PageParam, "p"), strconv.Itoa(page))
	return queryHref("Pagination", p.Path, q)
}

func queryHref(component, path string, q url.Values) string {
	href := "?" + q.Encode()
	if path != "" {
		u, err := url.Parse(path)
		if err != nil {
			panic("headless: " + component + " Path " + strconv.Quote(path) + " does not parse as a URL: " + err.Error())
		}
		u.RawQuery = q.Encode()
		href = u.String()
	}
	if urlsafe.CleanAnchor(href) == "" {
		panic("headless: " + component + " Path " + strconv.Quote(path) + " is not a URL the anchor policy allows")
	}
	return href
}

func pageNumbers(pages, page, window int) []int {
	if window > pages {
		window = pages
	}
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

// Step is one entry on a Steps rail.
type Step struct {
	// Label is the step's name. Required.
	Label string
	// Hint is the supporting line under the label.
	Hint string
	// Href makes the step a link, held to the anchor policy.
	Href string
	// Marker overrides the aria-hidden glyph in the marker circle.
	Marker render.HTML
	// State overrides the state derived from Current: done, current or todo.
	State string
}

// StepsProps configures a step indicator.
type StepsProps struct {
	// Steps are the entries in order. At least one.
	Steps []Step
	// Current is the 1-based step in progress; 0 means none has started.
	Current int

	ID         string
	ExtraAttrs html.Attrs

	Strings *Strings
}

// Steps renders a progress rail of states, of which at most one is current.
func Steps(p StepsProps, s Classes) render.HTML {
	w := p.Strings.Resolve()
	if len(p.Steps) == 0 {
		panic("headless: Steps requires Steps")
	}
	if p.Current < 0 || p.Current > len(p.Steps) {
		panic("headless: Steps Current " + strconv.Itoa(p.Current) + " outside 0.." + strconv.Itoa(len(p.Steps)))
	}
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
				marker = render.Text(w.Number(i + 1))
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
		stepOwn, textOwn := html.Attrs(nil), Internal(nil)
		if st.Marker == "" {
			stepOwn, textOwn = Internal(nil), nil
		}
		items = append(items, El("li", s, PartStep, Merge(attrs, stepOwn),
			El(tag, s, PartStepRow, rowAttrs,
				El("span", s, PartMarker,
					Attrs(map[string]string{"aria-hidden": "true"}), marker),
				El("span", s, PartStepText, textOwn, text...),
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
		Anatomy: []Part{PartRoot, PartIcon, PartLabel, PartBadgeDismiss},
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

// BackToTopProps configures the page's way back to its top.
type BackToTopProps struct {
	// Href is the same-origin anchor the link goes to. Required.
	Href string
	// Target is the id of the element the module scrolls to: an id, not a selector.
	Target string
	// Label is the accessible name; empty takes Strings.BackToTop.
	Label string
	// Icon replaces the default arrow glyph.
	Icon render.HTML
	// Threshold is the scroll offset in CSS pixels past which the link shows; zero is the module's default.
	Threshold int
	// Smooth asks for smooth scrolling, which reduced motion downgrades.
	Smooth bool

	ID         string
	ExtraAttrs html.Attrs

	Parts   Parts
	Strings *Strings
}

// BackToTop renders the jump link the headless-navigation module shows past the threshold.
func BackToTop(p BackToTopProps, s Classes) render.HTML {
	if p.Href == "" {
		panic("headless: BackToTop requires Href — a jump control with no destination is a dead link on the no-script page")
	}
	if urlsafe.CleanAnchor(p.Href) == "" {
		panic("headless: BackToTop Href " + strconv.Quote(p.Href) + " is not a URL the anchor policy allows")
	}
	if p.Target != "" && (strings.ContainsAny(p.Target, " \t\n\r") || strings.HasPrefix(p.Target, "#")) {
		panic("headless: BackToTop Target " + strconv.Quote(p.Target) + " is not an element id — an id, not a selector")
	}
	if p.Threshold < 0 {
		panic("headless: BackToTop Threshold " + strconv.Itoa(p.Threshold) + " is negative — an offset before the top of the page is no threshold")
	}

	b := p.Parts.Box(s)
	own := Merge(Safe(p.ExtraAttrs, "href"), Attrs(map[string]string{
		"href":       urlsafe.CleanAnchor(p.Href),
		"aria-label": orDefault(p.Label, p.Strings.Resolve().BackToTop),
		"id":         p.ID,
	}))
	Mark(own, "data-hui-back-to-top")
	if p.Target != "" {
		own["data-hui-back-to-top-target"] = p.Target
	}
	if p.Threshold > 0 {
		own["data-hui-back-to-top-threshold"] = strconv.Itoa(p.Threshold)
	}
	if p.Smooth {
		own["data-hui-back-to-top-smooth"] = ""
	}
	label := orDefault(p.Label, p.Strings.Resolve().BackToTop)
	icon := p.Icon
	if icon == "" {
		icon = render.Text("↑")
	}
	iconOwn, labelOwn := Attrs(map[string]string{"aria-hidden": "true"}), Internal(nil)
	if p.Icon == "" || ownedSlot(p.Icon) {
		iconOwn = Internal(iconOwn)
	}
	return b.El("a", PartRoot, own,
		b.El("span", PartIcon, iconOwn, icon),
		b.El("span", PartLabel, labelOwn, render.Text(label)),
	)
}

func init() {
	Register(Spec{
		Name:    "BackToTop",
		Anatomy: []Part{PartRoot, PartIcon, PartLabel},
		Hooks:   []string{"data-hui-back-to-top", "data-hui-back-to-top-target", "data-hui-back-to-top-threshold", "data-hui-back-to-top-smooth"},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return BackToTop(BackToTopProps{Href: "#main-content", Label: "Back to top", Parts: parts}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "to the page top",
				Why:  "the anchor is a real same-origin link, so the no-script page jumps exactly where it says; the module only adds the threshold, the scroll and the focus return",
				HTML: BackToTop(BackToTopProps{Href: "#main-content", Label: "Back to top"}, s),
			}, {
				Name: "the default name",
				Why:  "a jump control with no label of its own still names itself — the default word is the one Strings carries, and a translated page says it in the reader's language",
				HTML: BackToTop(BackToTopProps{Href: "#top"}, s),
			}, {
				Name: "an explicit target and threshold",
				Why:  "the target is an element id, never a selector — the module resolves it — and a threshold of zero would be the module's default, so an explicit one is the caller tuning when the link becomes true",
				HTML: BackToTop(BackToTopProps{Href: "#toc", Target: "toc", Label: "Back to contents",
					Threshold: 480, Smooth: true}, s),
			}}
		},
	})
}
