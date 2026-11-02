package check

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// LintRuntimeProse fires when English prose written in a JS string
// literal reaches a reader: a toast (NS.toast, NS._toastOrFallback,
// NS._fallbackToast, _showNavToast), the browser's own dialogs (alert,
// confirm, prompt), a text node (createTextNode, or a literal argument
// of append, prepend, replaceChildren, before, after or replaceWith,
// which make one; an append(name, value) whose first literal is a bare
// field name is the Headers / FormData / URLSearchParams method and
// stays quiet), an element's textContent, innerText, title,
// placeholder, alt, nodeValue or ARIA text property (ariaLabel,
// ariaDescription, ariaValueText, ariaRoleDescription,
// ariaPlaceholder), or a label attribute set through setAttribute
// (aria-label, aria-description, aria-valuetext, aria-roledescription,
// aria-placeholder, title, placeholder, alt). The dialogs, the
// text-node methods, the last three ARIA attributes and nodeValue
// arrived with the 2026-11-02 guard bash: each put English in front of
// a reader with the lint silent.
//
// Bug class: the runtime's own words never reached a catalog. A page
// rendered in Spanish still toasted "Request failed" and labelled the
// fallback notice "Dismiss notification", because the words lived in
// the JS where no translator looks. The fix spelling is say('key',
// 'English', params): the kernel reads the reader's translation from
// the host's #gofastr-strings block and falls back to the English
// beside the key, and TestRuntimeSayCallsMatchPhrases (core-ui/runtime)
// holds each call to a phrase registered with runtime.RegisterStrings.
//
// Prose is a literal holding two words, or one capitalised word
// ("Notifications"): a variant name ('error'), a key, a selector or a
// class stays quiet. The English argument of a say() call is the
// sanctioned spelling and stays quiet, as does any literal on a line a
// //gofastr:allow(runtimeprose) <reason> marker covers (trailing the
// line, or alone on the line above; see allowmarker.go): developer-facing
// text (a plugin host's diagnostics, a dev overlay) is not the reader's
// language.
func LintRuntimeProse(roots ...string) (*Result, error) {
	res := &Result{}
	files, err := loadJSSources(roots...)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		allowed := allowedLines(jsComments(f), "runtimeprose")
		for _, span := range proseSinkSpans(f) {
			for _, lit := range jsStringLiterals(f.Code[span[0]:span[1]]) {
				pos := span[0] + lit.pos
				if !(isReaderProse(lit.text) || gluedWord(f, span[0], pos, lit.text)) || saysEnglish(f.Code, pos) {
					continue
				}
				line := f.lineOf(pos)
				if allowed[line] {
					continue
				}
				text := lit.text
				if len(text) > 60 {
					text = text[:57] + "..."
				}
				res.add(f.Path, line, fmt.Sprintf("[runtime-prose] %q reaches the reader as written, in English on every locale — say it through say('<pkg>.<code>', %q) (NS.say in a module) and register the phrase with runtime.RegisterStrings, or mark developer-facing text //gofastr:allow(runtimeprose) <reason>", text, text))
			}
		}
	}
	return res, nil
}

// Sinks, matched on the blank view so a sink spelled in a comment or a
// string never counts. A toast call matches its optional-call spelling
// too (NS._toastOrFallback?.(...)): the lint missed rpc.js's 'Done'
// success toast behind one. A text sink matches += as well as =, and a
// lone capitalised word may end in a colon: signals.js wrote 'Error: '
// past both.
var (
	reToastSink = regexp.MustCompile(`\b(?:toast|_toastOrFallback|_fallbackToast|_showNavToast|createTextNode|alert|confirm|prompt)\s*(?:\?\.\s*)?\(`)
	reTextSink  = regexp.MustCompile(`\.(?:textContent|innerText|title|placeholder|ariaLabel|ariaDescription|ariaValueText|ariaRoleDescription|ariaPlaceholder|alt|nodeValue)\s*\+?=[^=]`)
	reAttrSink  = regexp.MustCompile(`\bsetAttribute\s*\(`)
	reLabelAttr = regexp.MustCompile(`^\s*['"](?:aria-label|aria-description|aria-valuetext|aria-roledescription|aria-placeholder|title|placeholder|alt)['"]\s*,`)
	// A string argument of these DOM methods becomes a text node, the
	// way createTextNode's does. Only a literal argument itself counts:
	// a nested call's arguments (append(make('div', …, 'x'))) are that
	// call's business.
	reNodeSink  = regexp.MustCompile(`\.(?:append|prepend|replaceChildren|before|after|replaceWith)\s*\(`)
	reSaidAs    = regexp.MustCompile(`\bsay\(\s*(['"])[^'"]*(['"])\s*,\s*$`)
	reTwoWords  = regexp.MustCompile(`[A-Za-z]{2,}[,.:;!?)]*(?:\s+\S+)*?\s+\S*[A-Za-z]{2,}`)
	reCapWord   = regexp.MustCompile(`^[A-Z][a-z]{2,}[.!?:]?$`)
	reInterp    = regexp.MustCompile(`\$\{[^}]*\}`)
	reLowerWord = regexp.MustCompile(`[a-z]{2,}`)
)

