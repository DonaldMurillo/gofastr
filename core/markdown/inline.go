package markdown

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
)

const maxInlineDepth = 64

const maxURLParenDepth = 32

func renderInline(input string) string {
	return renderInlineDepth(input, 0)
}

func renderInlineDepth(input string, depth int) string {
	var links *linkIndex
	if strings.IndexByte(input, '[') >= 0 {
		links = buildLinkIndex(input)
	}
	return renderInlineAt(input, depth, links, 0)
}

func renderInlineAt(input string, depth int, links *linkIndex, base int) string {
	if depth >= maxInlineDepth {
		return escapeHTML(input)
	}
	var sb strings.Builder
	var noCloser map[int]bool
	var runs *backtickRuns
	if strings.IndexByte(input, '`') >= 0 {
		runs = buildBacktickRuns(input)
	}
	i := 0
	for i < len(input) {
		ch := input[i]
		switch {
		case ch == '\\' && i+1 < len(input) && input[i+1] == '\n':
			sb.WriteString("<br>\n")
			i += 2
		case ch == ' ':
			j := i
			for j < len(input) && input[j] == ' ' {
				j++
			}
			switch {
			case j == len(input):
			case input[j] == '\n' && j-i >= 2:
				sb.WriteString("<br>\n")
				j++
			case input[j] == '\n':
				sb.WriteByte('\n')
				j++
			default:
				sb.WriteString(input[i:j])
			}
			i = j
		case ch == '\\' && i+1 < len(input) && isPunct(input[i+1]):
			sb.WriteString(escapeHTML(string(input[i+1])))
			i += 2
		case ch == '`':
			end, open := runs.findCodeEnd(i)
			if end >= 0 {
				sb.WriteString("<code>")
				sb.WriteString(escapeHTML(input[i+open : end]))
				sb.WriteString("</code>")
				i = end + open
				continue
			}
			sb.WriteString(input[i : i+open])
			i += open
		case ch == '!' && i+1 < len(input) && input[i+1] == '[':
			alt, url, end, ok := links.parseLink(input, base, i+1)
			if ok {
				sb.WriteString("<img src=\"")
				sb.WriteString(escapeAttr(safeImageURL(url)))
				sb.WriteString("\" alt=\"")
				sb.WriteString(escapeAttr(alt))
				sb.WriteString("\">")
				i = end
				continue
			}
			sb.WriteString("!")
			i++
		case ch == '[':
			text, url, end, ok := links.parseLink(input, base, i)
			if ok {
				sb.WriteString("<a href=\"")
				sb.WriteString(escapeAttr(safeLinkURL(url)))
				sb.WriteString("\">")
				sb.WriteString(renderInlineAt(text, depth+1, links, base+i+1))
				sb.WriteString("</a>")
				i = end
				continue
			}
			sb.WriteString("[")
			i++
		case ch == '_' && wordByteBefore(input, i):
			j := i
			for j < len(input) && input[j] == '_' {
				j++
			}
			sb.WriteString(input[i:j])
			i = j
		case ch == '*' || ch == '_':
			delim, run := scanRun(input, i, ch)
			if i+run >= len(input) || isBlank(input[i+run]) {
				sb.WriteString(input[i : i+run])
				i += run
				continue
			}
			key := int(delim)<<2 | run
			closeIdx := -1
			if noCloser == nil || !noCloser[key] {
				closeIdx = findClosingDelim(runs, links, input, base, i+run, delim, run)
				if closeIdx < 0 {
					if noCloser == nil {
						noCloser = make(map[int]bool, 4)
					}
					noCloser[key] = true
				}
			}
			if closeIdx >= 0 {
				inner := input[i+run : closeIdx]
				switch run {
				case 1:
					sb.WriteString("<em>")
					sb.WriteString(renderInlineAt(inner, depth+1, links, base+i+run))
					sb.WriteString("</em>")
				case 2:
					sb.WriteString("<strong>")
					sb.WriteString(renderInlineAt(inner, depth+1, links, base+i+run))
					sb.WriteString("</strong>")
				default:
					sb.WriteString("<strong><em>")
					sb.WriteString(renderInlineAt(inner, depth+1, links, base+i+run))
					sb.WriteString("</em></strong>")
				}
				i = closeIdx + run
				continue
			}
			sb.WriteString(escapeHTML(string(ch)))
			i++
		case ch == '\n':
			sb.WriteByte('\n')
			i++
		case ch == '<', ch == '>', ch == '&', ch == '"', ch == '\'':
			sb.WriteString(escapeHTML(string(ch)))
			i++
		default:
			sb.WriteByte(ch)
			i++
		}
	}
	return sb.String()
}

func scanRun(s string, i int, delim byte) (byte, int) {
	n := 0
	for n < 3 && i+n < len(s) && s[i+n] == delim {
		n++
	}
	return delim, n
}

