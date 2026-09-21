package ui

import (
	"fmt"
	stdhtml "html"
	"strconv"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// ─── PageHeader ─────────────────────────────────────────────────────

// PageHeaderConfig configures a page-top header.
type PageHeaderConfig struct {
	Title    string      // required
	Subtitle string      // optional supporting text below the title
	Eyebrow  string      // optional small label above the title (e.g. "Customers")
	Actions  render.HTML // optional trailing action slot (button row, link)
	// HeadingLevel overrides the title's heading level (default 1). Set to
	// 2 when the header is a sub-section of a page that already has an <h1>
	// (e.g. a related-list block on a detail page) so the outline doesn't
	// produce a second <h1> or skip levels.
	HeadingLevel int
	Class        string
	ID           string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the header's root <header>
	// element. Keys the component owns are dropped: class and id
	// (use Class / ID), style, data-fui-* and role.
	ExtraAttrs html.Attrs
}

// pageHeaderClasses dresses headless.PageHeader's parts.
var pageHeaderClasses = headless.Classes{
	headless.PartRoot:         "fui-page-header",
	headless.PartPageEyebrow:  "fui-page-header__eyebrow",
	headless.PartTitle:        "fui-page-header__title",
	headless.PartPageSubtitle: "fui-page-header__subtitle",
	headless.PartPageText:     "fui-page-header__text",
	headless.PartPageActions:  "fui-page-header__actions",
}

// PageHeader renders a top-of-page header on headless.PageHeader:
// the title, the words that qualify it, and the page's own actions.
// The element is a plain <header> — claiming role=banner is the
// top-level page header's decision, not the component's.
func PageHeader(cfg PageHeaderConfig) render.HTML {
	return pageHeaderStyle.WrapHTML(headless.PageHeader(headless.PageHeaderProps{
		Title:      cfg.Title,
		Level:      cfg.HeadingLevel,
		Eyebrow:    cfg.Eyebrow,
		Subtitle:   cfg.Subtitle,
		Actions:    cfg.Actions,
		ID:         cfg.ID,
		ExtraAttrs: headless.Safe(cfg.ExtraAttrs, "class", "id", "role"),
		Parts:      rootClassParts(cfg.Class),
	}, pageHeaderClasses))
}

// slug normalizes text into a URL/id-safe slug.
func slug(s string) string {
	out := make([]rune, 0, len(s))
	prevDash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out = append(out, r)
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
	return string(out)
}

// ─── Section ────────────────────────────────────────────────────────

// SectionConfig configures a labelled content section.
//
// ID behaviour, on headless.Section:
//   - If ID is set, it names the root, and the heading's own id
//     derives from it ("<id>-title"), so repeated heading text with
//     distinct IDs never shares a target.
//   - If ID is empty and Heading is set, the section auto-slugs the
//     heading as its id ("Forms" → id="forms"), the scrollspy/rail
//     case, and the heading's id is the slug plus "-title".
//   - If both ID and Heading are empty but Label is set, the region
//     is named by aria-label.
//   - If all three are empty the section renders a plain div: an
//     unnamed section is noise in the landmark list, not a landmark.
type SectionConfig struct {
	// Eyebrow is an optional short decorative kicker rendered above
	// the heading, e.g. a section number ("01 / what it generates").
	// It is marked aria-hidden because it duplicates the heading for
	// SR users.
	Eyebrow     string
	Heading     string // optional <h2> heading
	Description string // optional supporting text under the heading
	// DescriptionHTML lets the supporting text carry inline markup (code,
	// links). When non-empty it takes precedence over Description.
	DescriptionHTML render.HTML
	// Label sets the section's accessible name when there is no
	// Heading, by aria-label.
	Label string
	Class string
	ID    string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers) to the section's root <section> element.
	// Keys the component owns are dropped: class and id (use Class /
	// ID), data-fui-*, and the accessible-name contract (role,
	// aria-label, aria-labelledby).
	ExtraAttrs html.Attrs
}

// sectionClasses dresses headless.Section's parts.
var sectionClasses = headless.Classes{
	headless.PartRoot:        "fui-section",
	headless.PartSectionBrow: "fui-section__eyebrow",
	headless.PartTitle:       "fui-section__heading",
	headless.PartDesc:        "fui-section__description",
	headless.PartHeader:      "fui-section__head",
	headless.PartSectionBody: "fui-section__body",
}

// Section renders a content section on headless.Section: a heading
// names the region through aria-labelledby, a Label names it by
// aria-label when there is no heading, and neither means the region
// renders as a plain div rather than an unnamed landmark.
func Section(cfg SectionConfig, body ...render.HTML) render.HTML {
	sectionID := cfg.ID
	if sectionID == "" && cfg.Heading != "" {
		// Auto-anchor: typical use is in-page rails / scrollspy where the
		// rail's href="#<slug>" should land on this section without the
		// caller having to repeat the slug.
		sectionID = slug(cfg.Heading)
	}
	return sectionStyle.WrapHTML(headless.Section(headless.SectionProps{
		Title:           cfg.Heading,
		Level:           2,
		Eyebrow:         cfg.Eyebrow,
		Description:     cfg.Description,
		DescriptionHTML: cfg.DescriptionHTML,
		Label:           cfg.Label,
		ID:              sectionID,
		ExtraAttrs:      headless.Safe(cfg.ExtraAttrs, "class", "id", "role", "aria-label", "aria-labelledby"),
		Parts:           rootClassParts(cfg.Class),
	}, sectionClasses, body...))
}

// ─── FormField ──────────────────────────────────────────────────────

// FormFieldConfig configures a single form field row.
type FormFieldConfig struct {
	Label    string // required → <label>
	For      string // required → <label for=…> matches the control ID
	Help     string // optional helper text under the field
	Error    string // optional error message; non-empty marks the control invalid
	Required bool   // marks the label and the control

	// Input BUILDS the control from the wiring the field hands it:
	// the id the label points at, the described-by chain, the invalid
	// state and the required flag. It is a builder rather than a
	// pre-built value because the wiring has to reach the control, and
	// a pre-built control is how a hint ends up rendered, given an id,
	// and never referenced — visible on screen and absent to a screen
	// reader. Build the control with ui.Control (any input type), a
	// typed field, ui.PasswordInput (its Field field), or headless
	// directly; a closure that ignores its FieldControl compiles and
	// loses the wiring, which is the one way left to get this wrong.
	Input func(headless.FieldControl) render.HTML

	// ReserveError keeps an empty error paragraph rendered — wired
	// into the control's aria-describedby and found by the id that
	// rides it — for a script that fills it without
	// re-rendering (see headless.FieldProps.ReserveError). The caller
	// that fills it must also set aria-invalid on the control; the
	// server-rendered path should pass Error instead.
	ReserveError bool
	Class        string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers) to the field row's root <div>. Keys the
	// component owns are dropped: class and id (use Class) and
	// data-fui-*.
	ExtraAttrs html.Attrs
}

