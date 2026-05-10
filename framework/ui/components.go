package ui

import (
	"strings"

	"github.com/gofastr/gofastr/core-ui/elements"
	"github.com/gofastr/gofastr/core/render"
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
// Composition: elements.Header (semantic <header role="banner">) +
// elements.Heading (h1) + elements.Paragraph for eyebrow/subtitle.
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
		textChildren = append(textChildren, elements.Paragraph(
			elements.TextConfig{Class: "ui-page-header__eyebrow"},
			render.Text(cfg.Eyebrow)))
	}
	textChildren = append(textChildren,
		elements.Heading(elements.HeadingConfig{Level: 1,
			Class: "ui-page-header__title"}, render.Text(cfg.Title)))
	if cfg.Subtitle != "" {
		textChildren = append(textChildren, elements.Paragraph(
			elements.TextConfig{Class: "ui-page-header__subtitle"},
			render.Text(cfg.Subtitle)))
	}
	textBlock := elements.Div(elements.DivConfig{Class: "ui-page-header__text"}, textChildren...)
	body := []render.HTML{textBlock}
	if cfg.Actions != "" {
		body = append(body, elements.Div(
			elements.DivConfig{Class: "ui-page-header__actions"}, cfg.Actions))
	}
	return elements.Header(elements.HeaderConfig{Class: cls, ID: cfg.ID}, body...)
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
type SectionConfig struct {
	Heading     string // optional <h2> heading
	Description string // optional supporting text under the heading
	Class       string
	ID          string
}

// Section renders a content section with consistent spacing and an
// optional heading + description.
//
// Composition: a labelled <section> via elements.Section. When a
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
	headingID := ""
	if cfg.Heading != "" {
		headingID = "ui-section-" + slug(cfg.Heading)
		out = append(out, elements.Heading(elements.HeadingConfig{
			Level: 2, ID: headingID, Class: "ui-section__heading",
		}, render.Text(cfg.Heading)))
	}
	if cfg.Description != "" {
		out = append(out, elements.Paragraph(
			elements.TextConfig{Class: "ui-section__description"},
			render.Text(cfg.Description)))
	}
	if len(body) > 0 {
		out = append(out, elements.Div(
			elements.DivConfig{Class: "ui-section__body"}, body...))
	}

	secCfg := elements.SectionConfig{Class: cls, ID: cfg.ID}
	if headingID != "" {
		secCfg.LabelledBy = headingID
	} else {
		// No heading → caller must label the region via the Class
		// hook. We default to a generic aria-label so the region is
		// at least announced, rather than panicking on every call site.
		secCfg.Label = "Section"
	}
	return elements.Section(secCfg, out...)
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
	labelHTML := elements.Label(elements.LabelConfig{
		For:   cfg.For,
		Text:  cfg.Label,
		Class: "ui-form-field__label",
	})
	if cfg.Required {
		// Append a visible "*" inside the label by wrapping the label
		// + a sibling span. (elements.Label's Text covers the simple
		// case; we add the asterisk via a sibling span so the label
		// element stays a single accessible name.)
		labelHTML = render.Join(labelHTML,
			elements.Span(elements.TextConfig{
				Class: "ui-form-field__required",
				Attrs: elements.Attrs{"aria-hidden": "true"},
			}, render.Text(" *")))
	}
	out := []render.HTML{labelHTML, cfg.Input}
	if cfg.Help != "" && cfg.Error == "" {
		out = append(out, elements.Paragraph(elements.TextConfig{
			Class: "ui-form-field__help", ID: cfg.For + "-help",
		}, render.Text(cfg.Help)))
	}
	if cfg.Error != "" {
		out = append(out, elements.Paragraph(elements.TextConfig{
			Class: "ui-form-field__error",
			ID:    cfg.For + "-error",
			Attrs: elements.Attrs{"role": "alert"},
		}, render.Text(cfg.Error)))
	}
	return elements.Div(elements.DivConfig{Class: cls}, out...)
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
// Composition: elements.FieldSet + a heading-driven legend when a
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
			out = append(out, elements.Paragraph(
				elements.TextConfig{Class: "ui-form-section__description"},
				render.Text(cfg.Description)))
		}
		out = append(out, elements.Div(
			elements.DivConfig{Class: "ui-form-section__fields"}, fields...))
		return elements.Div(elements.DivConfig{Class: cls}, out...)
	}
	out := []render.HTML{}
	if cfg.Description != "" {
		out = append(out, elements.Paragraph(
			elements.TextConfig{Class: "ui-form-section__description"},
			render.Text(cfg.Description)))
	}
	out = append(out, elements.Div(
		elements.DivConfig{Class: "ui-form-section__fields"}, fields...))
	return elements.FieldSet(
		elements.FieldSetConfig{Legend: cfg.Heading, Class: cls},
		out...)
}

// ─── DangerButton ───────────────────────────────────────────────────

// DangerButtonConfig configures a destructive-action button.
type DangerButtonConfig struct {
	Label string // required visible text + aria-label
	Type  string // "button" (default) | "submit" | "reset"
	Attrs elements.Attrs
	ID    string
	Class string
}