func findClosingDelim(runs *backtickRuns, links *linkIndex, s string, base, start int, delim byte, run int) int {
	i := start
	for i < len(s) {
		if s[i] == '[' {
			if _, _, end, ok := links.parseLink(s, base, i); ok {
				i = end
				continue
			}
			i++
			continue
		}
		if s[i] == '`' {
			end, open := runs.findCodeEnd(i)
			if end >= 0 {
				i = end + open
				continue
			}
			i += open
			continue
		}
		if s[i] == delim {
			n := 0
			for i+n < len(s) && s[i+n] == delim {
				n++
			}
			if n == run && !isBlank(s[i-1]) && (delim != '_' || !wordByteAt(s, i+n)) {
				return i
			}
			i += n
			continue
		}
		i++
	}
	return -1
}

type backtickRuns struct {
	start []int
	lens  []int
	byLen map[int][]int
}

func buildBacktickRuns(s string) *backtickRuns {
	b := &backtickRuns{byLen: make(map[int][]int)}
	for i := 0; i < len(s); {
		if s[i] != '`' {
			i++
			continue
		}
		j := i + 1
		for j < len(s) && s[j] == '`' {
			j++
		}
		l := j - i
		b.start = append(b.start, i)
		b.lens = append(b.lens, l)
		b.byLen[l] = append(b.byLen[l], i)
		i = j
	}
	return b
}

func (b *backtickRuns) findCodeEnd(i int) (end, open int) {
	r := sort.Search(len(b.start), func(k int) bool { return b.start[k] > i }) - 1
	open = b.start[r] + b.lens[r] - i
	starts := b.byLen[open]
	k := sort.Search(len(starts), func(k int) bool { return starts[k] >= i+open })
	if k == len(starts) {
		return -1, open
	}
	return starts[k], open
}

type linkIndex struct {
	opens   []int
	match   []int
	parens  []int
	lparens []int
}

func buildLinkIndex(s string) *linkIndex {
	l := &linkIndex{}
	var stack []int
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '[':
			stack = append(stack, len(l.opens))
			l.opens = append(l.opens, i)
			l.match = append(l.match, -1)
		case ']':
			if n := len(stack); n > 0 {
				l.match[stack[n-1]] = i
				stack = stack[:n-1]
			}
		case '(':
			l.lparens = append(l.lparens, i)
		case ')':
			l.parens = append(l.parens, i)
		}
	}
	return l
}

func (l *linkIndex) parseLink(s string, base, i int) (text, url string, end int, ok bool) {
	if l == nil || i >= len(s) || s[i] != '[' {
		return "", "", 0, false
	}
	limit := base + len(s)
	o := sort.SearchInts(l.opens, base+i)
	if o == len(l.opens) || l.opens[o] != base+i {
		return "", "", 0, false
	}
	if m := l.match[o]; m < 0 || m >= limit {
		return "", "", 0, false
	}
	j := l.match[o] - base
	text = s[i+1 : j]
	if j+1 >= len(s) || s[j+1] != '(' {
		return "", "", 0, false
	}
	urlStart := j + 2
	p := sort.SearchInts(l.parens, base+urlStart)
	if p == len(l.parens) || l.parens[p] >= limit {
		return "", "", 0, false
	}
	lo := sort.SearchInts(l.lparens, base+urlStart)
	for q := p; q < len(l.parens) && l.parens[q] < limit && q-p <= maxURLParenDepth; q++ {
		if sort.SearchInts(l.lparens, l.parens[q])-lo == q-p {
			p = q
			break
		}
	}
	k := l.parens[p] - base
	url = strings.TrimSpace(s[urlStart:k])
	return text, url, k + 1, true
}

func wordByteBefore(s string, i int) bool {
	if i == 0 {
		return false
	}
	r, _ := utf8.DecodeLastRuneInString(s[:i])
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

func wordByteAt(s string, i int) bool {
	if i >= len(s) {
		return false
	}
	r, _ := utf8.DecodeRuneInString(s[i:])
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

func isBlank(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n'
}

func isPunct(b byte) bool {
	return strings.ContainsRune("\\`*_{}[]()#+-.!|~>", rune(b))
}

func slugify(text string) string {
	var sb strings.Builder
	prevDash := true
	for _, r := range strings.ToLower(text) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			sb.WriteRune(r)
			prevDash = false
		case r == ' ' || r == '-' || r == '_':
			if !prevDash {
				sb.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(sb.String(), "-")
}

var htmlEscaper = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	"\"", "&quot;",
	"'", "&#39;",
)

func escapeHTML(s string) string { return htmlEscaper.Replace(s) }

func escapeAttr(s string) string { return htmlEscaper.Replace(s) }

func safeLinkURL(url string) string {
	url = stripURLControlBytes(url)
	if urlsafe.Clean(url, urlsafe.Anchor) == "" {
		return "#"
	}
	return url
}

func safeImageURL(url string) string {
	url = stripURLControlBytes(url)
	if urlsafe.Clean(url, urlsafe.ImageSource) == "" {
		return "#"
	}
	return url
}

func stripURLControlBytes(url string) string {
	if !strings.ContainsAny(url, "\t\n\r\x00") {
		return url
	}
	var sb strings.Builder
	sb.Grow(len(url))
	for i := 0; i < len(url); i++ {
		switch url[i] {
		case '\t', '\n', '\r', '\x00':
		default:
			sb.WriteByte(url[i])
		}
	}
	return sb.String()
}
