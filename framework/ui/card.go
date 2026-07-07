package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── Card ───────────────────────────────────────────────────────────

// CardVariant selects the chrome treatment. Apps extend the set with
// RegisterCardVariant; unregistered values panic at render.
type CardVariant string

const (
	// CardElevated is the default — surface + shadow + radius.
	CardElevated CardVariant = ""
	// CardOutlined draws a 1px border instead of a shadow.
	CardOutlined CardVariant = "outlined"
	// CardFlat drops both the border and the shadow.
	CardFlat CardVariant = "flat"
)

// CardConfig configures a card.
type CardConfig struct {
	// Heading is the optional top-of-card title. When set, a labelled
	// <section> wraps the card so screen readers pick up the heading
	// as the region name.
	//
	// The heading's id (and the section's aria-labelledby target) is
	// derived from the heading text — "ui-card-" + slug(Heading) — so
	// it is deterministic across re-renders. The trade-off, shared with
	// html.Heading: two cards with EQUAL heading text on one page
	// produce duplicate ids (the render function has no page-wide
	// context to de-dupe against). cfg.ID sets the section wrapper's own
	// anchor id, NOT the heading id, so it does not de-dupe the
	// collision; when a page repeats heading text, use Header for full
	// control over the heading element and its id.
	Heading string

	// HeadingLevel overrides the heading element level (default 3).
	// Set to 2 when the card is a top-level page section (e.g. a
	// dashboard widget directly under the page <h1>) so the heading
	// outline doesn't skip from h1 to h3.
	HeadingLevel int

	// Description is optional supporting text rendered beneath the
	// heading.
	Description string

	// Header overrides the auto-rendered Heading/Description block.
	// Use when the header needs more than a title — e.g. a row with an
	// avatar and trailing actions.
	Header render.HTML

	// Footer renders below the body, separated by a hairline border.
	// Common usage: button row, last-updated timestamp, status pill.
	Footer render.HTML

	// Interactive flips the surface to a focusable, hover-able link
	// shell. When set, the card renders as an <a> wrapping a <section>
	// so the entire surface activates on click.
	Href string

	Variant CardVariant
	ID      string
	Class   string
}

// Card renders a labelled content card with optional header, footer,
// and interactive (linked) shell.
//
// Composition:
//   - With Heading: <section aria-labelledby="…"> → <h3> + <p> + body + footer
//   - With Header:  the caller's Header HTML replaces the auto-block
//   - With Href:    the whole shell becomes a focusable <a>, with the
//     internal landmark still present for screen readers
func Card(cfg CardConfig, body ...render.HTML) render.HTML {
	// Unknown variants panic like every other variant-taking component
	// (registered custom variants pass — see RegisterCardVariant).
	// Card used to emit ui-card--<anything> silently; that let typos
	// ship unstyled cards with no signal.
	checkCardVariant(cfg.Variant)
	cls := "ui-card"
	if cfg.Variant != CardElevated {
		cls += " ui-card--" + string(cfg.Variant)
	}
	if cfg.Href != "" {
		cls += " ui-card--interactive"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	headingID := ""
	out := []render.HTML{}
	if cfg.Header != "" {
		out = append(out, html.Div(html.DivConfig{Class: "ui-card__header"}, cfg.Header))
	} else if cfg.Heading != "" {
		headingID = "ui-card-" + slug(cfg.Heading)
		level := cfg.HeadingLevel
		if level < 1 || level > 6 {
			level = 3
		}
		hdr := []render.HTML{
			html.Heading(html.HeadingConfig{Level: level, ID: headingID, Class: "ui-card__heading"},
				render.Text(cfg.Heading)),
		}
		if cfg.Description != "" {
			hdr = append(hdr, html.Paragraph(
				html.TextConfig{Class: "ui-card__description"},
				render.Text(cfg.Description)))
		}
		out = append(out, html.Div(html.DivConfig{Class: "ui-card__header"}, hdr...))
	}
	if len(body) > 0 {
		out = append(out, html.Div(html.DivConfig{Class: "ui-card__body"}, body...))
	}
	if cfg.Footer != "" {
		out = append(out, html.Div(html.DivConfig{Class: "ui-card__footer"}, cfg.Footer))
	}

	// Linked variant: outer <a> wraps the whole card. The anchor's
	// text content is what assistive tech announces, so an inner
	// landmark would be redundant — keep the inner shell as a div.
	if cfg.Href != "" {
		// Drop unsafe hrefs (javascript:, data:, control bytes, …) —
		// same allow-list as ui.Link; see framework/ui/safety.go. Card
		// is a content-level component, so a rejected href degrades to
		// an inert "#" rather than panicking.
		href := safeURL(cfg.Href)
		if href == "" {
			href = "#"
		}
		inner := html.Div(html.DivConfig{Class: "ui-card__inner"}, out...)
		return cardStyle.WrapHTML(html.LinkHTML(html.LinkHTMLConfig{
			Href:    href,
			Class:   cls,
			ID:      cfg.ID,
			Content: inner,
		}))
	}

	if headingID != "" {
		return cardStyle.WrapHTML(html.Section(html.SectionConfig{
			Class: cls, ID: cfg.ID, LabelledBy: headingID,
		}, out...))
	}
	return cardStyle.WrapHTML(html.Div(html.DivConfig{Class: cls, ID: cfg.ID}, out...))
}
