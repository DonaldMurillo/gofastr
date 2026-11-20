// check-csp:ignore-file

package app

import (
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
)

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
	reLiOpen    = regexp.MustCompile(`(?is)<li(?:\s[^>]*)?>`)
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

	reTable   = regexp.MustCompile(`(?is)<table[^>]*>(.*?)</table>`)
	reRow     = regexp.MustCompile(`(?is)<tr[^>]*>(.*?)</tr>`)
	reCell    = regexp.MustCompile(`(?is)<t[hd][^>]*>(.*?)</t[hd]>`)
	reOlInner = regexp.MustCompile(`(?is)<li(?:\s[^>]*)?>`)

	reDtDd     = regexp.MustCompile(`(?is)</dt>\s*<dd(?:\s[^>]*)?>`)
	reDlBreak  = regexp.MustCompile(`(?is)<d[td](?:\s[^>]*)?>|</d[td]>`)
	reCellSlot = regexp.MustCompile(slotOpen + "a([0-9]+)" + slotClose)

	rePreBlock  = regexp.MustCompile(`(?is)<pre[^>]*>\s*<code[^>]*>(.*?)</code>\s*</pre>`)
	rePreBare   = regexp.MustCompile(`(?is)<pre(?:\s[^>]*)?>(.*?)</pre>`)
	reFenceSlot = regexp.MustCompile(slotOpen + "f([0-9]+)" + slotClose)
	reTagStart  = regexp.MustCompile(`<([a-zA-Z!/` + slotOpen + `])`)

	textWS = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ", "\t", " ")

	mdTextEscaper     = strings.NewReplacer(`\`, `\\`, `[`, `\[`, `]`, `\]`)
	mdLinkTextEscaper = strings.NewReplacer(`\`, `\\`, `[`, `\[`, `]`, `\]`, `(`, `\(`, `)`, `\)`)
	mdCellEscaper     = strings.NewReplacer(`\`, `\\`, `[`, `\[`, `]`, `\]`, `(`, `\(`, `)`, `\)`, `|`, `\|`)

	headingRes [7]*regexp.Regexp // index 1-6 used
)

const (
	slotOpen  = "\x00"
	slotClose = "\x01"
	parkedLT  = "\x02"
	parkedGT  = "\x03"
)

func init() {
	for i := 1; i <= 6; i++ {
		tag := string(rune('0' + i))
		headingRes[i] = regexp.MustCompile(`(?is)<h` + tag + `[^>]*>(.*?)</h` + tag + `>`)
	}
}

func htmlToMarkdown(h string) string {
	s := scrubControls(h)

	s = reScript.ReplaceAllString(s, "")
	s = reStyle.ReplaceAllString(s, "")

	s, fences := extractFencedBlocks(s)
	var inlineCode []string
	s = reCode.ReplaceAllStringFunc(s, func(match string) string {
		inlineCode = append(inlineCode, textWS.Replace(decodeEntities(stripTags(submatch(match, reCode, 1)))))
		return "<code></code>"
	})

	s = decodeTextNodes(s)

	inlineIndex := 0
	s = reCode.ReplaceAllStringFunc(s, func(string) string {
		content := inlineCode[inlineIndex]
		inlineIndex++
		return mdCodeSpan(strings.NewReplacer("<", parkedLT, ">", parkedGT).Replace(content))
	})

	for i := 6; i >= 1; i-- {
		prefix := strings.Repeat("#", i)
		res := headingRes[i]
		s = res.ReplaceAllStringFunc(s, func(m string) string {
			return "\n" + prefix + " " + mdInlineText(submatch(m, res, 1)) + "\n"
		})
	}

	s = reStrong.ReplaceAllStringFunc(s, func(m string) string {
		return "**" + mdInlineText(submatch(m, reStrong, 1)) + "**"
	})
	s = reB.ReplaceAllStringFunc(s, func(m string) string {
		return "**" + mdInlineText(submatch(m, reB, 1)) + "**"
	})

	s = reEm.ReplaceAllStringFunc(s, func(m string) string {
		return "*" + mdInlineText(submatch(m, reEm, 1)) + "*"
	})
	s = reI.ReplaceAllStringFunc(s, func(m string) string {
		return "*" + mdInlineText(submatch(m, reI, 1)) + "*"
	})

	s = convertTables(s, fences)

	s = reLink.ReplaceAllStringFunc(s, func(m string) string {
		sub := reLink.FindStringSubmatch(m)
		return "[" + mdLinkText(sub[2]) + "](" + mdLinkDest(decodeEntities(sub[1])) + ")"
	})

	s = reImgAltSrc.ReplaceAllStringFunc(s, func(m string) string {
		sub := reImgAltSrc.FindStringSubmatch(m)
		return "![" + mdImageAlt(sub[1]) + "](" + mdLinkDest(decodeEntities(sub[2])) + ")"
	})
	s = reImgSrcAlt.ReplaceAllStringFunc(s, func(m string) string {
		sub := reImgSrcAlt.FindStringSubmatch(m)
		return "![" + mdImageAlt(sub[2]) + "](" + mdLinkDest(decodeEntities(sub[1])) + ")"
	})

	s = reHR.ReplaceAllString(s, "\n---\n")

	s = reOlBlock.ReplaceAllStringFunc(s, func(match string) string {
		inner := reOlInner.ReplaceAllStringFunc(
			reOlTag.ReplaceAllString(match, ""),
			func(string) string { return "\n1. " },
		)
		return inner
	})
	s = reOlTag.ReplaceAllString(s, "")

	s = reLiOpen.ReplaceAllString(s, "\n- ")
	s = reLiClose.ReplaceAllString(s, "")

	s = reUlTag.ReplaceAllString(s, "")

	s = reDtDd.ReplaceAllString(s, ": ")
	s = reDlBreak.ReplaceAllString(s, "\n")

	s = rePOpen.ReplaceAllString(s, "\n\n")
	s = rePClose.ReplaceAllString(s, "\n")

	s = reDivOpen.ReplaceAllString(s, "\n")
	s = reDivClose.ReplaceAllString(s, "\n")

	s = reBR.ReplaceAllString(s, "\n")

	s = reAnyTag.ReplaceAllString(s, "")

	s = collapseWhitespace(s)
	s = restoreFences(s, fences)
	s = strings.NewReplacer(parkedLT, "<", parkedGT, ">").Replace(s)
	return strings.TrimSpace(s)
}

func convertTables(h string, fences []fencedBlock) string {
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
				b.WriteString(mdTableCellHTML(cell[1], fences) + " | ")
			}
			b.WriteString("\n")

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

func mdTableCellHTML(cellHTML string, fences []fencedBlock) string {
	var parked []string
	park := func(md string) string {
		parked = append(parked, md)
		return fmt.Sprintf(slotOpen+"a%d"+slotClose, len(parked)-1)
	}
	cellHTML = reFenceSlot.ReplaceAllStringFunc(cellHTML, func(m string) string {
		idx, err := strconv.Atoi(reFenceSlot.FindStringSubmatch(m)[1])
		if err != nil || idx >= len(fences) {
			return ""
		}
		code := strings.ReplaceAll(textWS.Replace(strings.TrimRight(fences[idx].code, "\r\n")), "|", `\|`)
		return park(strings.NewReplacer("<", parkedLT, ">", parkedGT).Replace(mdCodeSpan(code)))
	})
	text := func(v string) string {
		return strings.ReplaceAll(mdLinkText(stripTags(reBR.ReplaceAllString(v, " "))), "|", `\|`)
	}
	dest := func(v string) string { return strings.ReplaceAll(mdLinkDest(decodeEntities(v)), "|", "%7C") }
	image := func(alt, src string) string {
		return "![" + strings.ReplaceAll(mdImageAlt(alt), "|", `\|`) + "](" + dest(src) + ")"
	}
	c := reImgAltSrc.ReplaceAllStringFunc(cellHTML, func(m string) string {
		sub := reImgAltSrc.FindStringSubmatch(m)
		return park(image(sub[1], sub[2]))
	})
	c = reImgSrcAlt.ReplaceAllStringFunc(c, func(m string) string {
		sub := reImgSrcAlt.FindStringSubmatch(m)
		return park(image(sub[2], sub[1]))
	})
	c = reLink.ReplaceAllStringFunc(c, func(m string) string {
		sub := reLink.FindStringSubmatch(m)
		return park("[" + strings.TrimSpace(text(sub[2])) + "](" + dest(sub[1]) + ")")
	})
	c = reBR.ReplaceAllString(c, " ")
	c = mdTableCell(strings.TrimSpace(stripTags(c)))
	return restoreCellSlots(c, parked, len(parked))
}

func restoreCellSlots(s string, parked []string, limit int) string {
	return reCellSlot.ReplaceAllStringFunc(s, func(m string) string {
		idx, err := strconv.Atoi(reCellSlot.FindStringSubmatch(m)[1])
		if err != nil || idx >= limit {
			return ""
		}
		return restoreCellSlots(parked[idx], parked, idx)
	})
}

func stripTags(s string) string {
	return reStripTag.ReplaceAllString(s, "")
}

func collapseWhitespace(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	s = strings.Join(lines, "\n")

	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}

	return s
}

func scrubControls(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			return -1
		}
		return r
	}, s)
}

func decodeEntities(s string) string {
	return scrubControls(html.UnescapeString(s))
}

type fencedBlock struct {
	md, code string
}

func extractFencedBlocks(s string) (string, []fencedBlock) {
	var blocks []fencedBlock
	add := func(content string) string {
		code := decodeEntities(content)
		blocks = append(blocks, fencedBlock{md: mdFencedBlock(code), code: code})
		return fmt.Sprintf(slotOpen+"f%d"+slotClose, len(blocks)-1)
	}
	s = rePreBlock.ReplaceAllStringFunc(s, func(m string) string {
		return add(stripTags(rePreBlock.FindStringSubmatch(m)[1]))
	})
	s = rePreBare.ReplaceAllStringFunc(s, func(m string) string {
		content := stripTags(rePreBare.FindStringSubmatch(m)[1])
		if !strings.Contains(strings.TrimRight(content, "\r\n"), "\n") {
			return m
		}
		return add(content)
	})
	return s, blocks
}

func restoreFences(s string, blocks []fencedBlock) string {
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
		b.WriteString(blocks[idx].md)
		head, rest := escapeLeadingSplit(s[m[1]:])
		b.WriteString(head)
		s = rest
	}
	return b.String()
}

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
		d = reTagStart.ReplaceAllString(decodeAroundFenceSlots(d), "&lt;$1")
	}
	d = textWS.Replace(d)
	head, rest := escapeLeadingSplit(d)
	return head + rest
}

func decodeAroundFenceSlots(t string) string {
	var b strings.Builder
	for {
		loc := reFenceSlot.FindStringIndex(t)
		if loc == nil {
			b.WriteString(decodeEntities(t))
			return b.String()
		}
		b.WriteString(decodeEntities(t[:loc[0]]))
		b.WriteString(t[loc[0]:loc[1]])
		t = t[loc[1]:]
	}
}

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

func mdInlineText(s string) string {
	return mdTextEscaper.Replace(textWS.Replace(s))
}

func mdLinkText(s string) string {
	return mdLinkTextEscaper.Replace(textWS.Replace(s))
}

func mdImageAlt(alt string) string {
	return mdLinkText(reTagStart.ReplaceAllString(decodeEntities(alt), "&lt;$1"))
}

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

func mdTableCell(s string) string {
	return mdCellEscaper.Replace(textWS.Replace(s))
}

func mdHeadField(s string) string {
	line := s
	if i := strings.IndexAny(s, "\n\r"); i >= 0 {
		line = s[:i]
	}
	return strings.TrimRight(mdInlineText(line), " \t")
}

func mdCodeSpan(content string) string {
	run := strings.Repeat("`", longestBacktickRun(content)+1)
	spaced := strings.HasPrefix(content, " ") && strings.HasSuffix(content, " ") && strings.Trim(content, " ") != ""
	if spaced || strings.HasPrefix(content, "`") || strings.HasSuffix(content, "`") {
		return run + " " + content + " " + run
	}
	return run + content + run
}

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

func submatch(m string, re *regexp.Regexp, n int) string {
	return re.FindStringSubmatch(m)[n]
}
