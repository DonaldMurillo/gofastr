package ui

import (
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── PageHeader ─────────────────────────────────────────────────────

// PageHeaderConfig configures a page-top header.
type PageHeaderConfig struct {
	Title    string      // required
	Subtitle string      // optional supporting text below the title
	Eyebrow  string      // optional small label above the title (e.g. "Customers")
	Actions  render.HTML // optional trailing action slot (button row, link)
	Class    string
	ID       string
}

// PageHeader renders a top-of-page header with title, optional subtitle
// and eyebrow label, and an action slot.
//
// Composition: html.Header (semantic <header role="banner">) +
// html.Heading (h1) + html.Paragraph for eyebrow/subtitle.
func PageHeader(cfg PageHeaderConfig) render.HTML {
	if cfg.Title == "" {
		panic("ui: PageHeader requires Title")
	}
	cls := "ui-page-header"
	if cfg.Class != "" {
		cls = cls + " " + cfg.Class
	}
	textChildren := []render.HTML{}
	if cfg.Eyebrow != "" {
		textChildren = append(textChildren, html.Paragraph(
			html.TextConfig{Class: "ui-page-header__eyebrow"},
			render.Text(cfg.Eyebrow)))
	}
	textChildren = append(textChildren,
		html.Heading(html.HeadingConfig{Level: 1,
			Class: "ui-page-header__title"}, render.Text(cfg.Title)))
	if cfg.Subtitle != "" {
		textChildren = append(textChildren, html.Paragraph(
			html.TextConfig{Class: "ui-page-header__subtitle"},
			render.Text(cfg.Subtitle)))
	}
	textBlock := html.Div(html.DivConfig{Class: "ui-page-header__text"}, textChildren...)
	body := []render.HTML{textBlock}
	if cfg.Actions != "" {
		body = append(body, html.Div(
			html.DivConfig{Class: "ui-page-header__actions"}, cfg.Actions))
	}
	return pageHeaderStyle.WrapHTML(
		html.Header(html.HeaderConfig{Class: cls, ID: cfg.ID}, body...),
	)
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
// ID behavior:
//   - If ID is set, it's used verbatim — caller controls the anchor.
//   - If ID is empty and Heading is set, the section auto-slugs the
//     heading as its id ("Forms" → id="forms"). This is the typical
//     case for in-page navs / scrollspy rails where the rail's
//     anchor href should match the section just by typing the heading
//     text twice.
//   - If both ID and Heading are empty, the section gets no id and
//     a generic aria-label.
type SectionConfig struct {
	// Eyebrow is an optional short decorative kicker rendered above/around
	// the heading — e.g. a section number ("01 / what it generates"). It is
	// marked aria-hidden because it duplicates the heading for SR users.
	Eyebrow     string
	Heading     string // optional <h2> heading
	Description string // optional supporting text under the heading
	// DescriptionHTML lets the supporting text carry inline markup (code,
	// links). When non-empty it takes precedence over Description.
	DescriptionHTML render.HTML
	// Label sets the section's accessible name when there is no Heading.
	// Without a Heading or Label the section falls back to a generic
	// "Section" aria-label.
	Label string
	Class string
	ID    string
}

// Section renders a content section with consistent spacing and an
// optional heading + description.
//
// Composition: a labelled <section> via html.Section. When a
// Heading is provided, an h2 + aria-labelledby wires up the
// accessibility name; otherwise a generic aria-label is required.
// Without a heading or label this would silently produce an
// inaccessible region — Section panics in that case to push callers
// toward the right shape.
func Section(cfg SectionConfig, body ...render.HTML) render.HTML {
	cls := "ui-section"
	if cfg.Class != "" {
		cls = cls + " " + cfg.Class
	}

	out := []render.HTML{}
	if cfg.Eyebrow != "" {
		// Decorative kicker (section number/label). aria-hidden because it
		// duplicates the heading; visual-only.
		out = append(out, html.Span(html.TextConfig{
			Class:      "ui-section__eyebrow",
			ExtraAttrs: html.Attrs{"aria-hidden": "true"},
		}, render.Text(cfg.Eyebrow)))
	}
	headingID := ""
	if cfg.Heading != "" {
		headingID = "ui-section-" + slug(cfg.Heading)
		out = append(out, html.Heading(html.HeadingConfig{
			Level: 2, ID: headingID, Class: "ui-section__heading",
		}, render.Text(cfg.Heading)))
	}
	if cfg.DescriptionHTML != "" {
		out = append(out, html.Paragraph(
			html.TextConfig{Class: "ui-section__description"},
			cfg.DescriptionHTML))
	} else if cfg.Description != "" {
		out = append(out, html.Paragraph(
			html.TextConfig{Class: "ui-section__description"},
			render.Text(cfg.Description)))
	}
	if len(body) > 0 {
		out = append(out, html.Div(
			html.DivConfig{Class: "ui-section__body"}, body...))
	}

	sectionID := cfg.ID
	if sectionID == "" && cfg.Heading != "" {
		// Auto-anchor — typical use is in-page rails / scrollspy where the
		// rail's href="#<slug>" should land on this section without the
		// caller having to repeat the slug.
		sectionID = slug(cfg.Heading)
	}
	secCfg := html.SectionConfig{Class: cls, ID: sectionID}
	if headingID != "" {
		secCfg.LabelledBy = headingID
	} else if cfg.Label != "" {
		// No heading → use the caller-supplied accessible name.
		secCfg.Label = cfg.Label
	} else {
		// No heading and no label → default to a generic aria-label so the
		// region is at least announced, rather than panicking on every call.
		secCfg.Label = "Section"
	}
	return sectionStyle.WrapHTML(html.Section(secCfg, out...))
}

// ─── FormField ──────────────────────────────────────────────────────

// FormFieldConfig configures a single form field row.
type FormFieldConfig struct {
	Label    string // required → <label>
	For      string // required → <label for=…> matches the input ID
	Help     string // optional helper text under the field
	Error    string // optional error message; non-empty switches to error styling
	Required bool   // adds a visible "required" hint and aria-required
	Input    render.HTML
	Class    string
}

// FormField renders a labelled form field with optional help and error
// text. Wire the input ID to cfg.For for label association.
func FormField(cfg FormFieldConfig) render.HTML {
	if cfg.Label == "" {
		panic("ui: FormField requires Label")
	}
	if cfg.For == "" {
		panic("ui: FormField requires For (the input element's ID)")
	}
	if cfg.Input == "" {
		panic("ui: FormField requires Input")
	}
	cls := "ui-form-field"
	if cfg.Error != "" {
		cls += " is-error"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	labelEl := html.Label(html.LabelConfig{
		For:   cfg.For,
		Text:  cfg.Label,
		Class: "ui-form-field__label",
	})
	if cfg.Required {
		// Wrap label + asterisk in a flex container so they sit on
		// one line inside the grid. The asterisk is aria-hidden so
		// the label's accessible name stays clean.
		labelEl = render.Tag("div", map[string]string{"class": "ui-form-field__label-row"},
			labelEl,
			html.Span(html.TextConfig{
				Class:      "ui-form-field__required",
				ExtraAttrs: html.Attrs{"aria-hidden": "true"},
			}, render.Text(" *")),
		)
	}
	labelHTML := labelEl
	// When the field is in an error state, inject aria-invalid +
	// aria-describedby into the input's first open tag so SR users
	// hear "invalid entry" and the error message text. Without this
	// the visual error (red border) is the only signal — fails
	// WCAG 1.3.1 / 4.1.2 / 1.4.1.
	input := cfg.Input
	if cfg.Error != "" {
		input = injectAriaInvalid(input, cfg.For+"-error")
	} else if cfg.Help != "" {
		input = injectAriaDescribedBy(input, cfg.For+"-help")
	}
	out := []render.HTML{labelHTML, input}
	if cfg.Help != "" {
		out = append(out, html.Paragraph(html.TextConfig{
			Class: "ui-form-field__help", ID: cfg.For + "-help",
		}, render.Text(cfg.Help)))
	}
	if cfg.Error != "" {
		out = append(out, html.Paragraph(html.TextConfig{
			Class:      "ui-form-field__error",
			ID:         cfg.For + "-error",
			ExtraAttrs: html.Attrs{"role": "alert"},
		}, render.Text(cfg.Error)))
	}
	return formFieldStyle.WrapHTML(html.Div(html.DivConfig{Class: cls}, out...))
}

// injectAriaInvalid splices ` aria-invalid="true" aria-describedby="<id>"`
// into the first open tag of the input HTML. Idempotent — won't
// add duplicates.
func injectAriaInvalid(input render.HTML, errID string) render.HTML {
	safe := render.Escape(errID)
	return injectAttrs(input, ` aria-invalid="true" aria-describedby="`+safe+`"`)
}

// injectAriaDescribedBy splices ` aria-describedby="<id>"` for the
// non-error help text case.
func injectAriaDescribedBy(input render.HTML, helpID string) render.HTML {
	safe := render.Escape(helpID)
	return injectAttrs(input, ` aria-describedby="`+safe+`"`)
}

func injectAttrs(input render.HTML, attrs string) render.HTML {
	s := string(input)
	// Idempotence: skip injection only when ALL attribute names in the
	// attrs string are already present on the element. This prevents
	// aria-describedby from being skipped when aria-invalid is already
	// on the tag.
	if allAttrsPresent(s, attrs) {
		return input
	}
	// Find the real open tag, skipping leading whitespace and HTML
	// comments. The splice target is the `>` that closes that tag,
	// respecting attribute quotes (so `>` inside `title="a > b"`
	// doesn't terminate the tag prematurely).
	start := skipNonTagPreamble(s)
	if start < 0 || start >= len(s) || s[start] != '<' {
		return input
	}
	end := findFirstTagClose(s[start:])
	if end < 0 {
		return input
	}
	end += start
	insertAt := end
	if end > 0 && s[end-1] == '/' {
		insertAt = end - 1
	}
	return render.HTML(s[:insertAt] + attrs + s[insertAt:])
}

// skipNonTagPreamble returns the index of the first byte of the
// outermost real open tag, skipping whitespace + HTML comments.
// Returns -1 if no open tag is found.
func skipNonTagPreamble(s string) int {
	i := 0
	for i < len(s) {
		// whitespace
		for i < len(s) {
			c := s[i]
			if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
				i++
				continue
			}
			break
		}
		// HTML comment
		if i+4 <= len(s) && s[i:i+4] == "<!--" {
			end := strings.Index(s[i+4:], "-->")
			if end < 0 {
				return -1
			}
			i = i + 4 + end + 3
			continue
		}
		break
	}
	if i >= len(s) {
		return -1
	}
	return i
}

// findFirstTagClose returns the index of the first `>` that closes
// the open tag at offset 0 of s, respecting attribute quotes.
func findFirstTagClose(s string) int {
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			quote = c
		case '>':
			return i
		}
	}
	return -1
}

