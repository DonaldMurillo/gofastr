package markdown

import (
	"strings"
)

// splitFrontmatter detects a leading YAML-ish frontmatter block (delimited
// by --- on lines of their own) and returns the parsed key/value map plus
// the remaining body. If no frontmatter is present, the map is nil and the
// body is the original input.
//
// We deliberately do not parse full YAML — only `key: value` pairs at the
// top level. Quoted strings are unwrapped; everything else is kept verbatim.
// Lists, nested maps, and multi-line strings are not supported; if you need
// them, write a JSON sidecar or wrap goldmark instead.
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
	fm := make(map[string]string)
	for _, line := range lines[1:end] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		idx := strings.Index(line, ":")
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		val = unquote(val)
		if key != "" {
			fm[key] = val
		}
	}
	body := strings.Join(lines[end+1:], "\n")
	return fm, body
}

func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
