// Package jstest holds the comment-stripping scan the embedded-JS source
// pins share (core/mcp's widget client, framework/pluginhost's broker and
// frame client). Security assertions on embedded .js must run against
// executable code, never prose: the header comment states the same
// invariants in the same words, so a whole-file Contains would stay green
// with the check deleted.
package jstest

import "strings"

// NonCommentJS strips whole-line and trailing // comments, returning the
// executable lines joined, so a pin can't be satisfied by prose.
func NonCommentJS(js string) string {
	var b strings.Builder
	for line := range strings.SplitSeq(js, "\n") {
		code := strings.TrimSpace(line)
		if strings.HasPrefix(code, "//") || strings.HasPrefix(code, "*") ||
			strings.HasPrefix(code, "/*") {
			continue
		}
		if idx := strings.Index(code, "//"); idx >= 0 {
			code = strings.TrimSpace(code[:idx])
		}
		b.WriteString(code)
		b.WriteString("\n")
	}
	return b.String()
}