// leadingAttrName extracts the attribute name from an attrs string
// like ` aria-invalid="true" aria-describedby="x"` — returns
// "aria-invalid". Used for idempotence: if a tag already has the
// named attribute, skip injection.
func leadingAttrName(attrs string) string {
	a := strings.TrimSpace(attrs)
	eq := strings.IndexByte(a, '=')
	if eq <= 0 {
		return ""
	}
	return a[:eq]
}

// allAttrsPresent returns true when every attribute name in the attrs
// string (e.g. "aria-invalid" and "aria-describedby") is already
// present in the HTML string s. Returns false if any name is missing.
func allAttrsPresent(s, attrs string) bool {
	for _, chunk := range strings.Split(strings.TrimSpace(attrs), " ") {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		eq := strings.IndexByte(chunk, '=')
		if eq <= 0 {
			continue
		}
		name := chunk[:eq]
		if !strings.Contains(s, name+"=") {
			return false
		}
	}
	return true
}

// ─── FormSection ────────────────────────────────────────────────────

// FormSectionConfig groups related fields under a heading + description.
type FormSectionConfig struct {
	Heading     string // optional
	Description string // optional
	Class       string
}

// FormSection wraps a group of FormFields with a shared heading.
//
// Composition: html.FieldSet + a heading-driven legend when a
// heading is provided; otherwise a plain <div> container so screen
// readers don't announce an empty group label.
func FormSection(cfg FormSectionConfig, fields ...render.HTML) render.HTML {
	cls := "ui-form-section"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	if cfg.Heading == "" {
		// No heading → use a plain div, not <fieldset>, to avoid an
		// unlabelled grouping landmark.
		out := []render.HTML{}
		if cfg.Description != "" {
			out = append(out, html.Paragraph(
				html.TextConfig{Class: "ui-form-section__description"},
				render.Text(cfg.Description)))
		}
		out = append(out, html.Div(
			html.DivConfig{Class: "ui-form-section__fields"}, fields...))
		return formSectionStyle.WrapHTML(html.Div(html.DivConfig{Class: cls}, out...))
	}
	out := []render.HTML{}
	if cfg.Description != "" {
		out = append(out, html.Paragraph(
			html.TextConfig{Class: "ui-form-section__description"},
			render.Text(cfg.Description)))
	}
	out = append(out, html.Div(
		html.DivConfig{Class: "ui-form-section__fields"}, fields...))
	return formSectionStyle.WrapHTML(html.FieldSet(
		html.FieldSetConfig{Legend: cfg.Heading, Class: cls},
		out...))
}