// FormField renders a labelled form field with optional help and error
// text. Wire the control through Input's builder; the label's For and
// the control's id cannot disagree, because the control is built from
// the field's own wiring.
//
// Help and Error are BOTH rendered when both are set, the error
// first: the hint is the rule the value must obey and the error is
// the violation, so dropping the rule exactly when it was broken is
// dropping it when it is needed most.
func FormField(cfg FormFieldConfig) render.HTML {
	if cfg.Label == "" {
		panic("ui: FormField requires Label")
	}
	if cfg.For == "" {
		panic("ui: FormField requires For (the control element's ID)")
	}
	if cfg.Input == nil {
		panic("ui: FormField requires Input — a builder that receives the field's wiring (headless.FieldControl); build the control with ui.Control, a typed field, or headless directly")
	}
	return formFieldStyle.WrapHTML(headless.Field(headless.FieldProps{
		Label: cfg.Label, For: cfg.For,
		Hint: cfg.Help, Error: cfg.Error, Required: cfg.Required,
		ReserveError: cfg.ReserveError,
		Parts:        rootClassParts(cfg.Class),
		ExtraAttrs:   html.SafeExtraAttrs(cfg.ExtraAttrs),
	}, fieldClasses, cfg.Input))
}

// ─── FormSection ────────────────────────────────────────────────────

// FormSectionConfig groups related fields under a heading + description.
type FormSectionConfig struct {
	Heading     string // optional
	Description string // optional
	Class       string
	// ID names the group and roots the description's id (<ID>-desc).
	// Without one the id is derived from Heading, so two sections
	// with one heading and a description on a page would share it:
	// give the second an ID.
	ID string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers) to the group's root element, whichever shape
	// it takes (<div> without a Heading, <fieldset> with one). Keys
	// the component owns are dropped: class and id (use Class) and
	// data-fui-*.
	ExtraAttrs html.Attrs
}

// formSectionClasses dresses headless.Fieldset's parts. The legend
// maps to fui-form-section__heading: the name the sheet styles and
// the old markup emitted, so the legend's typography survives the
// move to the primitive.
var formSectionClasses = headless.Classes{
	headless.PartRoot:       "fui-form-section",
	headless.PartLegend:     "fui-form-section__heading",
	headless.PartGroupDesc:  "fui-form-section__description",
	headless.PartFields:     "fui-form-section__fields",
	headless.PartGroupError: "fui-form-section__error",
}

// FormSection wraps a group of FormFields with a shared heading, on
// headless.Fieldset: a heading renders the native fieldset + legend
// pair, and no heading renders the plain div — an unlabelled fieldset
// is a landmark that names nothing.
func FormSection(cfg FormSectionConfig, fields ...render.HTML) render.HTML {
	return formSectionStyle.WrapHTML(headless.Fieldset(headless.FieldsetProps{
		Legend:      cfg.Heading,
		Description: cfg.Description,
		ID:          cfg.ID,
		ExtraAttrs:  headless.Safe(cfg.ExtraAttrs, "class", "id"),
		Parts:       rootClassParts(cfg.Class),
	}, formSectionClasses, fields...))
}

// ─── Button ─────────────────────────────────────────────────────────

// ButtonVariant is the semantic variant of a Button. String-typed
// for ergonomic Go enums + readable serialization. Apps extend the
// set with RegisterButtonVariant; unregistered values panic at render.
type ButtonVariant string

const (
	ButtonPrimary   ButtonVariant = "primary"
	ButtonSecondary ButtonVariant = "secondary"
	ButtonDanger    ButtonVariant = "danger"
	ButtonGhost     ButtonVariant = "ghost"
)

// ButtonSize is the rendered button size. Default sits on a 44px
// touch-target floor (WCAG 2.5.5). ButtonSizeSmall opts out of the
// floor for row-action contexts where the parent row already provides
// the tap area (table rows, dense toolbars). ButtonSizeLarge bumps
// padding + font-size for hero CTAs.
type ButtonSize string

const (
	ButtonSizeDefault ButtonSize = ""
	ButtonSizeSmall   ButtonSize = "small"
	ButtonSizeLarge   ButtonSize = "large"
)

// ButtonConfig configures a button.
type ButtonConfig struct {
	Label string // required visible text + aria-label
	// AriaLabel overrides the accessible name when it must differ from
	// the visible Label: a row of buttons all reading "Revoke" that
	// each need a distinct accessible name ("Revoke admin from Alice").
	// Empty ⇒ the accessible name is Label. This is the supported way
	// to set it — an aria-label in ExtraAttrs is dropped (owned key).
	AriaLabel string
	// Variant defaults to ButtonPrimary.
	Variant ButtonVariant
	// Size defaults to ButtonSizeDefault.
	Size ButtonSize
	// Type is the button type: "button" (default), "submit", or "reset".
	Type string
	// Disabled renders the disabled state. This is the supported way to
	// set it — a disabled key in ExtraAttrs panics pointing here.
	Disabled bool
	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the rendered <button>.
	// Every data-fui-* key is runtime wiring and goes through the
	// typed Action seam (attach interactive wiring with
	// interactive.Action.Attrs(), interactive.OpenOnClick and friends —
	// headless.ButtonProps.Action admits exactly that vocabulary and
	// panics on any data-fui-* key outside it, where the old carrier
	// contract rendered it as a dead attribute). Keys the component
	// owns are dropped: class and id (use Class / ID), type (use
	// Type), disabled (use Disabled) and aria-label (use AriaLabel).
	ExtraAttrs html.Attrs
	ID         string
	Class      string
}

