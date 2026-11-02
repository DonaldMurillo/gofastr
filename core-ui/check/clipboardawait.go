package check

import "regexp"

// LintClipboardAwait fires when a clipboard write's promise is dropped
// and the same function goes on to tell the reader the copy happened.
//
// Bug class: the copy behaviour called
// navigator.clipboard.writeText(text).catch(() => {}) and then, in the
// same click handler, set the Copied label, the copy state, the status
// sentence and the toast (e2988bd2). writeText is asynchronous and can
// refuse (a declined permission prompt, an insecure origin, a document
// without focus), so the page announced a copy that never happened and
// a refusal was swallowed. The fix spelling keeps the promise and says
// the outcome from it:
//
//	const write = navigator.clipboard.writeText(text);
//	write.then(() => copied(wrap), () => failed(wrap));
//
// A write is dropped when its result is neither awaited, returned,
// assigned, passed on, nor chained with .then: an expression statement,
// with or without a .catch that swallows the refusal. It counts when a
// later statement of the same function writes what a reader sees or
// hears: textContent, innerText or innerHTML, a setAttribute, a toast,
// an announce or say call.
//
// Silent on:
//   - `await clip.writeText(t)` and a write chained with .then: the
//     outcome is in hand before anything is said;
//   - a write whose promise is kept (`const p = …writeText(t)`,
//     `return …`, an argument): the caller owns the outcome;
//   - a dropped write with nothing said after it: a silent
//     fire-and-forget copy announces nothing false;
//   - lines a //gofastr:allow(clipboardawait) <reason> marker covers.
func LintClipboardAwait(roots ...string) (*Result, error) {
	res := &Result{}
	files, err := loadJSSources(roots...)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		allowed := allowedLines(jsComments(f), "clipboardawait")
		for _, loc := range reClipboardWrite.FindAllStringIndex(f.Blank, -1) {
			open := loc[1] - 1
			close := matchDelimForward(f.Blank, open)
			if close < 0 || !writeDropped(f.Blank, loc[0], close) {
				continue
			}
			_, fnEnd := enclosingFunction(f.Blank, close)
			stmtEnd := statementEnd(f.Blank, close+1)
			if fnEnd <= stmtEnd || !reReaderWrite.MatchString(f.Blank[stmtEnd:fnEnd]) {
				continue
			}
			line := f.lineOf(loc[0])
			if allowed[line] {
				continue
			}
			res.add(f.Path, line, "[clipboard-await] the clipboard write's promise is dropped and the code after it tells the reader the copy happened — writeText resolves later and can refuse (a declined permission, an insecure origin), so the page announces a copy that never happened; keep the promise and announce from .then(ok, failed), or await it")
		}
	}
	return res, nil
}

// reClipboardWrite matches writeText( and a clipboard's write( on the
// blank view.
var reClipboardWrite = regexp.MustCompile(`\.\s*writeText\s*\(|\bclipboard\s*\.\s*write\s*\(`)

// reReaderWrite matches what puts words in front of a reader.
var reReaderWrite = regexp.MustCompile(`\.\s*(?:textContent|innerText|innerHTML)\s*=[^=]|\.\s*setAttribute\s*\(|\b(?:toast|announce|say)\s*\(`)

// writeDropped reports whether the call spanning blank[start:close+1]
// is an expression statement whose promise nobody keeps: the receiver
// chain starts a statement, and after the call comes only an optional
// .catch(...) before the statement ends.
func writeDropped(blank string, start, close int) bool {
	// Walk back over the receiver chain (navigator.clipboard, clip).
	// A keyword ahead of the chain is walked like a name: `if (c)`,
	// `else` and `void` still start a statement that drops the value,
	// while await and return keep it.
	i := start
	for {
		j := skipSpaceBack(blank, i-1)
		if j < 0 {
			break
		}
		if isJSIdentChar(blank[j]) {
			end := j + 1
			for j >= 0 && isJSIdentChar(blank[j]) {
				j--
			}
			if w := blank[j+1 : end]; w == "await" || w == "return" || w == "yield" {
				return false
			}
			i = j + 1
			continue
		}
		if blank[j] == '.' || (blank[j] == '?' && j+1 < len(blank) && blank[j+1] == '.') {
			i = j
			continue
		}
		if blank[j] == ')' || blank[j] == ']' {
			if k := matchDelimBack(blank, j); k >= 0 {
				i = k
				continue
			}
		}
		// Only a statement boundary drops the value. An assignment, an
		// argument, a ternary arm or an arrow's expression body keeps it.
		if blank[j] != ';' && blank[j] != '{' && blank[j] != '}' {
			return false
		}
		break
	}
	// Forward: optional .catch(...) chains, then the statement's end.
	k := close + 1
	for {
		k = skipSpace(blank, k)
		if m := reCatchAt.FindStringIndex(blank[k:]); m != nil {
			c := matchDelimForward(blank, k+m[1]-1)
			if c < 0 {
				return false
			}
			k = c + 1
			continue
		}
		break
	}
	return k >= len(blank) || blank[k] == ';' || blank[k] == '}' || blank[k] == '\n'
}

var reCatchAt = regexp.MustCompile(`^\??\.\s*catch\s*\(`)
