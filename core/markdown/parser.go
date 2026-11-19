package markdown

import (
	"fmt"
	"strconv"
	"strings"
)

type parser struct {
	lines       []string
	pos         int
	depth       int
	headingIDs  map[string]int
	headingUsed map[string]bool
	closeLine   int
	closeClean  bool
	closeKnown  bool
}

func (p *parser) eof() bool    { return p.pos >= len(p.lines) }
func (p *parser) line() string { return p.lines[p.pos] }
func (p *parser) advance()     { p.pos++ }
func (p *parser) peek(n int) string {
	if p.pos+n >= len(p.lines) {
		return ""
	}
	return p.lines[p.pos+n]
}

func (p *parser) atFence() bool {
	_, ok := openFence(p.line())
	return ok
}

func (p *parser) atComment() bool {
	return p.commentEnd() >= 0
}

func (p *parser) commentEnd() int {
	t := p.line()
	if indent := len(t) - len(strings.TrimLeft(t, " ")); indent > 3 {
		return -1
	}
	if !strings.HasPrefix(strings.TrimLeft(t, " "), "<!--") {
		return -1
	}
	rest := strings.TrimLeft(t, " ")[len("<!--"):]
	if strings.Contains(rest, "-->") {
		if closesAtEnd(rest) {
			return p.pos
		}
		return -1
	}
	if !p.closeKnown || p.closeLine <= p.pos {
		p.closeLine, p.closeClean, p.closeKnown = len(p.lines), false, true
		for i := p.pos + 1; i < len(p.lines); i++ {
			if strings.Contains(p.lines[i], "-->") {
				p.closeLine, p.closeClean = i, closesAtEnd(p.lines[i])
				break
			}
		}
	}
	if p.closeLine < len(p.lines) && p.closeClean {
		return p.closeLine
	}
	return -1
}

func closesAtEnd(line string) bool {
	t := strings.TrimRight(line, " \t")
	return strings.Index(t, "-->") == len(t)-len("-->")
}

func (p *parser) skipComment() {
	p.pos = p.commentEnd() + 1
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
	_, _, ok := splitListItem(p.line(), true)
	return ok
}

const maxOrdinalDigits = 9

