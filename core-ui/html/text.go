package html

import (
	"fmt"
	"html"
	"regexp"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// HeadingConfig configures an <h1>–<h6> element.
// Required: Level (1-6).
type HeadingConfig struct {
	Level      int // required: 1-6
	Class      string
	ID         string
	ExtraAttrs Attrs
}

// TextConfig configures a text container element (Paragraph, Span, Strong, Em, etc.).
// No required fields — used for generic text containers.
type TextConfig struct {
	Class      string
	ID         string
	ExtraAttrs Attrs
}

// AbbrConfig configures an <abbr> element.
// Required: Title.
type AbbrConfig struct {
	Title      string // required: full expansion
	Class      string
	ID         string
	ExtraAttrs Attrs
}

// TimeConfig configures a <time> element.
// Required: Datetime.
type TimeConfig struct {
	Datetime   string // required: machine-readable datetime
	Class      string
	ID         string
	ExtraAttrs Attrs
}

// nonAlphaNum matches runs of non-alphanumeric characters for slug generation.
var nonAlphaNum = regexp.MustCompile(`[^a-zA-Z0-9]+`)

// slugify converts a string to a URL-friendly slug for use as an HTML id.
// It first unescapes HTML entities (e.g. &#39; → ') so that escaped
// apostrophes and other characters don't leak numeric entity codes
// into the slug.
func slugify(s string) string {
	s = html.UnescapeString(s)
	s = strings.ToLower(strings.TrimSpace(s))
	s = nonAlphaNum.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 64 {
		s = s[:64]
	}
	return s
}

// Heading produces an <h1> through <h6> element.
// Required: Level (1-6). Auto-generates an id attribute from the text content
// of children for aria-labelledby references. If cfg.ID is set, it is used instead.
func Heading(cfg HeadingConfig, children ...render.HTML) render.HTML {
	level := cfg.Level
	if level < 1 || level > 6 {
		panic(fmt.Sprintf("html: Heading Level must be 1-6, got %d", level))
	}
	tag := fmt.Sprintf("h%d", level)

	attrs := buildAttrs(cfg.ExtraAttrs, cfg.ID, cfg.Class)

	// Auto-generate id from children if not already set.
	if _, ok := attrs["id"]; !ok {
		var text strings.Builder
		for _, c := range children {
			text.WriteString(string(c))
		}
		id := slugify(text.String())
		if id != "" {
			setAttr(attrs, "id", "heading-"+id)
		}
	}

	return render.Tag(tag, attrs, children...)
}

// Paragraph produces a <p> element.
func Paragraph(cfg TextConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.ExtraAttrs, cfg.ID, cfg.Class)
	return render.Tag("p", attrs, children...)
}

// Span produces a <span> element.
func Span(cfg TextConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.ExtraAttrs, cfg.ID, cfg.Class)
	return render.Tag("span", attrs, children...)
}

// Strong produces a <strong> element for strong importance.
func Strong(cfg TextConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.ExtraAttrs, cfg.ID, cfg.Class)
	return render.Tag("strong", attrs, children...)
}

// Em produces an <em> element for stress emphasis.
func Em(cfg TextConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.ExtraAttrs, cfg.ID, cfg.Class)
	return render.Tag("em", attrs, children...)
}

// Code produces a <code> element for inline code fragments.
func Code(cfg TextConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.ExtraAttrs, cfg.ID, cfg.Class)
	return render.Tag("code", attrs, children...)
}

// Pre produces a <pre> element for preformatted text.
func Pre(cfg TextConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.ExtraAttrs, cfg.ID, cfg.Class)
	return render.Tag("pre", attrs, children...)
}

// Kbd produces a <kbd> element for keyboard input — a single key, a chord,
// or a sequence. Pairs with framework/ui.ShortcutHint for rendering
// shortcut chips. Pure semantic markup, no scripting.
func Kbd(cfg TextConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.ExtraAttrs, cfg.ID, cfg.Class)
	return render.Tag("kbd", attrs, children...)
}

// Blockquote produces a <blockquote> element.
func Blockquote(cfg TextConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.ExtraAttrs, cfg.ID, cfg.Class)
	return render.Tag("blockquote", attrs, children...)
}

// Cite produces a <cite> element for the title of a work.
func Cite(cfg TextConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.ExtraAttrs, cfg.ID, cfg.Class)
	return render.Tag("cite", attrs, children...)
}

// Small produces a <small> element for side comments.
func Small(cfg TextConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.ExtraAttrs, cfg.ID, cfg.Class)
	return render.Tag("small", attrs, children...)
}

// Mark produces a <mark> element for highlighted text.
func Mark(cfg TextConfig, children ...render.HTML) render.HTML {
	attrs := buildAttrs(cfg.ExtraAttrs, cfg.ID, cfg.Class)
	return render.Tag("mark", attrs, children...)
}

// Abbr produces an <abbr> element with a title attribute for the full
// expansion of the abbreviation.
// Required: Title.
func Abbr(cfg AbbrConfig, children ...render.HTML) render.HTML {
	if cfg.Title == "" {
		panic("html: Abbr requires Title")
	}
	attrs := buildAttrs(cfg.ExtraAttrs, cfg.ID, cfg.Class)
	setAttr(attrs, "title", cfg.Title)
	return render.Tag("abbr", attrs, children...)
}

// Time produces a <time> element with a machine-readable datetime attribute.
// Required: Datetime.
func Time(cfg TimeConfig, children ...render.HTML) render.HTML {
	if cfg.Datetime == "" {
		panic("html: Time requires Datetime")
	}
	attrs := buildAttrs(cfg.ExtraAttrs, cfg.ID, cfg.Class)
	setAttr(attrs, "datetime", cfg.Datetime)
	return render.Tag("time", attrs, children...)
}