// ─── Button ─────────────────────────────────────────────────────────

// ButtonVariant is the semantic variant of a Button. String-typed
// for ergonomic Go enums + readable serialization.
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
	Label      string        // required visible text + aria-label
	Variant    ButtonVariant // defaults to ButtonPrimary
	Size       ButtonSize    // defaults to ButtonSizeDefault
	Type       string        // "button" (default) | "submit" | "reset"
	ExtraAttrs html.Attrs
	ID         string
	Class      string
}

// Button renders a semantic button with a typed variant. Variant
// maps to .ui-button--<variant> in the registered ui-button CSS;
// the framework's styled component handles the visual rules.
//
// Authors never reach for raw class strings — pick a variant.
// Unknown variants panic at render time so typos surface
// immediately rather than silently rendering an unstyled button.
func Button(cfg ButtonConfig) render.HTML {
	if cfg.Label == "" {
		panic("ui: Button requires Label")
	}
	v := cfg.Variant
	if v == "" {
		v = ButtonPrimary
	}
	switch v {
	case ButtonPrimary, ButtonSecondary, ButtonDanger, ButtonGhost:
		// recognized
	default:
		panic("ui: Button unknown Variant " + string(v) +
			" — pick one of: primary, secondary, danger, ghost")
	}
	switch cfg.Size {
	case ButtonSizeDefault, ButtonSizeSmall, ButtonSizeLarge:
		// recognized
	default:
		panic("ui: Button unknown Size " + string(cfg.Size) +
			" — pick one of: \"\" (default), small, large")
	}
	cls := "ui-button ui-button--" + string(v)
	if cfg.Size != ButtonSizeDefault {
		cls += " ui-button--" + string(cfg.Size)
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	// All variants share the single canonical ui-button marker; the
	// .ui-button--<variant> class on the same element drives the
	// visual delta via buttonCSS's variant rules. No legacy per-
	// variant marker / sheet.
	return buttonStyle.WrapHTML(html.Button(html.ButtonConfig{
		Label:      cfg.Label,
		Type:       cfg.Type,
		Class:      cls,
		ID:         cfg.ID,
		ExtraAttrs: cfg.ExtraAttrs,
	}))
}

// ─── LinkButton ─────────────────────────────────────────────────────

// LinkButtonConfig configures a button-styled <a> link. Use this when
// the affordance navigates (changes URL) — CTAs like "Get started",
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
	// External does not also need to "suppress SPA nav" — the
	// underlying SPA router already does the right thing.
	External   bool
	ID         string
	Class      string
	ExtraAttrs html.Attrs
}

