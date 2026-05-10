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
func PageHeader(cfg PageHeaderConfig) render.HTML {
	if cfg.Title == "" {
		panic("ui: PageHeader requires Title")
	}
	cls := "ui-page-header"
	if cfg.Class != "" {
		cls = cls + " " + cfg.Class
	}
	attrs := map[string]string{"class": cls}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	textChildren := []render.HTML{}
	if cfg.Eyebrow != "" {
		textChildren = append(textChildren, render.Tag("p",
			map[string]string{"class": "ui-page-header__eyebrow"}, render.Text(cfg.Eyebrow)))
	}
	textChildren = append(textChildren,
		elements.Heading(elements.HeadingConfig{Level: 1,
			Class: "ui-page-header__title"}, render.Text(cfg.Title)))
	if cfg.Subtitle != "" {
		textChildren = append(textChildren, render.Tag("p",
			map[string]string{"class": "ui-page-header__subtitle"}, render.Text(cfg.Subtitle)))
	}
	textBlock := render.Tag("div", map[string]string{"class": "ui-page-header__text"}, textChildren...)
	if cfg.Actions == "" {
		return render.Tag("header", attrs, textBlock)
	}
	return render.Tag("header", attrs, textBlock,
		render.Tag("div", map[string]string{"class": "ui-page-header__actions"}, cfg.Actions))
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
func Section(cfg SectionConfig, body ...render.HTML) render.HTML {
	cls := "ui-section"
	if cfg.Class != "" {
		cls = cls + " " + cfg.Class
	}
	attrs := map[string]string{"class": cls}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	out := []render.HTML{}
	if cfg.Heading != "" {
		out = append(out, elements.Heading(elements.HeadingConfig{Level: 2,
			Class: "ui-section__heading"}, render.Text(cfg.Heading)))
	}
	if cfg.Description != "" {
		out = append(out, render.Tag("p",
			map[string]string{"class": "ui-section__description"}, render.Text(cfg.Description)))
	}
	if len(body) > 0 {
		out = append(out, render.Tag("div",
			map[string]string{"class": "ui-section__body"}, body...))
	}
	return render.Tag("section", attrs, out...)
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
	labelChildren := []render.HTML{render.Text(cfg.Label)}
	if cfg.Required {
		labelChildren = append(labelChildren,
			render.Tag("span",
				map[string]string{"class": "ui-form-field__required", "aria-hidden": "true"},
				render.Text(" *")))
	}
	out := []render.HTML{
		render.Tag("label", map[string]string{
			"for":   cfg.For,
			"class": "ui-form-field__label",
		}, labelChildren...),
		cfg.Input,
	}
	if cfg.Help != "" && cfg.Error == "" {
		out = append(out, render.Tag("p", map[string]string{
			"class": "ui-form-field__help", "id": cfg.For + "-help",
		}, render.Text(cfg.Help)))
	}
	if cfg.Error != "" {
		out = append(out, render.Tag("p", map[string]string{
			"class": "ui-form-field__error",
			"id":    cfg.For + "-error",
			"role":  "alert",
		}, render.Text(cfg.Error)))
	}
	return render.Tag("div", map[string]string{"class": cls}, out...)
}

// ─── FormSection ────────────────────────────────────────────────────

// FormSectionConfig groups related fields under a heading + description.
type FormSectionConfig struct {
	Heading     string // optional
	Description string // optional
	Class       string
}

// FormSection wraps a group of FormFields with a shared heading.
func FormSection(cfg FormSectionConfig, fields ...render.HTML) render.HTML {
	cls := "ui-form-section"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	out := []render.HTML{}
	if cfg.Heading != "" {
		out = append(out, render.Tag("h3",
			map[string]string{"class": "ui-form-section__heading"}, render.Text(cfg.Heading)))
	}
	if cfg.Description != "" {
		out = append(out, render.Tag("p",
			map[string]string{"class": "ui-form-section__description"}, render.Text(cfg.Description)))
	}
	out = append(out, render.Tag("div",
		map[string]string{"class": "ui-form-section__fields"}, fields...))
	return render.Tag("fieldset", map[string]string{"class": cls}, out...)
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
	attrs := map[string]string{"class": cls}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	return render.Tag("span", attrs, render.Text(cfg.Label))
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
func EmptyState(cfg EmptyStateConfig) render.HTML {
	if cfg.Title == "" {
		panic("ui: EmptyState requires Title")
	}
	cls := "ui-empty-state"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	attrs := map[string]string{"class": cls}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	out := []render.HTML{render.Tag("h3",
		map[string]string{"class": "ui-empty-state__title"}, render.Text(cfg.Title))}
	if cfg.Description != "" {
		out = append(out, render.Tag("p",
			map[string]string{"class": "ui-empty-state__description"}, render.Text(cfg.Description)))
	}
	if cfg.Action != "" {
		out = append(out, render.Tag("div",
			map[string]string{"class": "ui-empty-state__action"}, cfg.Action))
	}
	return render.Tag("div", attrs, out...)
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
// Toast (ephemeral) — Callouts live inline with content.
func Callout(cfg CalloutConfig, body ...render.HTML) render.HTML {
	v := cfg.Variant
	if v == "" {
		v = StatusInfo
	}
	cls := "ui-callout ui-callout--" + string(v)
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	attrs := map[string]string{"class": cls, "role": calloutRole(v)}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	out := []render.HTML{}
	if cfg.Title != "" {
		out = append(out, render.Tag("strong",
			map[string]string{"class": "ui-callout__title"}, render.Text(cfg.Title)))
	}
	if len(body) > 0 {
		out = append(out, render.Tag("div",
			map[string]string{"class": "ui-callout__body"}, body...))
	}
	return render.Tag("aside", attrs, out...)
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
	attrs := map[string]string{"class": cls}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	out := []render.HTML{
		render.Tag("p", map[string]string{"class": "ui-stat-card__label"}, render.Text(cfg.Label)),
		render.Tag("p", map[string]string{"class": "ui-stat-card__value"}, render.Text(cfg.Value)),
	}
	if cfg.Trend != "" {
		dir := cfg.Direction
		if dir == "" {
			dir = TrendFlat
		}
		out = append(out, render.Tag("p",
			map[string]string{"class": "ui-stat-card__trend ui-stat-card__trend--" + string(dir)},
			render.Text(cfg.Trend)))
	}
	return render.Tag("div", attrs, out...)
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
	attrs := map[string]string{
		"class": cls,
		"style": "inline-size:" + size + ";block-size:" + size,
	}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	if cfg.Src != "" {
		return render.Tag("span", attrs,
			elements.Image(elements.ImageConfig{
				Src: cfg.Src, Alt: cfg.Name, Class: "ui-avatar__img",
			}))
	}
	return render.Tag("span", attrs,
		render.Tag("span", map[string]string{
			"class":       "ui-avatar__initials",
			"aria-hidden": "true",
		}, render.Text(initials(cfg.Name))),
		render.Tag("span", map[string]string{"class": "ui-visually-hidden"},
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