// proseSinkSpans returns the byte spans, in f's views, of every
// expression that reaches a reader: a toast call's arguments, the right
// side of a textContent assignment up to its statement's end, and a
// label setAttribute's value.
func proseSinkSpans(f jsSource) [][2]int {
	var spans [][2]int
	for _, loc := range reToastSink.FindAllStringIndex(f.Blank, -1) {
		open := loc[1] - 1
		if close := matchDelimForward(f.Blank, open); close > open {
			spans = append(spans, [2]int{open + 1, close})
		}
	}
	for _, loc := range reTextSink.FindAllStringIndex(f.Blank, -1) {
		spans = append(spans, [2]int{loc[1] - 1, exprEnd(f.Blank, loc[1]-1)})
	}
	for _, loc := range reAttrSink.FindAllStringIndex(f.Blank, -1) {
		open := loc[1] - 1
		close := matchDelimForward(f.Blank, open)
		if close <= open || !reLabelAttr.MatchString(f.Code[open+1:close]) {
			continue
		}
		if comma := topLevelComma(f.Blank, open+1, close); comma >= 0 {
			spans = append(spans, [2]int{comma + 1, close})
		}
	}
	for _, loc := range reNodeSink.FindAllStringIndex(f.Blank, -1) {
		open := loc[1] - 1
		close := matchDelimForward(f.Blank, open)
		if close <= open {
			continue
		}
		var args [][2]int
		for from := open + 1; from < close; {
			end := topLevelComma(f.Blank, from, close)
			if end < 0 {
				end = close
			}
			args = append(args, [2]int{from, end})
			from = end + 1
		}
		if strings.HasPrefix(f.Blank[loc[0]:], ".append") && fieldPairAppend(f.Code, args) {
			continue
		}
		for _, a := range args {
			if isStringLiteral(f.Code[a[0]:a[1]]) {
				spans = append(spans, a)
			}
		}
	}
	// Source order, so findings read top to bottom whichever sink
	// family found them.
	slices.SortFunc(spans, func(a, b [2]int) int { return a[0] - b[0] })
	return spans
}

// isStringLiteral reports whether a call argument is a string literal
// (quoted or a template).
func isStringLiteral(arg string) bool {
	arg = strings.TrimSpace(arg)
	return arg != "" && strings.ContainsRune("'\"`", rune(arg[0]))
}

// fieldPairAppend reports whether an append call's arguments read as
// Headers / FormData / URLSearchParams append(name, value[, filename]):
// two or three arguments, the first a literal field name with no
// whitespace in it ('Accept', 'Title'). Those appends add a header, a
// form field or a query parameter, not a text node, and the round-3
// review found them firing ("Accept" for h.append('Accept',
// 'text/event-stream')). A DOM append whose first literal is prose
// ('Last saved: ') or whose literal follows a node still reads as one.
func fieldPairAppend(code string, args [][2]int) bool {
	if len(args) < 2 || len(args) > 3 {
		return false
	}
	first := strings.TrimSpace(code[args[0][0]:args[0][1]])
	if !isStringLiteral(first) || len(first) < 2 {
		return false
	}
	name := first[1 : len(first)-1]
	return name != "" && !strings.ContainsAny(name, " \t\n")
}

// exprEnd is the offset of the first depth-zero ';' or closing
// delimiter at or after from, or the end of the source. Unlike
// statementEnd it reads across newlines, so a ternary split over lines
// stays one expression.
func exprEnd(blank string, from int) int {
	depth := 0
	for i := from; i < len(blank); i++ {
		switch blank[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth == 0 {
				return i
			}
			depth--
		case ';':
			if depth == 0 {
				return i
			}
		}
	}
	return len(blank)
}

// isReaderProse reports whether a literal reads as words for a person.
func isReaderProse(s string) bool {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "${") {
		// A template's text around an interpolation is prose when it
		// holds words of its own.
		s = reInterp.ReplaceAllString(s, " ")
	}
	return reTwoWords.MatchString(s) || reCapWord.MatchString(s)
}

// gluedWord reports whether the literal opening at pos is an operand of +
// and holds a word: a unit or noun glued to a value, as in
// n + ' chars' or ms + 'ms', reads English on every locale though no
// two-word or capitalised test sees it. Only at the sink's own level: inside
// a call in the span (getAttribute(P + 'fail')) the literal builds a name,
// not text.
func gluedWord(f jsSource, spanStart, pos int, text string) bool {
	if !reLowerWord.MatchString(text) {
		return false
	}
	depth := 0
	for _, c := range f.Blank[spanStart:pos] {
		switch c {
		case '(':
			depth++
		case ')':
			depth--
		}
	}
	if depth > 0 {
		return false
	}
	code := f.Code
	before := strings.TrimRight(code[:pos], " \t\r\n")
	if strings.HasSuffix(before, "+") && !strings.HasSuffix(before, "++") {
		return true
	}
	end := literalEnd(code, pos)
	after := strings.TrimLeft(code[end:], " \t\r\n")
	return strings.HasPrefix(after, "+") && !strings.HasPrefix(after, "+=") && !strings.HasPrefix(after, "++")
}

// literalEnd is the index just past the string literal opening at pos.
func literalEnd(code string, pos int) int {
	q := code[pos]
	for j := pos + 1; j < len(code); j++ {
		switch code[j] {
		case '\\':
			j++
		case q:
			return j + 1
		}
	}
	return len(code)
}

// saysEnglish reports whether the literal opening at pos is the English
// argument of a say() call.
func saysEnglish(code string, pos int) bool {
	return reSaidAs.MatchString(code[max(0, pos-120):pos])
}
