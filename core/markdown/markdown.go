package markdown

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core/render"
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

// maxBlockquoteDepth bounds blockquote nesting. Each level re-parses the
// remaining inner text, so unbounded nesting is O(n^2) on a single line of
// "> > > … x" — a CPU-DoS amplifier on any user-submitted markdown. Past
// the cap we stop recursing and emit the remaining text verbatim (escaped).
const maxBlockquoteDepth = 32

// renderBody runs the block parser and returns the HTML plus the first H1
// text seen (used as the document title).
func renderBody(input string) (render.HTML, string) {
	return renderBodyDepth(input, 0)
}

func renderBodyDepth(input string, depth int) (render.HTML, string) {
	p := &parser{lines: splitLines(input), depth: depth}
	var sb strings.Builder
	var firstH1 string
	for !p.eof() {
		// Progress guard: every block handler must consume at least one
		// line. A classifier that matches a line the handler then refuses
		// to consume (e.g. an atX() / renderX() whitespace-definition
		// mismatch) spins forever. Record the position and force-advance
		// past an unconsumable line so no handler bug can hang the render.
		start := p.pos
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
		if p.pos == start {
			p.advance()
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
