package markdown

import (
	"sort"
	"strings"
	"unicode"
)

// maxInlineDepth bounds inline nesting (link text, emphasis inner content).
// renderInline recurses once per nesting level: attacker-supplied markdown
// like "[[[…x…](u)](u)](u)" or deeply nested emphasis drives that recursion
// arbitrarily deep, exhausting the goroutine stack (an unrecoverable crash)
// and burning super-linear CPU. Past the cap we stop recursing and emit the
// remaining inner text verbatim (HTML-escaped), failing closed without
// dropping content, exactly like maxBlockquoteDepth in the block layer.
const maxInlineDepth = 64

// renderInline runs the inline parser over a single block of text and emits
// HTML. Order matters: code spans are extracted first so their contents are
// not interpreted as bold/italic, then images, then links, then bold, then
// italic. Plain text segments are HTML-escaped.
func renderInline(input string) string {
	return renderInlineDepth(input, 0)
}

func renderInlineDepth(input string, depth int) string {
	if depth >= maxInlineDepth {
		// Bail out: emit the rest of this block as escaped plain text
		// rather than recursing further into nested constructs.
		return escapeHTML(input)
	}
	var sb strings.Builder
	// noCloser memoizes (delim,run) pairs for which a scan has already
	// reached end-of-input without finding a matching closing run. Once a
	// run has no closer from some position, no later opener of the same
	// (delim,run) can find one either (its scan covers a suffix of the same
	// region), so we skip the rescan. This turns the unmatched-emphasis
	// case from O(n^2) into O(n), a CPU-DoS guard.
	var noCloser map[int]bool
	// runs indexes the block's maximal backtick runs so code-span closer
	// lookups are binary searches instead of tail scans. Built only when
	// the block contains a backtick at all; see backtickRuns for why a
	// scan-per-lookup is not just slow but a CPU-DoS on adversarial input.
	var runs *backtickRuns
	if strings.IndexByte(input, '`') >= 0 {
		runs = buildBacktickRuns(input)
	}
	i := 0
	for i < len(input) {
		ch := input[i]
		switch {
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
			// Unmatched run: the WHOLE run is literal text (CommonMark:
			// a backtick string not closed by a matching run is literal).
			// Consume the entire run, never one byte at a time. The next
			// position of the same run would re-run findCodeEnd for a
			// SHORTER length over the same tail, costing run × tail on
			// adversarial input (measured 639 ms on a 64 KiB document
			// before this skip). The emphasis branch's noCloser memo
			// cannot be reused here: its key is the run length, and each
			// successive position of a backtick run scans for a different
			// length, so a memo keyed that way never hits.
			sb.WriteString(input[i : i+open])
			i += open
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
				sb.WriteString(renderInlineDepth(text, depth+1))
				sb.WriteString("</a>")
				i = end
				continue
			}
			sb.WriteString("[")
			i++
		case ch == '*' || ch == '_':
			delim, run := scanRun(input, i, ch)
			key := int(delim)<<2 | run
			closeIdx := -1
			if noCloser == nil || !noCloser[key] {
				closeIdx = findClosingDelim(runs, input, i+run, delim, run)
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
					sb.WriteString(renderInlineDepth(inner, depth+1))
					sb.WriteString("</em>")
				case 2:
					sb.WriteString("<strong>")
					sb.WriteString(renderInlineDepth(inner, depth+1))
					sb.WriteString("</strong>")
				default:
					sb.WriteString("<strong><em>")
					sb.WriteString(renderInlineDepth(inner, depth+1))
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
	// Stop counting at 3: the caller only distinguishes runs of 1, 2 and
	// 3+, and counting a full run of N identical delimiters at every
	// position would be O(n^2) on a long unmatched run (a CPU-DoS vector).
	n := 0
	for n < 3 && i+n < len(s) && s[i+n] == delim {
		n++
	}
	return delim, n
}

// findClosingDelim looks for a matching delimiter run after position start.
// CommonMark has a more complex flanking rule; we use a simple "next run of
// the same length" search which works for everyday docs.
func findClosingDelim(runs *backtickRuns, s string, start int, delim byte, run int) int {
	i := start
	for i < len(s) {
		if s[i] == '`' {
			end, open := runs.findCodeEnd(i)
			if end >= 0 {
				i = end + open
				continue
			}
			// Unmatched code-span opener: skip the entire run so the walk
			// does not call findCodeEnd again from every backtick of the
			// same run — each call would rescan the tail for a shorter
			// length, the same run × tail blowup the main loop guards
			// against. Skipping the closing run entirely (end + open, not
			// end + 1) also stops the walk from re-entering a matched
			// span's closing run mid-run.
			i += open
			continue
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

// backtickRuns indexes the maximal backtick runs of one block so that
// code-span closer lookups are binary searches instead of forward scans.
//
// A scan-per-lookup is not merely slow, it is a CPU-DoS ladder: after the
// whole-run skip in the caller, every DISTINCT run length can still fail
// once, and each failure walks the remaining input. Runs of lengths
// 1..K fit in ~K²/2 bytes, so K failing scans over that cost O(n·√n) —
// measured 552 ms on a 1 MB document built as runs of 1..1414 backticks.
// With the index, a lookup is O(log n) and the shape is linear.
type backtickRuns struct {
	// start/lens list each maximal run's start offset and full length, in
	// document order. byLen maps a run length to the ascending starts of
	// all runs with that length. All three are int: Render caps no
	// document size, so an int32 offset wraps on a block over
	// math.MaxInt32 — a negative "open" reaches the caller and slices
	// out of range (pinned by TestMarkdown_GiantBacktickRunNoInt32Wrap).
	start []int
	lens  []int
	byLen map[int][]int
}

// buildBacktickRuns makes the index for s. Callers only build it when s
// contains a backtick (strings.IndexByte gate), keeping backtick-free
// blocks on the fast path.
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

// findCodeEnd returns the start index of the closing backtick run that
// matches the opening run at i, together with that opening run's length
// open (the matching closing run has the same length, so callers can skip
// past both runs in full). end is -1 if no matching run exists. i must be
// a backtick of a block this index was built for; mid-run positions use
// the remaining suffix as the opener, the same rule a linear scan had.
func (b *backtickRuns) findCodeEnd(i int) (end, open int) {
	r := sort.Search(len(b.start), func(k int) bool { return b.start[k] > i }) - 1
	// Callers only query at backticks, so run r contains i.
	open = b.start[r] + b.lens[r] - i
	starts := b.byLen[open]
	k := sort.Search(len(starts), func(k int) bool { return starts[k] >= i+open })
	if k == len(starts) {
		return -1, open
	}
	return starts[k], open
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

// escapeAttr is the same as escapeHTML for now. We never embed attribute
// values from user-controlled context without escaping, and the same set of
// characters needs to be neutralised in either spot.
func escapeAttr(s string) string { return htmlEscaper.Replace(s) }

// safeLinkURL refuses script-y schemes inside a markdown link href.
// `javascript:`, `vbscript:` and the small set of `data:` types that
// render executable content (text/html, application/xhtml+xml,
// image/svg+xml, which can carry inline JS) get replaced with `#` so a
// click can't navigate to an active payload. Other schemes, http(s),
// mailto, tel, fragment-only, relative paths, pass through unchanged.
func safeLinkURL(url string) string {
	url = stripURLControlBytes(url)
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
	url = stripURLControlBytes(url)
	lower := strings.ToLower(strings.TrimLeft(url, " \t\r\n"))
	if strings.HasPrefix(lower, "data:image/") && !strings.HasPrefix(lower, "data:image/svg") {
		return url
	}
	if isDangerousURLScheme(url) {
		return "#"
	}
	return url
}

// stripURLControlBytes removes the tab/LF/CR/NUL bytes that the HTML5
// URL parser deletes from a URL before resolving its scheme. Browsers
// ignore them anywhere in the URL, including in the MIDDLE of a scheme
// name, so `java\tscript:` resolves to `javascript:`. We delete them up
// front so both the scheme allow-list and the stored href see the same
// string the browser will execute, closing the interior-control-byte
// bypass.
func stripURLControlBytes(url string) string {
	if !strings.ContainsAny(url, "\t\n\r\x00") {
		return url
	}
	var sb strings.Builder
	sb.Grow(len(url))
	for i := 0; i < len(url); i++ {
		switch url[i] {
		case '\t', '\n', '\r', '\x00':
			// dropped, mirrors the browser URL parser
		default:
			sb.WriteByte(url[i])
		}
	}
	return sb.String()
}

// isDangerousURLScheme reports whether url begins with a URL scheme
// known to execute script or render HTML in a navigation context.
// Leading ASCII whitespace and control chars are ignored. They're
// stripped from the scheme by the HTML parser anyway, so we match the
// parser's view. Callers should run stripURLControlBytes first so that
// control bytes embedded INSIDE the scheme (java\tscript:) are also
// neutralised.
func isDangerousURLScheme(url string) bool {
	trimmed := stripURLControlBytes(url)
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
