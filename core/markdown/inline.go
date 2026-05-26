package markdown

import (
	"strings"
	"unicode"
)

// renderInline runs the inline parser over a single block of text and emits
// HTML. Order matters: code spans are extracted first so their contents are
// not interpreted as bold/italic, then images, then links, then bold, then
// italic. Plain text segments are HTML-escaped.
func renderInline(input string) string {
	var sb strings.Builder
	i := 0
	for i < len(input) {
		ch := input[i]
		switch {
		case ch == '\\' && i+1 < len(input) && isPunct(input[i+1]):
			sb.WriteString(escapeHTML(string(input[i+1])))
			i += 2
		case ch == '`':
			end := findCodeEnd(input, i)
			if end > i {
				sb.WriteString("<code>")
				sb.WriteString(escapeHTML(input[i+1 : end]))
				sb.WriteString("</code>")
				i = end + 1
				continue
			}
			sb.WriteString("`")
			i++
		case ch == '!' && i+1 < len(input) && input[i+1] == '[':
			alt, url, end, ok := parseLink(input, i+1)
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
			text, url, end, ok := parseLink(input, i)
			if ok {
				sb.WriteString("<a href=\"")
				sb.WriteString(escapeAttr(safeLinkURL(url)))
				sb.WriteString("\">")
				sb.WriteString(renderInline(text))
				sb.WriteString("</a>")
				i = end
				continue
			}
			sb.WriteString("[")
			i++
		case ch == '*' || ch == '_':
			delim, run := scanRun(input, i, ch)
			closeIdx := findClosingDelim(input, i+run, delim, run)
			if closeIdx >= 0 {
				inner := input[i+run : closeIdx]
				switch run {
				case 1:
					sb.WriteString("<em>")
					sb.WriteString(renderInline(inner))
					sb.WriteString("</em>")
				case 2:
					sb.WriteString("<strong>")
					sb.WriteString(renderInline(inner))
					sb.WriteString("</strong>")
				default:
					sb.WriteString("<strong><em>")
					sb.WriteString(renderInline(inner))
					sb.WriteString("</em></strong>")
				}
				i = closeIdx + run
				continue
			}
			sb.WriteString(escapeHTML(string(ch)))
			i++
		case ch == '\n':
			sb.WriteString("<br>\n")
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

// scanRun returns the run length of the same delimiter starting at i.
// It also returns the delimiter byte for clarity at call sites.
func scanRun(s string, i int, delim byte) (byte, int) {
	n := 0
	for i+n < len(s) && s[i+n] == delim {
		n++
	}
	if n > 3 {
		n = 3
	}
	return delim, n
}

// findClosingDelim looks for a matching delimiter run after position start.
// CommonMark has a more complex flanking rule; we use a simple "next run of
// the same length" search which works for everyday docs.
func findClosingDelim(s string, start int, delim byte, run int) int {
	i := start
	for i < len(s) {
		if s[i] == '`' {
			end := findCodeEnd(s, i)
			if end > i {
				i = end + 1
				continue
			}
		}
		if s[i] == delim {
			n := 0
			for i+n < len(s) && s[i+n] == delim {
				n++
			}
			if n == run {
				return i
			}
			i += n
			continue
		}
		i++
	}
	return -1
}

// findCodeEnd returns the index of the closing backtick run that matches the
// opening run starting at i. -1 if unbalanced.
func findCodeEnd(s string, i int) int {
	open := 0
	for i+open < len(s) && s[i+open] == '`' {
		open++
	}
	j := i + open
	for j < len(s) {
		if s[j] != '`' {
			j++
			continue
		}
		n := 0
		for j+n < len(s) && s[j+n] == '`' {
			n++
		}
		if n == open {
			return j
		}
		j += n
	}
	return -1
}

// parseLink parses [text](url) starting at the '[' at position i.
// Returns the link text, URL, the index immediately after the closing ')',
// and a success flag. Used for both links and (with caller adjustments) images.
func parseLink(s string, i int) (text, url string, end int, ok bool) {
	if i >= len(s) || s[i] != '[' {
		return "", "", 0, false
	}
	j := i + 1
	depth := 1
	for j < len(s) && depth > 0 {
		switch s[j] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				goto closed
			}
		}
		j++
	}
	return "", "", 0, false
closed:
	text = s[i+1 : j]
	if j+1 >= len(s) || s[j+1] != '(' {
		return "", "", 0, false
	}
	k := j + 2
	urlStart := k
	for k < len(s) && s[k] != ')' {
		k++
	}
	if k >= len(s) {
		return "", "", 0, false
	}
	url = strings.TrimSpace(s[urlStart:k])
	return text, url, k + 1, true
}

func isPunct(b byte) bool {
	return strings.ContainsRune("\\`*_{}[]()#+-.!|~>", rune(b))
}

// slugify produces an anchor-friendly id from heading text.
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

// ---------------------------------------------------------------------------
// Escaping
// ---------------------------------------------------------------------------

var htmlEscaper = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	"\"", "&quot;",
	"'", "&#39;",
)

func escapeHTML(s string) string { return htmlEscaper.Replace(s) }

// escapeAttr is the same as escapeHTML for now — we never embed attribute
// values from user-controlled context without escaping, and the same set of
// characters needs to be neutralised in either spot.
func escapeAttr(s string) string { return htmlEscaper.Replace(s) }

// safeLinkURL refuses script-y schemes inside a markdown link href.
// `javascript:`, `vbscript:` and the small set of `data:` types that
// render executable content (text/html, application/xhtml+xml,
// image/svg+xml — SVG can carry inline JS) get replaced with `#` so a
// click can't navigate to an active payload. Other schemes — http(s),
// mailto, tel, fragment-only, relative paths — pass through unchanged.
func safeLinkURL(url string) string {
	if isDangerousURLScheme(url) {
		return "#"
	}
	return url
}

// safeImageURL is the image counterpart of safeLinkURL: an `<img src>`
// can't navigate, but a same-origin `data:text/html` could still be
// piped into JS that loads the resource into a same-origin frame, and
// `javascript:` URLs render nothing useful anyway. We allow data:
// image/* (the legitimate use case for embedded images) and reject
// the rest of the dangerous set.
func safeImageURL(url string) string {
	lower := strings.ToLower(strings.TrimLeft(url, " \t\r\n"))
	if strings.HasPrefix(lower, "data:image/") && !strings.HasPrefix(lower, "data:image/svg") {
		return url
	}
	if isDangerousURLScheme(url) {
		return "#"
	}
	return url
}

// isDangerousURLScheme reports whether url begins with a URL scheme
// known to execute script or render HTML in a navigation context.
// Leading ASCII whitespace and control chars are ignored — they're
// stripped from the scheme by the HTML parser anyway, so we match the
// parser's view.
func isDangerousURLScheme(url string) bool {
	trimmed := url
	for len(trimmed) > 0 && (trimmed[0] == ' ' || trimmed[0] == '\t' || trimmed[0] == '\r' || trimmed[0] == '\n' || trimmed[0] < 0x20) {
		trimmed = trimmed[1:]
	}
	lower := strings.ToLower(trimmed)
	switch {
	case strings.HasPrefix(lower, "javascript:"):
		return true
	case strings.HasPrefix(lower, "vbscript:"):
		return true
	case strings.HasPrefix(lower, "data:text/html"):
		return true
	case strings.HasPrefix(lower, "data:application/xhtml"):
		return true
	case strings.HasPrefix(lower, "data:image/svg"):
		return true
	}
	return false
}