// LinkButton renders a button-styled anchor. Same variant/size grammar
// as Button — the visual styling is shared via the registered ui-button
// CSS (class-based, not tag-scoped). The difference is semantic:
// <a> for navigation, <button> for actions. Screen readers, "open in
// new tab", and SPA push-state nav all rely on the right tag choice.
func LinkButton(cfg LinkButtonConfig) render.HTML {
	if cfg.Label == "" {
		panic("ui: LinkButton requires Label")
	}
	if cfg.Href == "" {
		panic("ui: LinkButton requires Href — use Button for non-navigating actions")
	}
	// Refuse dangerous schemes at render time. The runtime's SPA
	// navigator screens these too, but a direct anchor click bypasses
	// the SPA interceptor (browser handles it natively), so the
	// rendered href must already be safe. javascript:/vbscript:/non-
	// image data: are the canonical XSS vectors for links.
	if isUnsafeScheme(cfg.Href) {
		panic("ui: LinkButton refuses unsafe Href scheme: " + cfg.Href)
	}
	v := cfg.Variant
	if v == "" {
		v = ButtonPrimary
	}
	switch v {
	case ButtonPrimary, ButtonSecondary, ButtonDanger, ButtonGhost:
	default:
		panic("ui: LinkButton unknown Variant " + string(v) +
			" — pick one of: primary, secondary, danger, ghost")
	}
	switch cfg.Size {
	case ButtonSizeDefault, ButtonSizeSmall, ButtonSizeLarge:
	default:
		panic("ui: LinkButton unknown Size " + string(cfg.Size) +
			" — pick one of: \"\" (default), small, large")
	}
	cls := "ui-button ui-button--" + string(v)
	if cfg.Size != ButtonSizeDefault {
		cls += " ui-button--" + string(cfg.Size)
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	extra := html.Attrs{}
	for k, val := range cfg.ExtraAttrs {
		extra[k] = val
	}
	if cfg.External {
		extra["target"] = "_blank"
		extra["rel"] = "noopener noreferrer"
	}
	return buttonStyle.WrapHTML(html.Link(html.LinkConfig{
		Href: cfg.Href, Text: cfg.Label, Class: cls, ID: cfg.ID,
		ExtraAttrs: extra,
	}))
}

// isUnsafeScheme rejects the canonical XSS vectors for `href`/`src`
// attributes — javascript:, vbscript:, and non-image data: URIs. Used
// by LinkButton (render-time guard) and shadowed by the runtime's
// _isUnsafeSignalUrl for programmatic SPA navigation.
func isUnsafeScheme(href string) bool {
	// Browsers strip ASCII whitespace and control bytes from a URL —
	// including bytes INTERIOR to the scheme token ("java\tscript:" is
	// resolved as "javascript:") — before scheme resolution. A leading-
	// only strip therefore misses "java\tscript:". Remove every ASCII
	// control byte and space anywhere in the string before matching, so
	// the deny-list sees the same scheme the browser will.
	var b strings.Builder
	b.Grow(len(href))
	for i := 0; i < len(href); i++ {
		c := href[i]
		if c == ' ' || c <= 0x1f || c == 0x7f {
			continue
		}
		b.WriteByte(c)
	}
	s := b.String()
	// Case-insensitive prefix check.
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "javascript:") {
		return true
	}
	if strings.HasPrefix(lower, "vbscript:") {
		return true
	}
	if strings.HasPrefix(lower, "data:") {
		// Allow data:image/* only.
		return !strings.HasPrefix(lower, "data:image/")
	}
	return false
}

