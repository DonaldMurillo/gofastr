package elements

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/gofastr/gofastr/core/render"
)

// nonAlphaNum matches runs of non-alphanumeric characters for slug generation.
var nonAlphaNum = regexp.MustCompile(`[^a-zA-Z0-9]+`)

// slugify converts a string to a URL-friendly slug for use as an HTML id.
func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = nonAlphaNum.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 64 {
		s = s[:64]
	}
	return s
}

// Heading produces an <h1> through <h6> element. It auto-generates an id
// attribute from the text content of children for aria-labelledby references.
// If attrs already contains an "id", the explicit id is kept.
func Heading(level int, attrs Attrs, children ...render.HTML) render.HTML {
	if level < 1 {
		level = 1
	}
	if level > 6 {
		level = 6
	}
	tag := fmt.Sprintf("h%d", level)

	// Auto-generate id from children if not already set.
	if attrs == nil || attrs["id"] == "" {
		var text strings.Builder
		for _, c := range children {
			text.WriteString(string(c))
		}
		id := slugify(text.String())
		if id != "" {
			attrs = setAttr(attrs, "id", "heading-"+id)
		}
	}

	return render.Tag(tag, attrs, children...)
}

// Paragraph produces a <p> element.
func Paragraph(attrs Attrs, children ...render.HTML) render.HTML {
	return render.Tag("p", attrs, children...)
}

// Span produces a <span> element.
func Span(attrs Attrs, children ...render.HTML) render.HTML {
	return render.Tag("span", attrs, children...)
}

// Strong produces a <strong> element for strong importance.
func Strong(attrs Attrs, children ...render.HTML) render.HTML {
	return render.Tag("strong", attrs, children...)
}

// Em produces an <em> element for stress emphasis.
func Em(attrs Attrs, children ...render.HTML) render.HTML {
	return render.Tag("em", attrs, children...)
}

// Code produces a <code> element for inline code fragments.
func Code(attrs Attrs, children ...render.HTML) render.HTML {
	return render.Tag("code", attrs, children...)
}

// Pre produces a <pre> element for preformatted text.
func Pre(attrs Attrs, children ...render.HTML) render.HTML {
	return render.Tag("pre", attrs, children...)
}

// Blockquote produces a <blockquote> element.
func Blockquote(attrs Attrs, children ...render.HTML) render.HTML {
	return render.Tag("blockquote", attrs, children...)
}

// Cite produces a <cite> element for the title of a work.
func Cite(attrs Attrs, children ...render.HTML) render.HTML {
	return render.Tag("cite", attrs, children...)
}

// Small produces a <small> element for side comments.
func Small(attrs Attrs, children ...render.HTML) render.HTML {
	return render.Tag("small", attrs, children...)
}

// Mark produces a <mark> element for highlighted text.
func Mark(attrs Attrs, children ...render.HTML) render.HTML {
	return render.Tag("mark", attrs, children...)
}

// Abbr produces an <abbr> element with a title attribute for the full
// expansion of the abbreviation.
func Abbr(title string, attrs Attrs, children ...render.HTML) render.HTML {
	attrs = setAttr(attrs, "title", title)
	return render.Tag("abbr", attrs, children...)
}

// Time produces a <time> element with a machine-readable datetime attribute.
func Time(datetime string, attrs Attrs, children ...render.HTML) render.HTML {
	attrs = setAttr(attrs, "datetime", datetime)
	return render.Tag("time", attrs, children...)
}