// Button renders a semantic button with a typed variant, through the
// headless structure dressed with this package's class map: Variant
// maps to .fui-button--<variant> in the registered ui-button CSS.
//
// Authors never reach for raw class strings. Pick a variant.
// Unknown variants panic at render time so typos surface
// immediately rather than silently rendering an unstyled button.
// Custom brand variants/sizes join the set via RegisterButtonVariant /
// RegisterButtonSize (shared with LinkButton).
func Button(cfg ButtonConfig) render.HTML {
	if cfg.Label == "" {
		panic("ui: Button requires Label")
	}
	v := cfg.Variant
	if v == "" {
		v = ButtonPrimary
	}
	checkButtonVariant("Button", v)
	checkButtonSize("Button", cfg.Size)
	action, extra := splitButtonAttrs(cfg.ExtraAttrs)
	// All variants share the single canonical ui-button marker; the
	// .fui-button--<variant> class on the same element drives the
	// visual delta via buttonCSS's variant rules. No legacy per-
	// variant marker / sheet.
	return buttonStyle.WrapHTML(headless.Button(headless.ButtonProps{
		Label:      cfg.Label,
		AriaLabel:  cfg.AriaLabel,
		Variant:    string(v),
		Size:       string(cfg.Size),
		Type:       cfg.Type,
		Disabled:   cfg.Disabled,
		ID:         cfg.ID,
		Action:     action,
		ExtraAttrs: extra,
		Parts:      rootClassParts(cfg.Class),
	}, buttonClasses))
}

// ─── LinkButton ─────────────────────────────────────────────────────

// LinkButtonConfig configures a button-styled <a> link. Use this when
// the affordance navigates (changes URL): CTAs like "Get started",
// "Read the docs". For in-page actions that don't change URL, use
// Button instead.
type LinkButtonConfig struct {
	Label   string        // required visible text
	Href    string        // required navigation target
	Variant ButtonVariant // defaults to ButtonPrimary
	Size    ButtonSize    // defaults to ButtonSizeDefault
	// External, when true, opens the link in a new tab with
	// rel="noopener noreferrer". Use for off-site links (docs to
	// GitHub, pkg.go.dev, etc.). The runtime's SPA-nav interceptor
	// naturally skips http(s):// hrefs (they're not "internal"), so
	// External does not also need to "suppress SPA nav": the
	// underlying SPA router already does the right thing.
	External bool
	// Icon, when set, renders the named registered icon (see
	// RegisterIcon / Icon) before the label. The button's inline-flex
	// gap handles spacing. Unknown names render the label alone.
	Icon  string
	ID    string
	Class string
	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the rendered <a>. The
	// four data-fui-* keys that make sense on a link (push-state,
	// prefetch, open, deeplink) go through the typed Action seam;
	// every other data-fui-* key is refused, as it always was — a
	// link navigates, a button acts. Keys the component owns are
	// dropped: class and id (use Class / ID) and href (use Href).
	// With External, target and rel are owned too; without it a caller
	// may still set them.
	ExtraAttrs html.Attrs
}

// LinkButton renders a button-styled anchor, through the same headless
// structure and class map as Button. The visual styling is shared via
// the registered ui-button CSS. The difference is semantic:
// <a> for navigation, <button> for actions. Screen readers, "open in
// new tab", and SPA push-state nav all rely on the right tag choice.
func LinkButton(cfg LinkButtonConfig) render.HTML {
	if cfg.Label == "" {
		panic("ui: LinkButton requires Label")
	}
	if cfg.Href == "" {
		panic("ui: LinkButton requires Href. Use Button for non-navigating actions")
	}
	// Refuse dangerous schemes at render time. The runtime's SPA
	// navigator screens these too, but a direct anchor click bypasses
	// the SPA interceptor (browser handles it natively), so the
	// rendered href must already be safe. javascript:/vbscript:/non-
	// image data: are the canonical XSS vectors for links. headless
	// degrades a rejected href to a dead link; this package refuses
	// outright, so the mistake is found where it is made. The same
	// posture covers the origin-absolute spellings — "//host/x" and
	// its backslash twin — which are not XSS but are hrefs headless
	// drops to a dead link; generated apps feed spec-authored hrefs
	// here, so an LLM's "//github.com/…" CTA must fail loudly at
	// build time, not ship as a disabled control.
	if isUnsafeScheme(cfg.Href) {
		panic("ui: LinkButton refuses unsafe Href scheme: " + cfg.Href)
	}
	if isCrossOriginAbsolute(cfg.Href) {
		panic("ui: LinkButton refuses a protocol-relative Href — name the scheme (https://…) or keep the path root-relative: " + cfg.Href)
	}
	v := cfg.Variant
	if v == "" {
		v = ButtonPrimary
	}
	checkButtonVariant("LinkButton", v)
	checkButtonSize("LinkButton", cfg.Size)
	action, extra := splitLinkAttrs(cfg.ExtraAttrs)
	var icon render.HTML
	if cfg.Icon != "" && IconRegistered(cfg.Icon) {
		icon = Icon(cfg.Icon, IconConfig{Size: "18"})
	}
	return buttonStyle.WrapHTML(headless.Button(headless.ButtonProps{
		Label:      cfg.Label,
		Href:       cfg.Href,
		External:   cfg.External,
		Variant:    string(v),
		Size:       string(cfg.Size),
		ID:         cfg.ID,
		Icon:       icon,
		Action:     action,
		ExtraAttrs: extra,
		Parts:      rootClassParts(cfg.Class),
	}, buttonClasses))
}

// rootClassParts carries a caller's Class onto the root part, where
// the headless box appends it after the class map's own classes
// instead of replacing them. The shared class map is never mutated.
func rootClassParts(class string) headless.Parts {
	if class == "" {
		return headless.Parts{}
	}
	return headless.Parts{Attrs: headless.PartAttrs{
		headless.PartRoot: {"class": class},
	}}
}