// ─── StatusBadge ────────────────────────────────────────────────────

// StatusVariant is the semantic variant of a StatusBadge.
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
}

// StatusBadge renders a small inline pill conveying state.
func StatusBadge(cfg StatusBadgeConfig) render.HTML {
	if cfg.Label == "" {
		panic("ui: StatusBadge requires Label")
	}
	v := cfg.Variant
	if v == "" {
		v = StatusNeutral
	}
	switch v {
	case StatusSuccess, StatusWarning, StatusDanger, StatusInfo, StatusNeutral:
	default:
		panic("ui: StatusBadge unknown Variant " + string(v) + " — pick one of: success, warning, danger, info, neutral")
	}
	cls := "ui-badge ui-badge--" + string(v)
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	return statusBadgeStyle.WrapHTML(html.Span(html.TextConfig{Class: cls, ID: cfg.ID},
		render.Text(cfg.Label)))
}

// ─── EmptyState ─────────────────────────────────────────────────────

// EmptyStateConfig configures an empty-state surface.
type EmptyStateConfig struct {
	Title       string      // required
	Description string      // optional supporting text
	Action      render.HTML // optional CTA (e.g. a button or link)
	ID          string
	Class       string
}

// EmptyState renders a centered title + description + optional CTA for
// blank lists or zero-data screens.
//
// Composition: html.Heading (h3) + html.Paragraph + a div for
// the action slot.
func EmptyState(cfg EmptyStateConfig) render.HTML {
	if cfg.Title == "" {
		panic("ui: EmptyState requires Title")
	}
	cls := "ui-empty-state"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	out := []render.HTML{
		html.Heading(html.HeadingConfig{
			Level: 3, Class: "ui-empty-state__title",
		}, render.Text(cfg.Title)),
	}
	if cfg.Description != "" {
		out = append(out, html.Paragraph(
			html.TextConfig{Class: "ui-empty-state__description"},
			render.Text(cfg.Description)))
	}
	if cfg.Action != "" {
		out = append(out, html.Div(
			html.DivConfig{Class: "ui-empty-state__action"}, cfg.Action))
	}
	return emptyStateStyle.WrapHTML(html.Div(html.DivConfig{Class: cls, ID: cfg.ID}, out...))
}

