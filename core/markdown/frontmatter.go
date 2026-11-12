package markdown

import (
	"strings"
)

// splitFrontmatter detects a leading YAML-ish frontmatter block (delimited
// by --- on lines of their own) and returns the parsed key/value map plus
// the remaining body. If no frontmatter is present, the map is nil and the
// body is the original input.
//
// We deliberately do not parse full YAML, only `key: value` pairs at the
// top level. Quoted strings are unwrapped; everything else is kept verbatim.
// Lists, nested maps, and multi-line strings are not supported; if you need
// them, write a JSON sidecar or wrap goldmark instead. Their lines (an
// indented line, or a `- item`, after a key) are skipped as the key's
// continuation, so the key reads as empty.
//
// A closed leading `---` pair is frontmatter only when every non-blank
// line between the two is a `key: value` pair with a non-blank key, a `#`
// comment or such a continuation. A document that opens with a thematic break, then prose,
// then another `---` is body: reading it as frontmatter dropped the prose
// without a word.
func splitFrontmatter(input string) (map[string]string, string) {
	lines := splitLines(input)
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil, input
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return nil, input
	}
	sawKey := false
	for _, line := range lines[1:end] {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if sawKey && isFrontmatterContinuation(line) {
			continue
		}
		// A pair needs a key: ` : x` has none, and reading the block as
		// frontmatter would drop that line and the text with it.
		if k, _, ok := strings.Cut(line, ":"); !ok || strings.TrimSpace(k) == "" {
			return nil, input
		}
		sawKey = true
	}
	fm := make(map[string]string)
	sawKey = false
	for _, line := range lines[1:end] {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if sawKey && isFrontmatterContinuation(line) {
			continue
		}
		sawKey = true
		idx := strings.Index(line, ":")
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		val = Unquote(val)
		// A key defined twice must not silently resolve to the LAST
		// value: a stale or hostile line lower in the block would
		// override the value a reviewer read first, and Title flows
		// into <title>, SEO meta, and templated page chrome. There is
		// no error channel on Render, so the fail-closed minimum is
		// first-wins — ambiguity never resolves to the line a
		// top-to-bottom reader did not see.
		if key != "" {
			if _, dup := fm[key]; !dup {
				fm[key] = val
			}
		}
	}
	body := strings.Join(lines[end+1:], "\n")
	return fm, body
}

// isFrontmatterContinuation reports whether a frontmatter line continues
// the key above it: an indented line (a nested map, a list item, a folded
// string) or an unindented `- item` YAML also accepts under a key.
func isFrontmatterContinuation(line string) bool {
	return strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") ||
		line == "-" || strings.HasPrefix(line, "- ")
}

// Unquote strips one layer of matching surrounding quotes (double or
// single) from a YAML frontmatter scalar, returning s unchanged when
// it is not quoted (or is too short to be). Exported for the skill
// frontmatter parser, which formerly carried a byte-identical
// trimYAMLQuotes copy.
func Unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