// splitButtonAttrs splits a Button's ExtraAttrs at the seam: every
// data-fui-* key is runtime wiring and travels through the typed
// Action, where headless admits exactly the wiring vocabulary and
// panics on anything else, naming the key; everything else is
// decoration and travels through headless's ExtraAttrs, whose Safe
// drops the keys the component owns (aria-label among them, which the
// AriaLabel field is the supported way to set).
func splitButtonAttrs(extra html.Attrs) (action, plain html.Attrs) {
	action, plain = html.Attrs{}, html.Attrs{}
	for k, v := range extra {
		lk := strings.ToLower(k)
		if _, dup := action[lk]; dup {
			panic("ui: ExtraAttrs carries " + lk + " under two spellings; one attribute, one spelling")
		}
		if _, dup := plain[lk]; dup {
			panic("ui: ExtraAttrs carries " + lk + " under two spellings; one attribute, one spelling")
		}
		switch {
		case lk == "disabled":
			panic("ui: Button ExtraAttrs carries disabled — use ButtonConfig.Disabled, the field owns the state")
		case lk == "aria-label":
			// Owned: use AriaLabel.
		case strings.HasPrefix(lk, "data-fui-"):
			action[lk] = v
		default:
			plain[lk] = v
		}
	}
	return action, plain
}

// splitLinkAttrs is splitButtonAttrs for a link: only the four
// data-fui-* keys that make sense on an anchor travel the Action seam;
// every other data-fui-* key is refused as it always was, because a
// link navigates and a button acts.
func splitLinkAttrs(extra html.Attrs) (action, plain html.Attrs) {
	action, plain = html.Attrs{}, html.Attrs{}
	for k, v := range extra {
		lk := strings.ToLower(k)
		if _, dup := action[lk]; dup {
			panic("ui: ExtraAttrs carries " + lk + " under two spellings; one attribute, one spelling")
		}
		if _, dup := plain[lk]; dup {
			panic("ui: ExtraAttrs carries " + lk + " under two spellings; one attribute, one spelling")
		}
		switch {
		case lk == "data-fui-push-state", lk == "data-fui-prefetch",
			lk == "data-fui-open", lk == "data-fui-deeplink":
			action[lk] = v
		case strings.HasPrefix(lk, "data-fui-"):
			// Refused, as before the seam existed.
		default:
			plain[lk] = v
		}
	}
	return action, plain
}