func ordinalDigits(t string) int {
	i := 0
	for i < len(t) && t[i] >= '0' && t[i] <= '9' {
		i++
	}
	return i
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

func parseHeading(line string) (level int, text string) {
	for level < len(line) && line[level] == '#' {
		level++
	}
	text = strings.TrimSpace(line[level:])
	if t := strings.TrimRight(text, "#"); t != text && (t == "" || strings.HasSuffix(t, " ")) {
		text = strings.TrimSpace(t)
	}
	return level, text
}

func (p *parser) headingID(text string) string {
	base := slugify(text)
	n := p.headingIDs[base]
	for {
		candidate := base
		if n > 0 {
			candidate = fmt.Sprintf("%s-%d", base, n+1)
		}
		n++
		if p.headingUsed[candidate] {
			continue
		}
		p.headingIDs[base] = n
		p.headingUsed[candidate] = true
		return candidate
	}
}

func headingHTML(level int, text, id string) string {
	tag := fmt.Sprintf("h%d", level)
	return fmt.Sprintf("<%s id=%q>%s</%s>\n", tag, id, renderInline(text), tag)
}

func renderFence(p *parser, sb *strings.Builder) {
	d, ok := openFence(p.line())
	if !ok {
		p.advance()
		return
	}
	info := ParseFenceInfo(d.info)
	p.advance()

	var body strings.Builder
	for !p.eof() {
		line := p.line()
		if d.closes(line) {
			p.advance()
			break
		}
		body.WriteString(line)
		body.WriteByte('\n')
		p.advance()
	}

	sb.WriteString(`<pre tabindex="0"><code`)
	if info.Lang != "" {
		fmt.Fprintf(sb, " class=\"%s\"", escapeAttr("language-"+info.Lang))
	}
	if info.Meta != "" {
		fmt.Fprintf(sb, " data-meta=\"%s\"", escapeAttr(info.Meta))
	}
	sb.WriteString(">")
	sb.WriteString(escapeHTML(body.String()))
	sb.WriteString("</code></pre>\n")
}

func (p *parser) atBlockStart() bool {
	return p.atFence() || p.atComment() || p.atHR() || p.atHeading() || p.atBlockquote() ||
		p.atUnorderedList() || p.atOrderedList() || p.atTable()
}

func renderParagraph(p *parser, sb *strings.Builder) {
	var lines []string
	for !p.eof() {
		raw := p.line()
		if strings.TrimSpace(raw) == "" {
			break
		}
		if p.atBlockStart() {
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
		sb.WriteString("<p>")
		sb.WriteString(renderInline(joined))
		sb.WriteString("</p>\n")
	} else {
		sub, _ := renderBodyDepth(joined, p.depth+1, p.headingIDs, p.headingUsed)
		sb.WriteString(string(sub))
	}
	sb.WriteString("</blockquote>\n")
}

func renderList(p *parser, sb *strings.Builder, ordered bool) {
	tag := "ul"
	if ordered {
		tag = "ol"
	}
	base := leadingSpaces(p.line())
	if n, ok := listStart(p.line()); ordered && ok && n != 1 {
		fmt.Fprintf(sb, "<ol start=\"%d\">\n", n)
	} else {
		sb.WriteString("<" + tag + ">\n")
	}
	for !p.eof() {
		line := p.line()
		_, content, ok := splitListItem(line, ordered)
		if !ok {
			break
		}
		width := len(line) - len(content)
		item := []string{content}
		p.advance()
		for !p.eof() {
			next := p.line()
			if strings.TrimSpace(next) == "" {
				j := p.pos
				for j < len(p.lines) && strings.TrimSpace(p.lines[j]) == "" {
					j++
				}
				if j == len(p.lines) || leadingSpaces(p.lines[j]) < base+2 {
					break
				}
				for ; p.pos < j; p.advance() {
					item = append(item, "")
				}
				continue
			}
			if leadingSpaces(next) < base+2 {
				break
			}
			if end := p.commentEnd(); end >= 0 {
				p.pos = end + 1
				continue
			}
			item = append(item, stripIndent(next, width))
			p.advance()
		}
		sb.WriteString("  <li>")
		writeListItem(p, sb, item)
		sb.WriteString("</li>\n")
		j := p.pos
		for j < len(p.lines) && strings.TrimSpace(p.lines[j]) == "" {
			j++
		}
		if j > p.pos && j < len(p.lines) && leadingSpaces(p.lines[j]) < base+2 {
			if _, _, sibling := splitListItem(p.lines[j], ordered); sibling {
				p.pos = j
			}
		}
	}
	sb.WriteString("</" + tag + ">\n")
}

func writeListItem(p *parser, sb *strings.Builder, item []string) {
	ip := &parser{lines: item, pos: 1}
	for !ip.eof() && strings.TrimSpace(ip.line()) != "" && !ip.atBlockStart() {
		ip.advance()
	}
	if ip.eof() || p.depth+1 >= maxBlockquoteDepth {
		sb.WriteString(renderInline(strings.Join(item, "\n")))
		return
	}
	sb.WriteString(renderInline(strings.Join(item[:ip.pos], "\n")))
	sb.WriteByte('\n')
	sub, _ := renderBodyDepth(strings.Join(item[ip.pos:], "\n"), p.depth+1, p.headingIDs, p.headingUsed)
	sb.WriteString(string(sub))
}

func leadingSpaces(line string) int {
	return len(line) - len(strings.TrimLeft(line, " "))
}

func stripIndent(line string, n int) string {
	if lead := leadingSpaces(line); lead < n {
		n = lead
	}
	return line[n:]
}

func listStart(line string) (int, bool) {
	t := strings.TrimLeft(line, " ")
	i := ordinalDigits(t)
	if i == 0 || i > maxOrdinalDigits {
		return 0, false
	}
	n, err := strconv.Atoi(t[:i])
	return n, err == nil
}

func splitListItem(line string, ordered bool) (marker, content string, ok bool) {
	t := strings.TrimLeft(line, " ")
	if ordered {
		i := ordinalDigits(t)
		if i == 0 || i > maxOrdinalDigits || i+1 >= len(t) || t[i] != '.' || t[i+1] != ' ' {
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
	p.advance()
	sepCells := splitTableRow(p.line())
	aligns := make([]string, len(header))
	for i, c := range sepCells {
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
	p.advance()

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
	if strings.HasSuffix(t, "|") && !strings.HasSuffix(t, `\|`) {
		t = strings.TrimSuffix(t, "|")
	}
	var parts []string
	var cell strings.Builder
	for i := 0; i < len(t); i++ {
		switch {
		case t[i] == '\\' && i+1 < len(t) && t[i+1] == '|':
			cell.WriteByte('|')
			i++
		case t[i] == '|':
			parts = append(parts, strings.TrimSpace(cell.String()))
			cell.Reset()
		default:
			cell.WriteByte(t[i])
		}
	}
	return append(parts, strings.TrimSpace(cell.String()))
}

func writeCell(sb *strings.Builder, tag, text, align string) {
	if align != "" {
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