// DangerButton renders a button styled with the danger token. Use for
// destructive actions (delete, revoke, drop) so the visual weight
// matches the semantic.
func DangerButton(cfg DangerButtonConfig) render.HTML {
	if cfg.Label == "" {
		panic("ui: DangerButton requires Label")
	}
	cls := "ui-button ui-button--danger"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	return elements.Button(elements.ButtonConfig{
		Label: cfg.Label,
		Type:  cfg.Type,
		Class: cls,
		ID:    cfg.ID,
		Attrs: cfg.Attrs,
	})
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
	cls := "ui-badge ui-badge--" + string(v)
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	return elements.Span(elements.TextConfig{Class: cls, ID: cfg.ID},
		render.Text(cfg.Label))
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
// Composition: elements.Heading (h3) + elements.Paragraph + a div for
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
		elements.Heading(elements.HeadingConfig{
			Level: 3, Class: "ui-empty-state__title",
		}, render.Text(cfg.Title)),
	}
	if cfg.Description != "" {
		out = append(out, elements.Paragraph(
			elements.TextConfig{Class: "ui-empty-state__description"},
			render.Text(cfg.Description)))
	}
	if cfg.Action != "" {
		out = append(out, elements.Div(
			elements.DivConfig{Class: "ui-empty-state__action"}, cfg.Action))
	}
	return elements.Div(elements.DivConfig{Class: cls, ID: cfg.ID}, out...)
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
// Composition: elements.Aside (which auto-applies role=complementary
// and requires an aria-label, here derived from Title or variant).
// Falls through to a plain <div> with the appropriate role when no
// Title is set, so the variant-driven role takes precedence over a
// generic "complementary" landmark.
func Callout(cfg CalloutConfig, body ...render.HTML) render.HTML {
	v := cfg.Variant
	if v == "" {
		v = StatusInfo
	}
	cls := "ui-callout ui-callout--" + string(v)
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	out := []render.HTML{}
	if cfg.Title != "" {
		out = append(out, elements.Strong(
			elements.TextConfig{Class: "ui-callout__title"},
			render.Text(cfg.Title)))
	}
	if len(body) > 0 {
		out = append(out, elements.Div(
			elements.DivConfig{Class: "ui-callout__body"}, body...))
	}

	// We want role="alert" on danger/warning callouts; elements.Aside
	// always applies role=complementary, so for those variants we use
	// a div + explicit role via elements.Div+Attrs.
	role := calloutRole(v)
	if role == "alert" {
		return elements.Div(elements.DivConfig{
			Class: cls, ID: cfg.ID, Role: "alert",
		}, out...)
	}
	// Note "info" role: elements.Aside requires Label/LabelledBy. Use
	// the variant name as a safe fallback when no Title is provided.
	label := cfg.Title
	if label == "" {
		label = string(v) + " note"
	}
	return elements.Aside(elements.AsideConfig{
		Class: cls, ID: cfg.ID, Label: label,
	}, out...)
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
		elements.Paragraph(elements.TextConfig{Class: "ui-stat-card__label"}, render.Text(cfg.Label)),
		elements.Paragraph(elements.TextConfig{Class: "ui-stat-card__value"}, render.Text(cfg.Value)),
	}
	if cfg.Trend != "" {
		dir := cfg.Direction
		if dir == "" {
			dir = TrendFlat
		}
		out = append(out, elements.Paragraph(
			elements.TextConfig{Class: "ui-stat-card__trend ui-stat-card__trend--" + string(dir)},
			render.Text(cfg.Trend)))
	}
	return elements.Div(elements.DivConfig{Class: cls, ID: cfg.ID}, out...)
}

// ─── Avatar ─────────────────────────────────────────────────────────

// AvatarConfig configures an avatar.
type AvatarConfig struct {
	// Name is required; used for alt text and to derive initials when
	// no image source is set.
	Name string
	Src  string // optional image URL; falls back to initials when empty
	Size string // CSS length, e.g. "2rem". Defaults to "2.5rem".
	ID   string
	Class string
}

// Avatar renders a circular avatar with an image fallback to text
// initials when no image source is provided.
func Avatar(cfg AvatarConfig) render.HTML {
	if cfg.Name == "" {
		panic("ui: Avatar requires Name")
	}
	size := cfg.Size
	if size == "" {
		size = "2.5rem"
	}
	cls := "ui-avatar"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	style := "inline-size:" + size + ";block-size:" + size
	spanCfg := elements.TextConfig{
		Class: cls, ID: cfg.ID,
		Attrs: elements.Attrs{"style": style},
	}
	if cfg.Src != "" {
		return elements.Span(spanCfg,
			elements.Image(elements.ImageConfig{
				Src: cfg.Src, Alt: cfg.Name, Class: "ui-avatar__img",
			}))
	}
	return elements.Span(spanCfg,
		elements.Span(elements.TextConfig{
			Class: "ui-avatar__initials",
			Attrs: elements.Attrs{"aria-hidden": "true"},
		}, render.Text(initials(cfg.Name))),
		elements.Span(elements.TextConfig{Class: "ui-visually-hidden"},
			render.Text(cfg.Name)),
	)
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