// ─── Callout ────────────────────────────────────────────────────────

// CalloutConfig configures a persistent informational block.
type CalloutConfig struct {
	Title   string
	Variant StatusVariant // info | success | warning | danger | neutral
	ID      string
	Class   string
}

// Callout renders a persistent info/warning/error block. Distinct from
// Toast / Notification (ephemeral) — Callouts live inline with content.
//
// Composition: html.Aside (which auto-applies role=complementary
// and requires an aria-label, here derived from Title or variant).
// Falls through to a plain <div> with the appropriate role when no
// Title is set, so the variant-driven role takes precedence over a
// generic "complementary" landmark.
func Callout(cfg CalloutConfig, body ...render.HTML) render.HTML {
	v := cfg.Variant
	if v == "" {
		v = StatusInfo
	}
	switch v {
	case StatusSuccess, StatusWarning, StatusDanger, StatusInfo, StatusNeutral:
	default:
		panic("ui: Callout unknown Variant " + string(v) + " — pick one of: success, warning, danger, info, neutral")
	}
	cls := "ui-callout ui-callout--" + string(v)
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	out := []render.HTML{}
	if cfg.Title != "" {
		out = append(out, html.Strong(
			html.TextConfig{Class: "ui-callout__title"},
			render.Text(cfg.Title)))
	}
	if len(body) > 0 {
		out = append(out, html.Div(
			html.DivConfig{Class: "ui-callout__body"}, body...))
	}

	// We want role="alert" on danger/warning callouts; html.Aside
	// always applies role=complementary, so for those variants we use
	// a div + explicit role via html.Div+Attrs.
	role := calloutRole(v)
	if role == "alert" {
		return calloutStyle.WrapHTML(html.Div(html.DivConfig{
			Class: cls, ID: cfg.ID, Role: "alert",
		}, out...))
	}
	// Note "info" role: html.Aside requires Label/LabelledBy. Use
	// the variant name as a safe fallback when no Title is provided.
	label := cfg.Title
	if label == "" {
		label = string(v) + " note"
	}
	return calloutStyle.WrapHTML(html.Aside(html.AsideConfig{
		Class: cls, ID: cfg.ID, Label: label,
	}, out...))
}

func calloutRole(v StatusVariant) string {
	switch v {
	case StatusDanger, StatusWarning:
		return "alert"
	default:
		return "note"
	}
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
}

// StatCard renders a metric card — label, value, optional trend pill.
func StatCard(cfg StatCardConfig) render.HTML {
	if cfg.Label == "" {
		panic("ui: StatCard requires Label")
	}
	if cfg.Value == "" {
		panic("ui: StatCard requires Value")
	}
	cls := "ui-stat-card"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	out := []render.HTML{
		html.Paragraph(html.TextConfig{Class: "ui-stat-card__label"}, render.Text(cfg.Label)),
		html.Paragraph(html.TextConfig{Class: "ui-stat-card__value"}, render.Text(cfg.Value)),
	}
	if cfg.Trend != "" {
		dir := cfg.Direction
		if dir == "" {
			dir = TrendFlat
		}
		out = append(out, html.Paragraph(
			html.TextConfig{Class: "ui-stat-card__trend ui-stat-card__trend--" + string(dir)},
			render.Text(cfg.Trend)))
	}
	return statCardStyle.WrapHTML(html.Div(html.DivConfig{Class: cls, ID: cfg.ID}, out...))
}

