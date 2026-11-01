package check

import (
	"fmt"
	"regexp"
	"strings"
)

// LintRuntimeProse fires when English prose written in a JS string
// literal reaches a reader: a toast (NS.toast, NS._toastOrFallback,
// NS._fallbackToast, _showNavToast), a text node (createTextNode), an
// element's textContent, innerText, title, placeholder, ariaLabel or
// alt, or a label attribute set through setAttribute (aria-label,
// aria-description, title, placeholder, alt).
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
	reToastSink = regexp.MustCompile(`\b(?:toast|_toastOrFallback|_fallbackToast|_showNavToast|createTextNode)\s*(?:\?\.\s*)?\(`)
	reTextSink  = regexp.MustCompile(`\.(?:textContent|innerText|title|placeholder|ariaLabel|alt)\s*\+?=[^=]`)
	reAttrSink  = regexp.MustCompile(`\bsetAttribute\s*\(`)
	reLabelAttr = regexp.MustCompile(`^\s*['"](?:aria-label|aria-description|title|placeholder|alt)['"]\s*,`)
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
	return spans
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
