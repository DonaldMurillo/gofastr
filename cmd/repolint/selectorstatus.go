package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// testOperandPrefix matches the text in front of a command substitution
// that is the operand of a -n/-z test: `[ -n "`, `[[ -z `, `test -n "`,
// with a `!` before the operator or not.
// Inside a test the command substitution's exit status is lost, so a
// selector that fails prints nothing and reads as "nothing affected": the
// step skips a suite where it should fail. Assigning the output on its own
// line keeps the status, and the step's `bash -e` stops on it.
var testOperandPrefix = regexp.MustCompile(`(?:\[\[?|(?:^|[\s;&|(!])test)\s+(?:!\s+)?-[nz]\s+"?$`)

const affectedTestedMessage = "tests a cmd/affected command substitution inside [ ], [[ ]] or test: the selector's exit status is lost, so a failure reads as nothing affected and the suite skips; assign the output on its own line, then test the variable"

// lintSelectorStatus applies the selector-status rule to the workflow
// files, which lintRepo's walk skips with the other dot directories.
// Build scripts get the same rule from lintBytes.
func lintSelectorStatus(root string) ([]finding, error) {
	paths, err := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.y*ml"))
	if err != nil {
		return nil, err
	}
	var out []finding
	for _, p := range paths {
		body, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			rel = p
		}
		out = append(out, selectorStatusFindings(filepath.ToSlash(rel), body)...)
	}
	return out, nil
}

func selectorStatusFindings(rel string, body []byte) []finding {
	var out []finding
	text := string(body)
	flagged := map[int]bool{}
	for start := 0; ; {
		i := strings.Index(text[start:], "$(")
		if i < 0 {
			break
		}
		i += start
		start = i + 2
		// The span runs to the ")" that closes it: nested parentheses, an
		// inner $(dirname "$0") and a quoted ")" do not end it, and it may
		// cross lines.
		span := text[i+2:]
		span = span[:substitutionEnd(span)]
		if !strings.Contains(span, "cmd/affected") {
			continue
		}
		// The test may start on an earlier line joined by a trailing
		// backslash (`if [ -n \` then the substitution).
		lineStart := strings.LastIndexByte(text[:i], '\n') + 1
		for lineStart >= 2 && text[lineStart-2] == '\\' {
			lineStart = strings.LastIndexByte(text[:lineStart-2], '\n') + 1
		}
		prefix := strings.ReplaceAll(text[lineStart:i], "\\\n", " ")
		if !testOperandPrefix.MatchString(prefix) {
			continue
		}
		line := strings.Count(text[:lineStart], "\n") + 1
		if flagged[line] {
			continue
		}
		flagged[line] = true
		out = append(out, finding{
			File:    rel,
			Line:    line,
			Rule:    "selector-status-discarded",
			Message: affectedTestedMessage,
		})
	}
	return out
}

// substitutionEnd returns the index in s of the ")" that closes a command
// substitution whose "$(" was just read, or len(s) when none does. It
// reads the shell's quoting: a ")" inside '…' or "…", or after a
// backslash, is text, and a "$(" inside either nesting level opens its
// own substitution.
func substitutionEnd(s string) int {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '\'':
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				return len(s)
			}
			i += j + 1
		case '"':
			i = doubleQuoteEnd(s, i+1)
		case '$':
			if i+1 < len(s) && s[i+1] == '(' {
				i += 2 + substitutionEnd(s[i+2:])
			}
		case '(':
			depth++
		case ')':
			if depth == 0 {
				return i
			}
			depth--
		}
	}
	return len(s)
}

// doubleQuoteEnd returns the index of the '"' closing a double-quoted
// string that opens just before s[i], or len(s).
func doubleQuoteEnd(s string, i int) int {
	for ; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '"':
			return i
		case '$':
			if i+1 < len(s) && s[i+1] == '(' {
				i += 2 + substitutionEnd(s[i+2:])
			}
		}
	}
	return len(s)
}
