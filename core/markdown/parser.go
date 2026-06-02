package markdown

import (
	"fmt"
	"strings"
)

// parser is a tiny line-cursor used by the block-level renderers.
type parser struct {
	lines []string
	pos   int
	// depth is the current blockquote nesting level. It bounds the
	// O(n)-per-level re-parse in renderBlockquote so deeply nested
	// blockquotes can't drive O(n^2) CPU work.
	depth int
}

func (p *parser) eof() bool      { return p.pos >= len(p.lines) }
func (p *parser) line() string   { return p.lines[p.pos] }
func (p *parser) advance()       { p.pos++ }
func (p *parser) peek(n int) string {
	if p.pos+n >= len(p.lines) {
		return ""
	}
	return p.lines[p.pos+n]
}

// ---------------------------------------------------------------------------
// Block detection
// ---------------------------------------------------------------------------

func (p *parser) atFence() bool {
	t := strings.TrimLeft(p.line(), " ")
	return strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~")
}

func (p *parser) atHR() bool {
	t := strings.TrimSpace(p.line())
	if len(t) < 3 {
		return false
	}
	for _, ch := range "-*_" {
		if isAllChar(t, ch) {
			return true
		}
	}
	return false
}

func (p *parser) atHeading() bool {
	t := p.line()
	if !strings.HasPrefix(t, "#") {
		return false
	}
	i := 0
	for i < len(t) && t[i] == '#' {
		i++
	}
	return i >= 1 && i <= 6 && i < len(t) && t[i] == ' '
}

func (p *parser) atBlockquote() bool {
	// Use space-only trimming to match renderBlockquote's line consumer.
	// TrimSpace here would classify e.g. "\f>" as a blockquote while the
	// consumer (HasPrefix on a space-trimmed line) refuses to strip it,
	// leaving the parser unable to advance — an infinite-loop DoS.
	trimmed := strings.TrimLeft(p.line(), " ")
	return strings.HasPrefix(trimmed, "> ") || trimmed == ">"
}

func (p *parser) atUnorderedList() bool {
	t := strings.TrimLeft(p.line(), " ")
	if len(t) < 2 {
		return false
	}
	return (t[0] == '-' || t[0] == '*' || t[0] == '+') && t[1] == ' '
}

func (p *parser) atOrderedList() bool {
	t := strings.TrimLeft(p.line(), " ")
	i := 0
	for i < len(t) && t[i] >= '0' && t[i] <= '9' {
		i++
	}
	if i == 0 || i+1 >= len(t) {
		return false
	}
	return t[i] == '.' && t[i+1] == ' '
}

func (p *parser) atTable() bool {
	if !strings.Contains(p.line(), "|") {
		return false
	}
	sep := p.peek(1)
	if !strings.Contains(sep, "|") {
		return false
	}
	for _, ch := range strings.TrimSpace(sep) {
		if ch != '-' && ch != '|' && ch != ':' && ch != ' ' {
			return false
		}
	}
	return true
}

func isAllChar(s string, ch rune) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c != ch && c != ' ' {
			return false
		}
	}
	return strings.Count(s, string(ch)) >= 3
}

// ---------------------------------------------------------------------------
// Block renderers
// ---------------------------------------------------------------------------

func parseHeading(line string) (level int, text string) {
	for level < len(line) && line[level] == '#' {
		level++
	}
	return level, strings.TrimSpace(strings.TrimLeft(line, "# "))
}

func headingHTML(level int, text string) string {
	tag := fmt.Sprintf("h%d", level)
	id := slugify(text)
	return fmt.Sprintf("<%s id=%q>%s</%s>\n", tag, id, renderInline(text), tag)
}

func renderFence(p *parser, sb *strings.Builder) {
	open := strings.TrimLeft(p.line(), " ")
	fence := "```"
	if strings.HasPrefix(open, "~~~") {
		fence = "~~~"
	}
	lang := strings.TrimSpace(strings.TrimPrefix(open, fence))
	p.advance()

	var body strings.Builder
	for !p.eof() {
		line := p.line()
		if strings.HasPrefix(strings.TrimSpace(line), fence) {
			p.advance()
			break
		}
		body.WriteString(line)
		body.WriteByte('\n')
		p.advance()
	}

	sb.WriteString(`<pre tabindex="0"><code`)
	if lang != "" {
		// %q is NOT HTML-attribute-safe: it escapes " as \" (a literal
		// backslash + quote in HTML, so the quote terminates the value)
		// and leaves > untouched, letting an attacker-controlled info
		// string break out into element context. HTML-escape instead.
		fmt.Fprintf(sb, " class=\"%s\"", escapeAttr("language-"+lang))
	}
	sb.WriteString(">")
	sb.WriteString(escapeHTML(body.String()))
	sb.WriteString("</code></pre>\n")
}

func renderParagraph(p *parser, sb *strings.Builder) {
	var lines []string
	for !p.eof() {
		raw := p.line()
		if strings.TrimSpace(raw) == "" {
			break
		}
		if p.atFence() || p.atHR() || p.atHeading() || p.atBlockquote() ||
			p.atUnorderedList() || p.atOrderedList() || p.atTable() {
			break
		}
		lines = append(lines, raw)
		p.advance()
	}
	if len(lines) == 0 {
		return
	}
	sb.WriteString("<p>")
	sb.WriteString(renderInline(strings.Join(lines, "\n")))
	sb.WriteString("</p>\n")
}

