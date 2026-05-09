package markdown

import (
	"strings"

	"github.com/gofastr/gofastr/core/render"
)

// Document is the result of parsing a Markdown source.
type Document struct {
	// Frontmatter holds key/value pairs from a leading `--- ... ---` block.
	// Values are kept as raw strings; callers do their own typing.
	Frontmatter map[string]string

	// HTML is the rendered body.
	HTML render.HTML

	// Title is the text of the first H1 in the document if present, otherwise
	// the value of the `title` frontmatter key. Useful for setting <title>.
	Title string
}

// Render parses Markdown source and returns the rendered Document.
func Render(input string) Document {
	fm, body := splitFrontmatter(input)
	html, title := renderBody(body)
	if title == "" {
		title = fm["title"]
	}
	return Document{Frontmatter: fm, HTML: html, Title: title}
}

// RenderHTML is a convenience wrapper that returns just the HTML body.
func RenderHTML(input string) render.HTML {
	return Render(input).HTML
}

// renderBody runs the block parser and returns the HTML plus the first H1
// text seen (used as the document title).
func renderBody(input string) (render.HTML, string) {
	p := &parser{lines: splitLines(input)}
	var sb strings.Builder
	var firstH1 string
	for !p.eof() {
		switch {
		case p.atFence():
			renderFence(p, &sb)
		case p.atHR():
			sb.WriteString("<hr>\n")
			p.advance()
		case p.atHeading():
			level, text := parseHeading(p.line())
			if level == 1 && firstH1 == "" {
				firstH1 = text
			}
			sb.WriteString(headingHTML(level, text))
			p.advance()
		case p.atTable():
			renderTable(p, &sb)
		case p.atBlockquote():
			renderBlockquote(p, &sb)
		case p.atUnorderedList():
			renderList(p, &sb, false)
		case p.atOrderedList():
			renderList(p, &sb, true)
		case strings.TrimSpace(p.line()) == "":
			p.advance()
		default:
			renderParagraph(p, &sb)
		}
	}
	return render.HTML(sb.String()), firstH1
}

// splitLines splits input into lines without keeping the trailing line break.
// We normalise CRLF to LF first so logic only deals with one form.
func splitLines(input string) []string {
	input = strings.ReplaceAll(input, "\r\n", "\n")
	input = strings.ReplaceAll(input, "\r", "\n")
	return strings.Split(input, "\n")
}
