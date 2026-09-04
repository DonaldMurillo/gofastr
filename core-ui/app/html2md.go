// check-csp:ignore-file
// This file builds regex patterns that match (and strip) <script> and
// <style> blocks from rendered HTML before converting it to Markdown
// for /llm.md. The patterns never emit script tags, they only consume
// them, but the literal `<script` substring trips the no-inline-script
// linter. The directive exempts this file from that check.
package app

import (
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
)

// Pre-compiled regexes, compiled once, reused per request.
var (
	reScript    = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	reStyle     = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	reCode      = regexp.MustCompile(`(?is)<code[^>]*>(.*?)</code>`)
	reStrong    = regexp.MustCompile(`(?is)<strong[^>]*>(.*?)</strong>`)
	reB         = regexp.MustCompile(`(?is)<b(?:\s[^>]*)?>(.*?)</b>`)
	reEm        = regexp.MustCompile(`(?is)<em[^>]*>(.*?)</em>`)
	reI         = regexp.MustCompile(`(?is)<i(?:\s[^>]*)?>(.*?)</i>`)
	reLink      = regexp.MustCompile(`(?is)<a[^>]*href="([^"]*)"[^>]*>(.*?)</a>`)
	reImgAltSrc = regexp.MustCompile(`(?is)<img[^>]*alt="([^"]*)"[^>]*src="([^"]*)"[^>]*/?\s*>`)
	reImgSrcAlt = regexp.MustCompile(`(?is)<img[^>]*src="([^"]*)"[^>]*alt="([^"]*)"[^>]*/?\s*>`)
	reHR        = regexp.MustCompile(`(?is)<hr[^>]*/?\s*>`)
	reLiOpen    = regexp.MustCompile(`(?is)<li[^>]*>`)
	reLiClose   = regexp.MustCompile(`(?is)</li>`)
	reOlBlock   = regexp.MustCompile(`(?is)(<ol[^>]*>)(.*?)(</ol>)`)
	reOlTag     = regexp.MustCompile(`(?is)</?ol[^>]*>`)
	reUlTag     = regexp.MustCompile(`(?is)</?ul[^>]*>`)
	rePOpen     = regexp.MustCompile(`(?is)<p[^>]*>`)
	rePClose    = regexp.MustCompile(`(?is)</p>`)
	reDivOpen   = regexp.MustCompile(`(?is)<div[^>]*>`)
	reDivClose  = regexp.MustCompile(`(?is)</div>`)
	reBR        = regexp.MustCompile(`(?is)<br[^>]*/?\s*>`)
	reAnyTag    = regexp.MustCompile(`(?is)<[^>]+>`)
	reStripTag  = regexp.MustCompile(`<[^>]+>`)

	// Table regexes
	reTable   = regexp.MustCompile(`(?is)<table[^>]*>(.*?)</table>`)
	reRow     = regexp.MustCompile(`(?is)<tr[^>]*>(.*?)</tr>`)
	reCell    = regexp.MustCompile(`(?is)<t[hd][^>]*>(.*?)</t[hd]>`)
	reOlInner = regexp.MustCompile(`(?is)<li[^>]*>`)

	// rePreBlock matches one <pre><code>…</code></pre> block whole, so
	// its content can be fenced (and entity-decoded exactly once)
	// before any inline pass runs over it.
	rePreBlock = regexp.MustCompile(`(?is)<pre[^>]*>\s*<code[^>]*>(.*?)</code>\s*</pre>`)
	// reFenceSlot matches the \x00-parked fenced-block placeholders
	// extractFencedBlocks leaves in the document.
	reFenceSlot = regexp.MustCompile("\x00f([0-9]+)\x00")
	// reTagStart matches a '<' that begins something tag-shaped, so a
	// decoded entity (&lt;script&gt;) can be re-neutralized without
	// touching comparison operators ("1 < 2").
	reTagStart = regexp.MustCompile(`<([a-zA-Z!/])`)

	// textWS flattens line breaks and tabs to single spaces: HTML
	// renders them that way, and a flattened value cannot start a
	// markdown line of its own.
	textWS = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ", "\t", " ")

	// mdTextEscaper escapes the characters that open or close markdown
	// link/image syntax in flowing text. Backslash first so an escaped
	// bracket cannot be smuggled in as a literal one.
	mdTextEscaper = strings.NewReplacer(`\`, `\\`, `[`, `\[`, `]`, `\]`)
	// mdLinkTextEscaper additionally escapes parentheses: link text
	// sits bracket-adjacent, where a stray (…) can unbalance the
	// syntax around it.
	mdLinkTextEscaper = strings.NewReplacer(`\`, `\\`, `[`, `\[`, `]`, `\]`, `(`, `\(`, `)`, `\)`)
	// mdCellEscaper escapes the link-syntax characters plus the pipe
	// (backslashes first, so an escaped bracket cannot smuggle a raw
	// column split through).
	mdCellEscaper = strings.NewReplacer(`\`, `\\`, `[`, `\[`, `]`, `\]`, `(`, `\(`, `)`, `\)`, `|`, `\|`)

	// Heading regexes built once
	headingRes [7]*regexp.Regexp // index 1-6 used
)

func init() {
	for i := 1; i <= 6; i++ {
		tag := string(rune('0' + i))
		headingRes[i] = regexp.MustCompile(`(?is)<h` + tag + `[^>]*>(.*?)</h` + tag + `>`)
	}
}

// htmlToMarkdown converts rendered HTML into readable markdown.
// It handles headings, paragraphs, lists, code blocks, tables,
// links, images, emphasis, and strips the rest to plain text.
//
// Every extracted value is decoded and escaped per markdown slot on
// its way in (fence lengths, link brackets, cell pipes, head fields):
// the content is component- and entity-derived, and llm.md exists to
// be trusted by agents, so no value may introduce markdown structure
// of its own. Decoding happens once per value, before escaping — a
// document-wide unescape after the structure is built would re-decode
// already-escaped values (&amp;#124; → | after the pipe pass).
func htmlToMarkdown(h string) string {
	s := scrubControls(h)

	// Remove script/style blocks entirely
	s = reScript.ReplaceAllString(s, "")
	s = reStyle.ReplaceAllString(s, "")

	// Fenced code blocks are lifted out whole before any text-level
	// pass: their content is decoded once and fenced with a backtick
	// run one longer than any run inside it. The fenced markdown is
	// parked in a \x00 slot and restored after the inline passes, so
	// the normalization below cannot reach through the fence markers.
	s, fences := extractFencedBlocks(s)

	// Decode entities once per text node (runs between tags) and
	// flatten line breaks the way HTML renders them. Attribute values
	// are decoded where each regex extracts them instead.
	s = decodeTextNodes(s)

	// Inline code: a backtick run one longer than the longest run in
	// the content, so the content cannot close its own span.
	s = reCode.ReplaceAllStringFunc(s, func(m string) string {
		return mdCodeSpan(submatch(m, reCode, 1))
	})

	// Headings: h1-h6
	for i := 6; i >= 1; i-- {
		prefix := strings.Repeat("#", i)
		res := headingRes[i]
		s = res.ReplaceAllStringFunc(s, func(m string) string {
			return "\n" + prefix + " " + mdInlineText(submatch(m, res, 1)) + "\n"
		})
	}

	// Bold
	s = reStrong.ReplaceAllStringFunc(s, func(m string) string {
		return "**" + mdInlineText(submatch(m, reStrong, 1)) + "**"
	})
	s = reB.ReplaceAllStringFunc(s, func(m string) string {
		return "**" + mdInlineText(submatch(m, reB, 1)) + "**"
	})

	// Italic
	s = reEm.ReplaceAllStringFunc(s, func(m string) string {
		return "*" + mdInlineText(submatch(m, reEm, 1)) + "*"
	})
	s = reI.ReplaceAllStringFunc(s, func(m string) string {
		return "*" + mdInlineText(submatch(m, reI, 1)) + "*"
	})

	// Links: text and destination escaped per slot; href decodes here
	// (attribute values sit inside tags, which decodeTextNodes skips).
	s = reLink.ReplaceAllStringFunc(s, func(m string) string {
		sub := reLink.FindStringSubmatch(m)
		return "[" + mdLinkText(sub[2]) + "](" + mdLinkDest(html.UnescapeString(sub[1])) + ")"
	})

	// Images: alt text and src treated exactly like link text/dest.
	s = reImgAltSrc.ReplaceAllStringFunc(s, func(m string) string {
		sub := reImgAltSrc.FindStringSubmatch(m)
		return "![" + mdLinkText(html.UnescapeString(sub[1])) + "](" + mdLinkDest(html.UnescapeString(sub[2])) + ")"
	})
	s = reImgSrcAlt.ReplaceAllStringFunc(s, func(m string) string {
		sub := reImgSrcAlt.FindStringSubmatch(m)
		return "![" + mdLinkText(html.UnescapeString(sub[2])) + "](" + mdLinkDest(html.UnescapeString(sub[1])) + ")"
	})

	// Horizontal rules
	s = reHR.ReplaceAllString(s, "\n---\n")

	// Table handling
	s = convertTables(s)

	// Ordered lists: track ol/li, must run before generic <li> replacement
	s = reOlBlock.ReplaceAllStringFunc(s, func(match string) string {
		inner := reOlInner.ReplaceAllStringFunc(
			reOlTag.ReplaceAllString(match, ""),
			func(string) string { return "\n1. " },
		)
		return inner
	})
	s = reOlTag.ReplaceAllString(s, "")

	// Unordered lists: <li> → "- item"
	s = reLiOpen.ReplaceAllString(s, "\n- ")
	s = reLiClose.ReplaceAllString(s, "")

	s = reUlTag.ReplaceAllString(s, "")

	// Paragraphs → double newline
	s = rePOpen.ReplaceAllString(s, "\n\n")
	s = rePClose.ReplaceAllString(s, "\n")

	// Div → newline
	s = reDivOpen.ReplaceAllString(s, "\n")
	s = reDivClose.ReplaceAllString(s, "\n")

	// BR
	s = reBR.ReplaceAllString(s, "\n")

	// Strip all remaining tags
	s = reAnyTag.ReplaceAllString(s, "")

	// Collapse whitespace, put the fenced blocks back, trim
	s = collapseWhitespace(s)
	s = restoreFences(s, fences)
	return strings.TrimSpace(s)
}

// convertTables converts HTML tables to markdown tables. Cell content
// arrives already entity-decoded (decodeTextNodes); mdTableCell keeps
// it inside its cell.
func convertTables(h string) string {
	return reTable.ReplaceAllStringFunc(h, func(table string) string {
		var b strings.Builder
		b.WriteString("\n")

		rows := reRow.FindAllString(table, -1)

		first := true
		for _, row := range rows {
			cells := reCell.FindAllStringSubmatch(row, -1)
			if len(cells) == 0 {
				continue
			}

			b.WriteString("| ")
			for _, cell := range cells {
				content := stripTags(cell[1])
				content = strings.TrimSpace(content)
				content = mdTableCell(content)
				b.WriteString(content + " | ")
			}
			b.WriteString("\n")

			// Add separator after header row
			if first {
				for range cells {
					b.WriteString("| --- ")
				}
				b.WriteString("|\n")
				first = false
			}
		}
		b.WriteString("\n")
		return b.String()
	})
}

// stripTags removes all HTML tags from a string.
func stripTags(s string) string {
	return reStripTag.ReplaceAllString(s, "")
}

// collapseWhitespace reduces multiple blank lines to at most two newlines.
func collapseWhitespace(s string) string {
	// Trim trailing spaces from each line
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	s = strings.Join(lines, "\n")

	// Collapse 3+ newlines to 2
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}

	return s
}

// ── per-slot markdown escaping ─────────────────────────────────────
//
// llm.md is an agent-facing surface built from entity-derived content;
// each helper prepares one kind of value for one kind of markdown slot
// so a value cannot introduce structure of its own (fences, headings,
// links, table columns). The sibling per-value escaper for the SEO
// front-matter is framework/uihost/llmmd_seo.go::yamlDoubleQuote.

// scrubControls drops C0 control bytes other than tab, newline and CR.
// They are invalid in HTML text, and \x00 doubles as the fence-slot
// marker below, so an input-borne control byte must not reach it.
func scrubControls(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			return -1
		}
		return r
	}, s)
}

// extractFencedBlocks replaces every <pre><code> block with a slot
// marker and returns the built fenced markdown alongside. Content is
// tag-stripped, decoded exactly once, and fenced one backtick longer
// than the longest run it contains, so no line of it can close the
// fence.
func extractFencedBlocks(s string) (string, []string) {
	var blocks []string
	s = rePreBlock.ReplaceAllStringFunc(s, func(m string) string {
		content := stripTags(rePreBlock.FindStringSubmatch(m)[1])
		blocks = append(blocks, mdFencedBlock(html.UnescapeString(content)))
		return fmt.Sprintf("\x00f%d\x00", len(blocks)-1)
	})
	return s, blocks
}

// restoreFences puts the parked fenced blocks back. The block ends
// with a newline, so whatever follows one starts a line:
// escapeLeadingSplit neutralizes a structure initiator at its head,
// mirroring decodeTextNode for text nodes after tags.
func restoreFences(s string, blocks []string) string {
	if len(blocks) == 0 {
		return s
	}
	var b strings.Builder
	for {
		m := reFenceSlot.FindStringSubmatchIndex(s)
		if m == nil {
			b.WriteString(s)
			break
		}
		idx, _ := strconv.Atoi(s[m[2]:m[3]])
		b.WriteString(s[:m[0]])
		b.WriteString(blocks[idx])
		head, rest := escapeLeadingSplit(s[m[1]:])
		b.WriteString(head)
		s = rest
	}
	return b.String()
}

// decodeTextNodes decodes HTML entities once per text node (the runs
// between tags) and flattens line breaks to spaces, matching how HTML
// renders text. A '<' the decode produces is re-encoded as &lt; when
// it begins something tag-shaped, so a decoded &lt;script&gt; cannot
// masquerade as a real tag for the structure passes. A structure
// initiator at the head of the node (which lands at a line start once
// the preceding tag becomes a newline) is backslash-escaped.
func decodeTextNodes(s string) string {
	var b strings.Builder
	pos := 0
	for {
		loc := reStripTag.FindStringIndex(s[pos:])
		if loc == nil {
			b.WriteString(decodeTextNode(s[pos:]))
			break
		}
		b.WriteString(decodeTextNode(s[pos : pos+loc[0]]))
		b.WriteString(s[pos+loc[0] : pos+loc[1]])
		pos += loc[1]
	}
	return b.String()
}

func decodeTextNode(t string) string {
	if t == "" {
		return t
	}
	d := t
	if strings.Contains(d, "&") {
		d = reTagStart.ReplaceAllString(html.UnescapeString(d), "&lt;$1")
	}
	d = textWS.Replace(d)
	head, rest := escapeLeadingSplit(d)
	return head + rest
}

// escapeLeadingSplit splits s at its first markdown structure
// initiator, backslash-escaping it: "#", ">", "-", "+", "*", "`",
// "=" (setext rule), "|" (table row), "![" (image), and an ordered-
// list opener (digits + "." or ")"). Leading spaces and tabs are kept
// but skipped by the check. head carries the escaped initiator (or is
// empty when there is none); rest is the unexamined remainder.
func escapeLeadingSplit(s string) (head, rest string) {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	if i >= len(s) {
		return s, ""
	}
	escapeAt := func(j int) (string, string) {
		return s[:j] + `\` + s[j:j+1], s[j+1:]
	}
	switch c := s[i]; {
	case c == '#' || c == '>' || c == '-' || c == '+' || c == '*' || c == '`' || c == '=' || c == '|':
		return escapeAt(i)
	case c == '!' && i+1 < len(s) && s[i+1] == '[':
		return escapeAt(i)
	case c >= '0' && c <= '9':
		j := i
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		if j < len(s) && (s[j] == '.' || s[j] == ')') {
			return escapeAt(j)
		}
	}
	return "", s
}

// mdInlineText prepares a single-line inline value for flowing text:
// line breaks collapse to spaces (HTML renders them that way) and
// brackets are escaped so the value cannot open or close link syntax.
func mdInlineText(s string) string {
	return mdTextEscaper.Replace(textWS.Replace(s))
}

// mdLinkText prepares anchor/alt text: mdInlineText plus parentheses,
// which are bracket-adjacent in this slot.
func mdLinkText(s string) string {
	return mdLinkTextEscaper.Replace(textWS.Replace(s))
}

// mdLinkDest percent-encodes the bytes that can terminate a markdown
// link destination (whitespace, parentheses, angle brackets). A
// backslash escape would not work here: destinations do not process
// them.
func mdLinkDest(u string) string {
	var b strings.Builder
	for _, c := range []byte(u) {
		switch c {
		case ' ', '\t', '\n', '\r', '(', ')', '<', '>', '\\':
			fmt.Fprintf(&b, "%%%02X", c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// mdTableCell keeps a value inside one table cell: line breaks flatten
// (a newline would split the row) and pipes escape (a pipe would add a
// column).
func mdTableCell(s string) string {
	return mdCellEscaper.Replace(textWS.Replace(s))
}

// mdHeadField normalizes a one-line header field (a screen title, a
// description bullet): the first line only — anything after a line
// break is structure the value tried to inject, not title text — with
// inline escaping on what remains.
func mdHeadField(s string) string {
	line := s
	if i := strings.IndexAny(s, "\n\r"); i >= 0 {
		line = s[:i]
	}
	return strings.TrimRight(mdInlineText(line), " \t")
}

// mdCodeSpan wraps inline code content in a backtick run one longer
// than the longest run the content contains, padding with single
// spaces when the content starts or ends with a backtick (CommonMark
// code-span rule).
func mdCodeSpan(content string) string {
	run := strings.Repeat("`", longestBacktickRun(content)+1)
	if strings.HasPrefix(content, "`") || strings.HasSuffix(content, "`") {
		return run + " " + content + " " + run
	}
	return run + content + run
}

// mdFencedBlock fences code content with a backtick run one longer
// than the longest run the content contains (minimum three). A content
// line whose first non-space characters are a backtick run is tab-
// prefixed: a tab is four columns of indentation in CommonMark, which
// cannot close a fence, and it keeps line-walking consumers from
// misreading the line as a fence toggle.
func mdFencedBlock(content string) string {
	n := longestBacktickRun(content)
	if n < 3 {
		n = 3
	}
	fence := strings.Repeat("`", n+1)
	var b strings.Builder
	b.WriteString("\n" + fence + "\n")
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(strings.TrimLeft(line, " "), "```") {
			b.WriteString("\t")
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString(fence + "\n")
	return b.String()
}

// longestBacktickRun returns the length of the longest run of
// backticks in s. Backticks are ASCII, so byte scanning is rune-safe.
func longestBacktickRun(s string) int {
	best, cur := 0, 0
	for _, c := range []byte(s) {
		if c == '`' {
			cur++
			if cur > best {
				best = cur
			}
		} else {
			cur = 0
		}
	}
	return best
}

// submatch extracts capture group n from a match handed to a
// ReplaceAllStringFunc callback.
func submatch(m string, re *regexp.Regexp, n int) string {
	return re.FindStringSubmatch(m)[n]
}
