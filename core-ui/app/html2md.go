//check-csp:ignore-file
// This file builds regex patterns that match (and strip) <script> and
// <style> blocks from rendered HTML before converting it to Markdown
// for /llm.md. The patterns never emit script tags — they only consume
// them — but the literal `<script` substring trips the no-inline-script
// linter. The directive exempts this file from that check.
package app

import (
	"html"
	"regexp"
	"strings"
)

// Pre-compiled regexes — compiled once, reused per request.
var (
	reScript    = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	reStyle     = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	rePreOpen   = regexp.MustCompile(`(?is)<pre[^>]*>\s*<code[^>]*>`)
	rePreClose  = regexp.MustCompile(`(?is)</code>\s*</pre>`)
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
func htmlToMarkdown(h string) string {
	s := h

	// Remove script/style blocks entirely
	s = reScript.ReplaceAllString(s, "")
	s = reStyle.ReplaceAllString(s, "")

	// Code blocks: <pre><code>...</code></pre>
	s = rePreOpen.ReplaceAllString(s, "\n```\n")
	s = rePreClose.ReplaceAllString(s, "\n```\n")

	// Inline code
	s = reCode.ReplaceAllString(s, "`$1`")

	// Headings: h1-h6
	for i := 6; i >= 1; i-- {
		prefix := strings.Repeat("#", i)
		s = headingRes[i].ReplaceAllString(s, "\n"+prefix+" $1\n")
	}

	// Bold
	s = reStrong.ReplaceAllString(s, "**$1**")
	s = reB.ReplaceAllString(s, "**$1**")

	// Italic
	s = reEm.ReplaceAllString(s, "*$1*")
	s = reI.ReplaceAllString(s, "*$1*")

	// Links
	s = reLink.ReplaceAllString(s, "[$2]($1)")

	// Images
	s = reImgAltSrc.ReplaceAllString(s, "![$1]($2)")
	s = reImgSrcAlt.ReplaceAllString(s, "![$2]($1)")

	// Horizontal rules
	s = reHR.ReplaceAllString(s, "\n---\n")

	// Table handling
	s = convertTables(s)

	// Ordered lists: track ol/li — must run before generic <li> replacement
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

	// Decode HTML entities (covers named + numeric — much more complete
	// than listing a handful of replacements).
	s = html.UnescapeString(s)

	// Collapse whitespace
	s = collapseWhitespace(s)

	return strings.TrimSpace(s)
}

// convertTables converts HTML tables to markdown tables.
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
				content = strings.ReplaceAll(content, "|", "\\|")
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
