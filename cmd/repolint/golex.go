package main

import "strings"

// goLexState is the lexical context that survives a newline in Go source.
type goLexState struct {
	inBlockComment bool
	inRawString    bool
}

// outsideGoStrings drops string and rune literal contents from a line
// stripGoComments already cleared of comments. inRaw says the line opens
// inside a raw string.
func outsideGoStrings(code string, inRaw bool) string {
	var out strings.Builder
	var quote byte
	if inRaw {
		quote = '`'
	}
	for i := 0; i < len(code); i++ {
		c := code[i]
		if quote != 0 {
			if c == '\\' && quote != '`' {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '"' || c == '\'' || c == '`' {
			quote = c
			continue
		}
		out.WriteByte(c)
	}
	return out.String()
}

// stripGoComments returns the code on one line of Go source with both
// comment forms removed, plus the lexical state the next line starts in.
func stripGoComments(line string, st goLexState) (string, goLexState) {
	var code strings.Builder
	var quote byte
	if st.inRawString {
		quote = '`'
	}
	for i := 0; i < len(line); i++ {
		c := line[i]
		if st.inBlockComment {
			if c == '*' && i+1 < len(line) && line[i+1] == '/' {
				st.inBlockComment = false
				i++
			}
			continue
		}
		if quote != 0 {
			code.WriteByte(c)
			if c == '\\' && quote != '`' {
				if i+1 < len(line) {
					code.WriteByte(line[i+1])
				}
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '"' || c == '\'' || c == '`' {
			quote = c
			code.WriteByte(c)
			continue
		}
		if c == '/' && i+1 < len(line) {
			if line[i+1] == '/' {
				st.inRawString = false
				return code.String(), st
			}
			if line[i+1] == '*' {
				st.inBlockComment = true
				i++
				continue
			}
		}
		code.WriteByte(c)
	}
	st.inRawString = quote == '`'
	return code.String(), st
}
