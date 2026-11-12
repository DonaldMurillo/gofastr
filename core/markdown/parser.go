package markdown

import (
	"fmt"
	"strconv"
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
	// headingIDs tracks generated heading IDs for the current document. It is
	// shared with recursive blockquote parsers so IDs stay unique document-wide.
	headingIDs map[string]int
	// headingUsed records every emitted ID, including IDs that are natural
	// slugs for a different heading (e.g. "setup-2").
	headingUsed map[string]bool
	// closeLine caches the index of the first line below some earlier
	// comment opener that holds "-->" (len(lines) when none does),
	// closeClean whether that line ends at its "-->", and closeKnown that
	// the cache is filled. See commentEnd.
	closeLine  int
	closeClean bool
	closeKnown bool
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

// ---------------------------------------------------------------------------
// Block detection
// ---------------------------------------------------------------------------

func (p *parser) atFence() bool {
	_, ok := openFence(p.line())
	return ok
}

// atComment reports whether an HTML comment block starts here: a line
// opening with "<!--" (up to three spaces of indent) and a later or same
// line ending in "-->". The block renders as nothing, the way a browser
// hides a comment; this package never passes raw HTML through, so the
// alternative is the escaped comment showing up as page text. Docs carry
// tooling directives this way (<!-- gofastr:compile … --> above a fence).
// An unclosed opener, or text after the closing "-->", stays an escaped
// paragraph so no content is swallowed.
func (p *parser) atComment() bool {
	return p.commentEnd() >= 0
}

// commentEnd returns the index of the line that closes the comment block
// starting at p.pos, or -1. The block ends at the first line below the
// opener holding "-->", and that line is cached: no line between an
// opener and it holds "-->", so every later opener above it ends at the
// same line, and the next scan starts below it. Each line is scanned
// once, so a document of openers stays linear whether its close is
// missing or carries trailing text.
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

// closesAtEnd reports whether the first "-->" on line is its last text,
// so nothing after the comment's close is dropped with it.
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
	// Use space-only trimming to match renderBlockquote's line consumer.
	// TrimSpace here would classify e.g. "\f>" as a blockquote while the
	// consumer (HasPrefix on a space-trimmed line) refuses to strip it,
	// leaving the parser unable to advance, an infinite-loop DoS.
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

// maxOrdinalDigits is CommonMark's cap on an ordered list marker's
// number: a longer digit run is paragraph text, not a list item.
// atOrderedList took any run while listStart capped at nine, so a
// ten-digit ordinal opened a list that renumbered from 1.
const maxOrdinalDigits = 9

// ordinalDigits returns the length of the digit run t opens with.
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

// ---------------------------------------------------------------------------
// Block renderers
// ---------------------------------------------------------------------------

func parseHeading(line string) (level int, text string) {
	for level < len(line) && line[level] == '#' {
		level++
	}
	// Only the opening run is syntax. Trimming every leading '#' and
	// space ate the start of the text too: "## #1 priority" read as
	// "1 priority".
	text = strings.TrimSpace(line[level:])
	// A closing run of '#' set off by a space is syntax as well
	// ("## Setup ##"); "# C#" keeps its '#'.
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
		// atFence gates this call; advance anyway so a classifier/handler
		// disagreement can never spin the block loop.
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
		// %q is NOT HTML-attribute-safe: it escapes " as \" (a literal
		// backslash + quote in HTML, so the quote terminates the value)
		// and leaves > untouched, letting an attacker-controlled info
		// string break out into element context. HTML-escape instead.
		fmt.Fprintf(sb, " class=\"%s\"", escapeAttr("language-"+info.Lang))
	}
	if info.Meta != "" {
		// Fence options ride in their own attribute. Folding them into the
		// class is what used to emit class="language-go title=&quot;x&quot;",
		// a class no highlighter matches.
		fmt.Fprintf(sb, " data-meta=\"%s\"", escapeAttr(info.Meta))
	}
	sb.WriteString(">")
	sb.WriteString(escapeHTML(body.String()))
	sb.WriteString("</code></pre>\n")
}

// atBlockStart reports whether the current line opens a block other than
// a paragraph, which ends the paragraph (or list item text) above it.
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
		// Depth cap reached: stop recursing into renderBody (which would
		// re-scan the whole remaining string for every further level) and
		// emit the inner text as an escaped paragraph. Fails closed against
		// the nested-blockquote CPU-DoS without dropping content.
		sb.WriteString("<p>")
		sb.WriteString(renderInline(joined))
		sb.WriteString("</p>\n")
	} else {
		sub, _ := renderBodyDepth(joined, p.depth+1, p.headingIDs, p.headingUsed)
		sb.WriteString(string(sub))
	}
	sb.WriteString("</blockquote>\n")
}

// renderList renders one list. An item owns the lines indented two or
// more spaces past its marker, blank lines between them included, so it
// can hold a nested list, a fenced code block or a second paragraph. Read
// as inline text, which is what every continuation line used to be, a
// nested "- item" showed its dash and a fence under a numbered step ran
// together as one inline code span. Items separated by a blank line stay
// one list, and an ordered list that does not begin at 1 says so in
// start, so "2." after a code block does not count again from 1.
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
		// width is the column the item's text starts at; continuation
		// lines lose up to that much indentation.
		width := len(line) - len(content)
		item := []string{content}
		p.advance()
		for !p.eof() {
			next := p.line()
			if strings.TrimSpace(next) == "" {
				// A blank run stays in the item only when indented
				// text follows it. The run is measured once, so a
				// document of blank lines stays linear.
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
			// A comment block under the item hides as it would at the
			// top level; the item's text continues after it.
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
		// A blank run between two items of this list does not end it.
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

// writeListItem renders an item's lines. Text up to the first blank line
// or block opener is inline, as a one-paragraph item always was; whatever
// follows goes through the block parser. Past the nesting cap the whole
// item is inline text, the same fail-closed stop a blockquote has.
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

// stripIndent removes up to n leading spaces.
func stripIndent(line string, n int) string {
	if lead := leadingSpaces(line); lead < n {
		n = lead
	}
	return line[n:]
}

// listStart returns the number an ordered list item opens with. A number
// too long to be a real step count reports false.
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

// splitTableRow cuts a row at its unescaped pipes. "\|" is a literal pipe
// in the cell (GFM), inside a code span too: split on every pipe, a cell
// documenting `text\|number` became two cells and pushed the rest of its
// row one column right.
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
