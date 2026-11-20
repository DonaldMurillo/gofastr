package markdown

import (
	"strings"
)

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
		if key != "" {
			if _, dup := fm[key]; !dup {
				fm[key] = val
			}
		}
	}
	body := strings.Join(lines[end+1:], "\n")
	return fm, body
}

func isFrontmatterContinuation(line string) bool {
	return strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") ||
		line == "-" || strings.HasPrefix(line, "- ")
}

// Unquote strips one layer of matching double or single quotes from s.
func Unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
