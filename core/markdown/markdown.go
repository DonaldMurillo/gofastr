package markdown

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// Document is the result of parsing a Markdown source.
type Document struct {
	// Frontmatter holds the leading block's key/value pairs as strings.
	Frontmatter map[string]string

	// HTML is the rendered body.
	HTML render.HTML

	// Title is the first H1's text, else the title frontmatter key.
	Title string
}

// Render parses Markdown source and returns the rendered Document.
func Render(input string) Document {
	fm, body := splitFrontmatter(strings.TrimPrefix(input, "\ufeff"))
	html, title := renderBody(body)
	if title == "" {
		title = fm["title"]
	}
	return Document{Frontmatter: fm, HTML: html, Title: title}
}

// RenderHTML returns Render(input).HTML.
func RenderHTML(input string) render.HTML {
	return Render(input).HTML
}

const maxBlockquoteDepth = 32

func renderBody(input string) (render.HTML, string) {
	return renderBodyDepth(input, 0, make(map[string]int), make(map[string]bool))
}

func renderBodyDepth(input string, depth int, headingIDs map[string]int, headingUsed map[string]bool) (render.HTML, string) {
	p := &parser{
		lines:       splitLines(input),
		depth:       depth,
		headingIDs:  headingIDs,
		headingUsed: headingUsed,
	}
	var sb strings.Builder
	var firstH1 string
	for !p.eof() {
		start := p.pos
		switch {
		case p.atFence():
			renderFence(p, &sb)
		case p.atComment():
			p.skipComment()
		case p.atHR():
			sb.WriteString("<hr>\n")
			p.advance()
		case p.atHeading():
			level, text := parseHeading(p.line())
			if level == 1 && firstH1 == "" {
				firstH1 = text
			}
			sb.WriteString(headingHTML(level, text, p.headingID(text)))
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

func splitLines(input string) []string {
	input = strings.ReplaceAll(input, "\r\n", "\n")
	input = strings.ReplaceAll(input, "\r", "\n")
	return strings.Split(input, "\n")
}