// ─── Avatar ─────────────────────────────────────────────────────────

// AvatarSize is one of a small set of pre-defined avatar sizes.
// Sizes are CSS classes — no inline styles — so a strict CSP that
// blocks `style="…"` attributes still works.
type AvatarSize string

const (
	AvatarSm AvatarSize = "sm" // ~1.5rem
	AvatarMd AvatarSize = ""   // default ~2.5rem
	AvatarLg AvatarSize = "lg" // ~3rem
	AvatarXl AvatarSize = "xl" // ~4rem
)

// AvatarConfig configures an avatar.
type AvatarConfig struct {
	// Name is required; used for alt text and to derive initials when
	// no image source is set.
	Name  string
	Src   string     // optional image URL; falls back to initials when empty
	Size  AvatarSize // sm | "" (default md) | lg | xl
	ID    string
	Class string
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
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	spanCfg := html.TextConfig{
		Class: cls, ID: cfg.ID,
	}
	if cfg.Src != "" {
		return avatarStyle.WrapHTML(html.Span(spanCfg,
			html.Image(html.ImageConfig{
				Src: cfg.Src, Alt: cfg.Name, Class: "ui-avatar__img",
			})))
	}
	return avatarStyle.WrapHTML(html.Span(spanCfg,
		html.Span(html.TextConfig{
			Class:      "ui-avatar__initials",
			ExtraAttrs: html.Attrs{"aria-hidden": "true"},
		}, render.Text(initials(cfg.Name))),
		html.Span(html.TextConfig{Class: "ui-visually-hidden"},
			render.Text(cfg.Name)),
	))
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
	// per-token markup — pass already-escaped, trusted HTML.
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
	ID          string
	Class       string
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
	framed := cfg.Filename != "" || cfg.ShowCopy || cfg.LineNumbers || len(cfg.Lines) > 0
	label := "source code"
	if cfg.Language != "" {
		label = cfg.Language + " source"
	}

	// Body <pre>. WCAG 2.1.1: tabindex=0 so keyboard users can pan the
	// horizontal scroll. (role=region is intentionally avoided — it would
	// make every block a landmark and fail landmark-unique.)
	bodyID := cfg.ID
	if framed && cfg.ShowCopy && bodyID == "" {
		bodyID = "ui-code-block-" + strconv.FormatUint(codeBlockSeq.Add(1), 10)
	}
	var body render.HTML
	if len(cfg.Lines) > 0 {
		wrapped := make([]render.HTML, len(cfg.Lines))
		for i, ln := range cfg.Lines {
			wrapped[i] = html.Span(html.TextConfig{Class: "ui-code-block__line"}, ln)
		}
		body = render.Join(wrapped...)
	} else {
		body = render.Tag("code", nil, render.HTML(escapeHTML(cfg.Code)))
	}

	if !framed {
		cls := "ui-code-block"
		if cfg.Class != "" {
			cls += " " + cfg.Class
		}
		preAttrs := map[string]string{"class": cls, "tabindex": "0", "aria-label": label}
		if bodyID != "" {
			preAttrs["id"] = bodyID
		}
		return codeBlockStyle.WrapHTML(render.Tag("pre", preAttrs, body))
	}

	cls := "ui-code-block ui-code-block--framed"
	if cfg.LineNumbers {
		cls += " ui-code-block--numbered"
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
		html.Div(html.DivConfig{Class: cls, ID: cfg.ID}, head, pre))
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
//	// Or with no Target — defaults to "main-content".
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
	cls := "ui-skip-link"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	return skipLinkStyle.WrapHTML(
		html.Link(html.LinkConfig{
			Href:  "#" + target,
			Text:  text,
			Class: cls,
			ID:    cfg.ID,
		}),
	)
}

// escapeHTML is the minimal entity-escape sufficient for code
// content (we only emit text + tag context — no attribute use here).
func escapeHTML(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
	)
	return r.Replace(s)
}
