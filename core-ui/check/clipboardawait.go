package check

import "regexp"

// LintClipboardAwait fires when a clipboard write's promise is dropped and the same function goes
// on to tell the reader the copy happened.
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

var reClipboardWrite = regexp.MustCompile(`\.\s*writeText\s*\(|\bclipboard\s*\.\s*write\s*\(`)

var reReaderWrite = regexp.MustCompile(`\.\s*(?:textContent|innerText|innerHTML)\s*=[^=]|\.\s*setAttribute\s*\(|\b(?:toast|announce|say)\s*\(`)

func writeDropped(blank string, start, close int) bool {
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
		if blank[j] != ';' && blank[j] != '{' && blank[j] != '}' {
			return false
		}
		break
	}
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