func renderBlockquote(p *parser, sb *strings.Builder) {
	var inner []string
	for !p.eof() {
		line := strings.TrimLeft(p.line(), " ")
		if !strings.HasPrefix(line, ">") {
			break
		}
		stripped := strings.TrimPrefix(line, ">")
		stripped = strings.TrimPrefix(stripped, " ")
		inner = append(inner, stripped)
		p.advance()
	}
	sb.WriteString("<blockquote>\n")
	joined := strings.Join(inner, "\n")
	if p.depth+1 >= maxBlockquoteDepth {
		// Depth cap reached: stop recursing into renderBody (which would
		// re-scan the whole remaining string for every further level) and
		// emit the inner text as an escaped paragraph. Fails closed against
		// the nested-blockquote CPU-DoS without dropping content.
		sb.WriteString("<p>")
		sb.WriteString(renderInline(joined))
		sb.WriteString("</p>\n")
	} else {
		sub, _ := renderBodyDepth(joined, p.depth+1)
		sb.WriteString(string(sub))
	}
	sb.WriteString("</blockquote>\n")
}

func renderList(p *parser, sb *strings.Builder, ordered bool) {
	tag := "ul"
	if ordered {
		tag = "ol"
	}
	sb.WriteString("<" + tag + ">\n")
	for !p.eof() {
		marker, content, ok := splitListItem(p.line(), ordered)
		if !ok {
			break
		}
		_ = marker
		// Continuation lines: indented text under the same item.
		var item []string
		item = append(item, content)
		p.advance()
		for !p.eof() {
			next := p.line()
			if strings.HasPrefix(next, "  ") && strings.TrimSpace(next) != "" {
				item = append(item, strings.TrimPrefix(next, "  "))
				p.advance()
				continue
			}
			break
		}
		sb.WriteString("  <li>")
		sb.WriteString(renderInline(strings.Join(item, "\n")))
		sb.WriteString("</li>\n")
	}
	sb.WriteString("</" + tag + ">\n")
}

func splitListItem(line string, ordered bool) (marker, content string, ok bool) {
	t := strings.TrimLeft(line, " ")
	if ordered {
		i := 0
		for i < len(t) && t[i] >= '0' && t[i] <= '9' {
			i++
		}
		if i == 0 || i+1 >= len(t) || t[i] != '.' || t[i+1] != ' ' {
			return "", "", false
		}
		return t[:i+1], t[i+2:], true
	}
	if len(t) < 2 || (t[0] != '-' && t[0] != '*' && t[0] != '+') || t[1] != ' ' {
		return "", "", false
	}
	return string(t[0]), t[2:], true
}

func renderTable(p *parser, sb *strings.Builder) {
	header := splitTableRow(p.line())
	p.advance() // header
	sepCells := splitTableRow(p.line())
	aligns := make([]string, len(header))
	for i, c := range sepCells {
		// A malformed table can have more separator cells than header
		// cells ("|\n||:"); aligns is sized to the header, so ignore the
		// surplus rather than indexing out of range (panic → request DoS).
		if i >= len(aligns) {
			break
		}
		c = strings.TrimSpace(c)
		switch {
		case strings.HasPrefix(c, ":") && strings.HasSuffix(c, ":"):
			aligns[i] = "center"
		case strings.HasSuffix(c, ":"):
			aligns[i] = "right"
		case strings.HasPrefix(c, ":"):
			aligns[i] = "left"
		}
	}
	p.advance() // separator

	sb.WriteString("<table>\n<thead>\n<tr>")
	for i, c := range header {
		writeCell(sb, "th", c, alignAt(aligns, i))
	}
	sb.WriteString("</tr>\n</thead>\n<tbody>\n")
	for !p.eof() && strings.Contains(p.line(), "|") && strings.TrimSpace(p.line()) != "" {
		row := splitTableRow(p.line())
		sb.WriteString("<tr>")
		for i, c := range row {
			writeCell(sb, "td", c, alignAt(aligns, i))
		}
		sb.WriteString("</tr>\n")
		p.advance()
	}
	sb.WriteString("</tbody>\n</table>\n")
}

func splitTableRow(line string) []string {
	t := strings.TrimSpace(line)
	t = strings.TrimPrefix(t, "|")
	t = strings.TrimSuffix(t, "|")
	parts := strings.Split(t, "|")
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
	}
	return parts
}

func writeCell(sb *strings.Builder, tag, text, align string) {
	if align != "" {
		// Emit `class="md-align-<dir>"` instead of inline style so the
		// markdown output stays compatible with strict-CSP hosts (the
		// framework default). Host stylesheets / a future markdown
		// preset map .md-align-{left,right,center} → text-align.
		fmt.Fprintf(sb, "<%s class=\"md-align-%s\">%s</%s>", tag, align, renderInline(text), tag)
		return
	}
	fmt.Fprintf(sb, "<%s>%s</%s>", tag, renderInline(text), tag)
}

func alignAt(aligns []string, i int) string {
	if i >= len(aligns) {
		return ""
	}
	return aligns[i]
}