// stripURLControls removes every ASCII control byte and space from a
// URL-shaped string. Browsers strip this set from a URL — including
// bytes INTERIOR to the scheme token ("java\tscript:" resolves as
// "javascript:") and between the slashes of an origin ("/\t/evil"
// resolves as "//evil") — before scheme and origin resolution, so the
// checks that follow must see the same URL the browser will.
func stripURLControls(href string) string {
	var b strings.Builder
	b.Grow(len(href))
	for i := range len(href) {
		c := href[i]
		if c == ' ' || c <= 0x1f || c == 0x7f {
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// isUnsafeScheme rejects the canonical XSS vectors for `href`/`src`
// attributes: javascript:, vbscript:, and non-image data: URIs. Used
// by LinkButton (render-time guard) and shadowed by the runtime's
// _isUnsafeSignalUrl for programmatic SPA navigation.
func isUnsafeScheme(href string) bool {
	s := stripURLControls(href)
	// Case-insensitive prefix check.
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "javascript:") {
		return true
	}
	if strings.HasPrefix(lower, "vbscript:") {
		return true
	}
	if strings.HasPrefix(lower, "data:") {
		// Every data: URL, images included: the anchor policy headless
		// applies (urlsafe.CleanAnchor) refuses them all, so admitting
		// data:image/ here would render a dead link instead of a panic.
		return true
	}
	return false
}

// isCrossOriginAbsolute reports whether href names a foreign origin
// WITHOUT naming its scheme: the protocol-relative "//host/x" and its
// backslash twin "/\host/x" (a URL parser treats "\" as a path
// separator, so both resolve to an origin-absolute URL). headless's
// anchor policy drops both to a dead link; LinkButton refuses them
// with their own reason instead.
func isCrossOriginAbsolute(href string) bool {
	s := stripURLControls(href)
	return strings.HasPrefix(s, "//") || strings.HasPrefix(s, `/\`)
}

// ─── StatusBadge ────────────────────────────────────────────────────

// StatusVariant is the semantic variant of a StatusBadge. The same
// set drives Callout, Tag, Notification, and FilterChipBar chips;
// apps extend it with RegisterStatusVariant. Unregistered values
// panic at render.
type StatusVariant string

const (
	StatusSuccess StatusVariant = "success"
	StatusWarning StatusVariant = "warning"
	StatusDanger  StatusVariant = "danger"
	StatusInfo    StatusVariant = "info"
	StatusNeutral StatusVariant = "neutral"
)

// StatusBadgeConfig configures a small status pill.
type StatusBadgeConfig struct {
	Label   string        // required visible text
	Variant StatusVariant // defaults to Neutral
	ID      string
	Class   string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the pill's root <span>.
	// Keys the component owns are dropped: class and id (use Class /
	// ID) and data-fui-*.
	ExtraAttrs html.Attrs
}

// StatusBadge renders a small inline pill conveying state, on
// headless.Badge: the label is the whole of what a screen reader
// hears, so the tone a variant paints is decoration for the word.
func StatusBadge(cfg StatusBadgeConfig) render.HTML {
	if cfg.Label == "" {
		panic("ui: StatusBadge requires Label")
	}
	v := cfg.Variant
	if v == "" {
		v = StatusNeutral
	}
	checkStatusVariant("StatusBadge", v)
	cls := joinNonEmpty("fui-badge--"+string(v), cfg.Class)
	return statusBadgeStyle.WrapHTML(headless.Badge(headless.BadgeProps{
		Label:      cfg.Label,
		ID:         cfg.ID,
		ExtraAttrs: headless.Safe(cfg.ExtraAttrs, "class", "id"),
		Parts:      rootClassParts(cls),
	}, badgeClasses))
}

// badgeClasses dresses headless.Badge.
var badgeClasses = headless.Classes{
	headless.PartRoot: "fui-badge",
}

// ─── EmptyState ─────────────────────────────────────────────────────

// EmptyStateConfig configures an empty-state surface.
type EmptyStateConfig struct {
	Title       string      // required
	Description string      // optional supporting text
	Action      render.HTML // optional CTA (e.g. a button or link)
	ID          string
	Class       string

	// HeadingLevel overrides the title's heading level (1–6). Zero defaults
	// to 3 (h3), preserving the gallery/demo behaviour where the empty state
	// nests inside a section. A real page that mounts the empty state as the
	// only content under the page <h1> (e.g. an admin list with zero rows)
	// passes 2 so the outline doesn't skip h1 → h3.
	HeadingLevel int

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the empty state's root.
	// Keys the component owns are dropped: class and id (use
	// Class / ID), style, data-fui-*, role, aria-label and
	// aria-labelledby.
	ExtraAttrs html.Attrs
}

// emptyStateClasses dresses headless.EmptyState's parts.
var emptyStateClasses = headless.Classes{
	headless.PartRoot:       "fui-empty-state",
	headless.PartEmptyTitle: "fui-empty-state__title",
	headless.PartEmptyDesc:  "fui-empty-state__description",
	headless.PartEmptyAct:   "fui-empty-state__action",
}

// EmptyState renders the nothing-here on headless.EmptyState: a
// region named by its own heading, so "no results" is a findable
// place with a way out.
func EmptyState(cfg EmptyStateConfig) render.HTML {
	return emptyStateStyle.WrapHTML(headless.EmptyState(headless.EmptyStateProps{
		Title:       cfg.Title,
		Level:       cfg.HeadingLevel,
		Description: cfg.Description,
		Action:      cfg.Action,
		ID:          cfg.ID,
		ExtraAttrs:  headless.Safe(cfg.ExtraAttrs, "class", "id", "role", "aria-label", "aria-labelledby"),
	}, emptyStateClasses))
}

// ─── Callout ────────────────────────────────────────────────────────

// CalloutConfig configures a persistent informational block.
type CalloutConfig struct {
	Title   string
	Variant StatusVariant // info | success | warning | danger | neutral
	ID      string
	Class   string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers) to the callout's root element. Keys the
	// component owns are dropped: class and id (use Class / ID),
	// data-fui-* and role.
	ExtraAttrs html.Attrs
}

// calloutClasses dresses headless.Alert's parts. The tone modifier
// class is appended by the adapter — the registered custom variants
// arrive as names, not class-map entries.
var calloutClasses = headless.Classes{
	headless.PartRoot:   "fui-callout",
	headless.PartHeader: "fui-callout__head",
	headless.PartTitle:  "fui-callout__title",
	headless.PartDesc:   "fui-callout__desc",
	headless.PartBody:   "fui-callout__body",
	headless.PartFooter: "fui-callout__footer",
}

// Callout renders a persistent info/warning/error block on
// headless.Alert. Distinct from Toast / Notification (ephemeral);
// callouts live inline with content.
//
// Danger and warning callouts interrupt (role=alert); the rest are
// standing messages the page rendered, which do not. The old
// complementary-<aside> shape is gone: a tip is emphasis, not a
// tangential region, and a nested complementary landmark is the axe
// finding the Landmark field existed to dodge.
func Callout(cfg CalloutConfig, body ...render.HTML) render.HTML {
	v := cfg.Variant
	if v == "" {
		v = StatusInfo
	}
	checkStatusVariant("Callout", v)
	live := headless.LiveOff
	if v == StatusDanger || v == StatusWarning {
		live = headless.LiveAssertive
	}
	var bodyHTML render.HTML
	if len(body) > 0 {
		bodyHTML = render.Join(body...)
	}
	cls := joinNonEmpty("fui-callout--"+string(v), cfg.Class)
	return calloutStyle.WrapHTML(headless.Alert(headless.AlertProps{
		Title:      cfg.Title,
		Tone:       string(v),
		Body:       bodyHTML,
		Live:       live,
		ID:         cfg.ID,
		ExtraAttrs: headless.Safe(cfg.ExtraAttrs, "class", "id", "role"),
		Parts:      rootClassParts(cls),
	}, calloutClasses))
}

// ─── StatCard ───────────────────────────────────────────────────────

// TrendDirection indicates the direction of a stat trend.
type TrendDirection string

const (
	TrendUp   TrendDirection = "up"
	TrendDown TrendDirection = "down"
	TrendFlat TrendDirection = "flat"
)

// StatCardConfig configures a metric card.
type StatCardConfig struct {
	Label string // required (e.g. "Active users")
	Value string // required (e.g. "12,483" or "98.4%")
	Trend string // optional trend label (e.g. "+12% vs. last week")

	// Direction colors the trend pill. Defaults to flat.
	Direction TrendDirection

	ID    string
	Class string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the stat card's root <div>.
	// Keys the component owns are dropped: class and id (use Class /
	// ID) and data-fui-*.
	ExtraAttrs html.Attrs
}

// statCardClasses dresses headless.StatCard's parts.
var statCardClasses = headless.Classes{
	headless.PartRoot:      "fui-stat-card",
	headless.PartLabel:     "fui-stat-card__label",
	headless.PartStatValue: "fui-stat-card__value",
	headless.PartStatTrend: "fui-stat-card__trend",

	headless.Part("stat-trend--up"):   "fui-stat-card__trend--up",
	headless.Part("stat-trend--down"): "fui-stat-card__trend--down",
	headless.Part("stat-trend--flat"): "fui-stat-card__trend--flat",
}

// StatCard renders a metric card on headless.StatCard: label, value,
// optional trend — the label first, because the name before the
// number is what makes the number a fact.
func StatCard(cfg StatCardConfig) render.HTML {
	if cfg.Label == "" {
		panic("ui: StatCard requires Label")
	}
	if cfg.Value == "" {
		panic("ui: StatCard requires Value")
	}
	dir := cfg.Direction
	if dir == "" {
		dir = TrendFlat
	}
	return statCardStyle.WrapHTML(headless.StatCard(headless.StatCardProps{
		Label:      cfg.Label,
		Value:      cfg.Value,
		Trend:      cfg.Trend,
		Direction:  string(dir),
		ID:         cfg.ID,
		ExtraAttrs: headless.Safe(cfg.ExtraAttrs, "class", "id"),
		Parts:      rootClassParts(cfg.Class),
	}, statCardClasses))
}

// ─── Avatar ─────────────────────────────────────────────────────────

// AvatarSize is one of a small set of pre-defined avatar sizes.
// Sizes are CSS classes, not inline styles, so a strict CSP that
// blocks `style="…"` attributes still works.
type AvatarSize string

const (
	AvatarSm AvatarSize = "sm" // ~1.5rem
	AvatarMd AvatarSize = ""   // default ~2.5rem
	AvatarLg AvatarSize = "lg" // ~3rem
	AvatarXl AvatarSize = "xl" // ~4rem
)

// AvatarStatus is a presence indicator drawn as a small dot in the
// avatar's lower corner. Empty renders no dot. Colors come from the
// status tokens so a themed app recolors them for free. This is the
// visual half of presence; the framework does not track who is online:
// an app feeds the status from its own source (see the presence
// note in framework/docs/content/interactive-patterns.md).
type AvatarStatus string

const (
	AvatarStatusNone AvatarStatus = ""        // no dot (default)
	AvatarOnline     AvatarStatus = "online"  // success token
	AvatarAway       AvatarStatus = "away"    // warning token
	AvatarBusy       AvatarStatus = "busy"    // danger token
	AvatarOffline    AvatarStatus = "offline" // muted token
)

// AvatarConfig configures an avatar.
type AvatarConfig struct {
	// Name is required; used for alt text and to derive initials when
	// no image source is set.
	Name string
	Src  string     // optional image URL; falls back to initials when empty
	Size AvatarSize // sm | "" (default md) | lg | xl

	// Status draws a presence dot in the lower corner (online / away /
	// busy / offline). Empty renders no dot.
	Status AvatarStatus
	// StatusLabel overrides the dot's accessible name. Defaults to the
	// status value (e.g. "online"). Ignored when Status is empty.
	StatusLabel string

	ID    string
	Class string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the avatar's root <span>.
	// Keys the component owns are dropped: class and id (use Class /
	// ID) and data-fui-*.
	ExtraAttrs html.Attrs
}

// Avatar renders a circular avatar with an image fallback to text
// initials when no image source is provided.
func Avatar(cfg AvatarConfig) render.HTML {
	if cfg.Name == "" {
		panic("ui: Avatar requires Name")
	}
	cls := "ui-avatar"
	if cfg.Size != AvatarMd {
		cls += " ui-avatar--" + string(cfg.Size)
	}
	if cfg.Status != AvatarStatusNone {
		cls += " ui-avatar--has-status"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	spanCfg := html.TextConfig{Class: cls, ID: cfg.ID,
		ExtraAttrs: html.SafeExtraAttrs(cfg.ExtraAttrs)}

	var inner []render.HTML
	if cfg.Src != "" {
		inner = append(inner, html.Image(html.ImageConfig{
			Src: cfg.Src, Alt: cfg.Name, Class: "ui-avatar__img",
		}))
	} else {
		inner = append(inner,
			html.Span(html.TextConfig{
				Class:      "ui-avatar__initials",
				ExtraAttrs: html.Attrs{"aria-hidden": "true"},
			}, render.Text(initials(cfg.Name))),
			html.Span(html.TextConfig{Class: "ui-visually-hidden"},
				render.Text(cfg.Name)),
		)
	}
	if cfg.Status != AvatarStatusNone {
		inner = append(inner, avatarStatusDot(cfg.Status, cfg.StatusLabel))
	}
	return avatarStyle.WrapHTML(html.Span(spanCfg, inner...))
}

// avatarStatusDot renders the presence dot. It carries role="img" +
// aria-label so the status is announced (a bare aria-label on a span is
// rejected by axe: the implicit role doesn't accept a name), matching
// the AvatarGroup overflow-chip pattern.
func avatarStatusDot(status AvatarStatus, label string) render.HTML {
	if label == "" {
		label = string(status)
	}
	return html.Span(html.TextConfig{
		Class: "ui-avatar__status ui-avatar__status--" + string(status),
		ExtraAttrs: html.Attrs{
			"role":       "img",
			"aria-label": label,
		},
	})
}

func initials(name string) string {
	parts := strings.Fields(name)
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		s := []rune(parts[0])
		if len(s) == 0 {
			return ""
		}
		return strings.ToUpper(string(s[0]))
	}
	first := []rune(parts[0])
	last := []rune(parts[len(parts)-1])
	out := []rune{}
	if len(first) > 0 {
		out = append(out, first[0])
	}
	if len(last) > 0 {
		out = append(out, last[0])
	}
	return strings.ToUpper(string(out))
}

// ─── CodeBlock ──────────────────────────────────────────────────────

// CodeBlockConfig configures a styled code-sample block.
type CodeBlockConfig struct {
	Code     string // raw source to render; escaped. Ignored when Lines is set.
	Language string // optional, used for aria-label only
	// Lines carries pre-rendered (e.g. syntax-highlighted) logical source
	// lines. When non-empty it takes precedence over Code; each entry is
	// wrapped as one line so LineNumbers can number it. Callers own the
	// per-token markup: pass already-escaped, trusted HTML.
	Lines []render.HTML
	// Filename, when set, renders a chrome header (status dot + filename)
	// above the body and switches the wrapper to a framed container.
	Filename string
	// ShowCopy adds a copy-to-clipboard button (the framework CopyButton)
	// in the header, targeting this block's own body. Forces a header even
	// when Filename is empty.
	ShowCopy bool
	// LineNumbers renders a left gutter numbering each line.
	LineNumbers bool
	// Scroll caps the body height (var(--ui-code-block-scroll-max,
	// 26rem)) and makes it scroll vertically, for showing a long file in
	// full without letting it dominate the page. Implies the framed
	// container.
	Scroll bool
	// HighlightLines marks the given 1-based source lines with
	// ui-code-block__line--highlight, a background band that reaches the
	// block's edge. Ranges past the last line match nothing.
	HighlightLines []LineRange
	// Diff classifies lines by their first character: '+' (including the
	// '+++' file-header form) gets ui-code-block__line--added, '-' (and
	// '---') gets --removed. The marker stays in the text: a diff's
	// content IS the diff. On the Lines path a leading token span is
	// skipped, so the marker behind it still classifies.
	Diff bool
	// HighlightWords wraps literal (not regex) matches inside a line in
	// <mark class="ui-code-block__mark">. Matching runs on the source
	// text of each line's text nodes: a word never matches across a tag
	// boundary, and marked text is escaped like the rest of the line.
	HighlightWords []string
	// Wrap soft-wraps long lines (white-space: pre-wrap) instead of the
	// default horizontal scroll. The zero value keeps today's behaviour:
	// code blocks scroll, they do not wrap.
	Wrap  bool
	ID    string
	Class string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers) to the block's root element, whichever shape
	// it takes (a bare <pre> or a framed <div>). Keys the component
	// owns are dropped: class and id (use Class / ID), data-fui-*, and
	// the scroll contract (tabindex, aria-label) that lives on the
	// <pre> body.
	ExtraAttrs html.Attrs
}

// LineRange is a 1-based, inclusive range of source lines. To == 0 means
// a single line (From only).
type LineRange struct {
	From int
	To   int
}

// ParseLineRanges parses a comma-separated list of 1-based line numbers
// and ascending inclusive ranges ("2", "1,3-5"), the form the highlight
// fence option accepts. An empty spec parses to no ranges. Anything that
// is not a positive line number or an ascending range is an error;
// callers that degrade instead of failing (ui.Markdown) drop the option.
func ParseLineRanges(spec string) ([]LineRange, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, nil
	}
	parts := strings.Split(spec, ",")
	ranges := make([]LineRange, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		lo, hi, isRange := strings.Cut(p, "-")
		from, err := parseLineNumber(lo)
		if err != nil {
			return nil, err
		}
		r := LineRange{From: from}
		if isRange {
			r.To, err = parseLineNumber(hi)
			if err != nil {
				return nil, err
			}
			if r.To < r.From {
				return nil, fmt.Errorf("ui: line range %q ends before it starts", p)
			}
		}
		ranges = append(ranges, r)
	}
	return ranges, nil
}

func parseLineNumber(s string) (int, error) {
	s = strings.TrimSpace(s)
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("ui: %q is not a positive line number", s)
	}
	return n, nil
}

// codeBlockSeq mints a process-unique id for a framed block's body so the
// copy button can target its own <pre> via #id.
var codeBlockSeq atomic.Uint64

// CodeBlock renders a styled code sample. In its simplest form (Code only) it
// is a bare, horizontally-scrollable <pre>. Set Filename / ShowCopy /
// LineNumbers (or pass Lines) to get the framed variant: a chrome header with
// the filename, an optional copy button, and an optional line-number gutter.
//
// The wrapper element carries data-fui-comp="ui-code-block" so the runtime
// auto-loads the scoped stylesheet on first appearance.
func CodeBlock(cfg CodeBlockConfig) render.HTML {
	framed := cfg.Filename != "" || cfg.ShowCopy || cfg.LineNumbers || cfg.Scroll || len(cfg.Lines) > 0
	label := "source code"
	if cfg.Language != "" {
		label = cfg.Language + " source"
	}
	extra := html.SafeExtraAttrs(cfg.ExtraAttrs, "tabindex", "aria-label")

	// Body <pre>. WCAG 2.1.1: tabindex=0 so keyboard users can pan the
	// horizontal scroll. (role=region is intentionally avoided. It would
	// make every block a landmark and fail landmark-unique.)
	bodyID := cfg.ID
	if framed && cfg.ShowCopy && bodyID == "" {
		bodyID = "ui-code-block-" + strconv.FormatUint(codeBlockSeq.Add(1), 10)
	}
	// Any per-line feature (highlight, diff, word marks) forces the
	// per-line wrapper on the Code path so the classes attach to the same
	// ui-code-block__line element the Lines path uses; a config with none
	// keeps today's <code> body.
	perLine := len(cfg.HighlightLines) > 0 || cfg.Diff || len(cfg.HighlightWords) > 0
	var body render.HTML
	switch {
	case len(cfg.Lines) > 0:
		wrapped := make([]render.HTML, len(cfg.Lines))
		for i, ln := range cfg.Lines {
			content := string(ln)
			if len(cfg.HighlightWords) > 0 {
				content = markWords(content, cfg.HighlightWords)
			}
			wrapped[i] = html.Span(html.TextConfig{Class: codeBlockLineClass(cfg, i+1, content)}, render.HTML(content))
		}
		body = render.Join(wrapped...)
	case perLine:
		src := strings.Split(cfg.Code, "\n")
		// Mirror HighlightLines: a trailing newline must not render a
		// blank last row, or line numbers drift from the Lines path.
		if n := len(src); n > 1 && src[n-1] == "" {
			src = src[:n-1]
		}
		wrapped := make([]render.HTML, len(src))
		for i, raw := range src {
			content := render.Escape(raw)
			if len(cfg.HighlightWords) > 0 {
				content = markWords(content, cfg.HighlightWords)
			}
			wrapped[i] = html.Span(html.TextConfig{Class: codeBlockLineClass(cfg, i+1, content)}, render.HTML(content))
		}
		body = render.Join(wrapped...)
	default:
		body = render.Tag("code", nil, render.Text(cfg.Code))
	}

	if !framed {
		cls := "ui-code-block"
		if cfg.Wrap {
			cls += " ui-code-block--wrap"
		}
		if cfg.Class != "" {
			cls += " " + cfg.Class
		}
		preAttrs := extra
		if preAttrs == nil {
			preAttrs = html.Attrs{}
		}
		preAttrs["class"] = cls
		preAttrs["tabindex"] = "0"
		preAttrs["aria-label"] = label
		if bodyID != "" {
			preAttrs["id"] = bodyID
		}
		return codeBlockStyle.WrapHTML(render.Tag("pre", preAttrs, body))
	}

	cls := "ui-code-block ui-code-block--framed"
	if cfg.LineNumbers {
		cls += " ui-code-block--numbered"
	}
	if cfg.Scroll {
		cls += " ui-code-block--scroll"
	}
	if cfg.Wrap {
		cls += " ui-code-block--wrap"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	headChildren := []render.HTML{}
	if cfg.Filename != "" {
		headChildren = append(headChildren,
			html.Span(html.TextConfig{
				Class:      "ui-code-block__status",
				ExtraAttrs: html.Attrs{"aria-hidden": "true"},
			}),
			html.Span(html.TextConfig{Class: "ui-code-block__file"}, render.Text(cfg.Filename)),
		)
	}
	metaChildren := []render.HTML{}
	if len(cfg.Lines) > 0 {
		metaChildren = append(metaChildren,
			html.Span(html.TextConfig{}, render.Text(strconv.Itoa(len(cfg.Lines))+" lines")))
	}
	if cfg.ShowCopy {
		metaChildren = append(metaChildren, CopyButton(CopyButtonConfig{
			Target:       "#" + bodyID,
			Label:        "copy",
			CopiedLabel:  "copied",
			AnnounceText: "Copied",
			Class:        "ui-code-block__copy",
		}))
	}
	if len(metaChildren) > 0 {
		headChildren = append(headChildren,
			html.Div(html.DivConfig{Class: "ui-code-block__meta"}, metaChildren...))
	}
	head := html.Div(html.DivConfig{Class: "ui-code-block__head"}, headChildren...)

	preAttrs := map[string]string{"class": "ui-code-block__body", "tabindex": "0", "aria-label": label}
	if bodyID != "" {
		preAttrs["id"] = bodyID
	}
	pre := render.Tag("pre", preAttrs, body)
	return codeBlockStyle.WrapHTML(
		html.Div(html.DivConfig{Class: cls, ID: cfg.ID, ExtraAttrs: extra}, head, pre))
}

// lineHighlighted reports whether the 1-based line n falls inside one of
// the ranges. To == 0 means "From only".
func lineHighlighted(ranges []LineRange, n int) bool {
	for _, r := range ranges {
		if n < r.From {
			continue
		}
		if r.To == 0 {
			if n == r.From {
				return true
			}
			continue
		}
		if n <= r.To {
			return true
		}
	}
	return false
}

// firstTextByte returns the line's first visible character, skipping
// tags so a diff marker behind a token span
// ('<span class="tk-pn">-</span>x') still classifies. 0 when the line
// starts with no text: an entity decodes to one of &<>"', never a
// marker, and an empty line classifies as neither.
func firstTextByte(s string) byte {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '<':
			j := strings.IndexByte(s[i:], '>')
			if j < 0 {
				return 0
			}
			i += j
		case '&':
			return 0
		default:
			return s[i]
		}
	}
	return 0
}

// codeBlockLineClass builds the per-line class: the base wrapper plus
// the highlight band and/or the diff marker class.
func codeBlockLineClass(cfg CodeBlockConfig, n int, content string) string {
	cls := "ui-code-block__line"
	if lineHighlighted(cfg.HighlightLines, n) {
		cls += " ui-code-block__line--highlight"
	}
	if cfg.Diff {
		switch firstTextByte(content) {
		case '+':
			cls += " ui-code-block__line--added"
		case '-':
			cls += " ui-code-block__line--removed"
		}
	}
	return cls
}

// markWords wraps literal matches of any word in
// <mark class="ui-code-block__mark">, touching text nodes only: tags
// pass through untouched and a word never matches across a tag boundary,
// because each text node is matched on its own. Text nodes are decoded
// to source text for matching and re-escaped on output, so a word like
// "<b>" matches the source characters, never emitted markup. A text
// node with no match is copied verbatim (no decode/re-encode roundtrip).
func markWords(fragment string, words []string) string {
	if len(words) == 0 {
		return fragment
	}
	var b strings.Builder
	i := 0
	for i < len(fragment) {
		if fragment[i] == '<' {
			j := strings.IndexByte(fragment[i:], '>')
			if j < 0 {
				break // malformed trailing '<': copy verbatim below
			}
			end := i + j + 1
			b.WriteString(fragment[i:end])
			i = end
			continue
		}
		end := len(fragment)
		if j := strings.IndexByte(fragment[i:], '<'); j >= 0 {
			end = i + j
		}
		node := fragment[i:end]
		if text := stdhtml.UnescapeString(node); containsAnyWord(text, words) {
			b.WriteString(markText(text, words))
		} else {
			b.WriteString(node)
		}
		i = end
	}
	b.WriteString(fragment[i:])
	return b.String()
}

func containsAnyWord(text string, words []string) bool {
	for _, w := range words {
		if strings.Contains(text, w) {
			return true
		}
	}
	return false
}

// markText marks every literal word occurrence in decoded text and
// returns escaped HTML. The longest word wins at a given position, so
// overlapping words ("err", "error") mark the full token once.
func markText(text string, words []string) string {
	var b strings.Builder
	start := 0
	for i := 0; i < len(text); {
		if w := matchWordAt(text, i, words); w != "" {
			b.WriteString(render.Escape(text[start:i]))
			b.WriteString(`<mark class="ui-code-block__mark">`)
			b.WriteString(render.Escape(w))
			b.WriteString(`</mark>`)
			i += len(w)
			start = i
			continue
		}
		_, size := utf8.DecodeRuneInString(text[i:])
		if size == 0 {
			size = 1
		}
		i += size
	}
	b.WriteString(render.Escape(text[start:]))
	return b.String()
}

// matchWordAt returns the longest word that matches text at byte offset
// at, or "" when none does. Empty words never match (a zero-length match
// would not advance and must not mark between characters).
func matchWordAt(text string, at int, words []string) string {
	best := ""
	for _, w := range words {
		if len(w) > len(best) && at+len(w) <= len(text) && text[at:at+len(w)] == w {
			best = w
		}
	}
	return best
}

// ─── SkipLink ──────────────────────────────────────────────────────

// SkipLinkConfig configures a skip-navigation link.
//
// Renders a visually-hidden anchor that becomes visible on keyboard
// focus, letting users jump past repetitive navigation to the main
// content area. Required for WCAG 2.1 Level A (criterion 2.4.1
// "Bypass Blocks").
//
// Place SkipLink as the first element inside <body>.
//
// Usage:
//
//	ui.SkipLink(ui.SkipLinkConfig{Target: "main-content"})
//	// … then on the main element:
//	// <main id="main-content"> ...
//	// Or with no Target: defaults to "main-content".
//	ui.SkipLink(ui.SkipLinkConfig{})
type SkipLinkConfig struct {
	// Target is the id of the element to jump to.
	// Defaults to "main-content" when empty.
	Target string
	// Text is the visible label shown on focus.
	// Defaults to "Skip to main content" when empty.
	Text  string
	Class string
	ID    string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers) to the link's root <a>. Keys the component
	// owns are dropped: class and id (use Class / ID), data-fui-*, and
	// href (use Target).
	ExtraAttrs html.Attrs
}

// SkipLink renders a WCAG 2.4.1 skip-navigation link.
func SkipLink(cfg SkipLinkConfig) render.HTML {
	target := cfg.Target
	if target == "" {
		target = "main-content"
	}
	text := cfg.Text
	if text == "" {
		text = "Skip to main content"
	}
	cls := "fui-skip-link"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	return skipLinkStyle.WrapHTML(
		html.Link(html.LinkConfig{
			Href:       "#" + target,
			Text:       text,
			Class:      cls,
			ID:         cfg.ID,
			ExtraAttrs: html.SafeExtraAttrs(cfg.ExtraAttrs, "href"),
		}),
	)
}
