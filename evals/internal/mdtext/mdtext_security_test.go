package mdtext

import (
	"strings"
	"testing"
)

// Property: text produced by the thing being evaluated must not reach a
// committed markdown report with live markdown syntax in it. The two
// RESULTS.md writers (backend-adoption's RenderMarkdown, ui-quality's
// leaderboardMarkdown) route every candidate-controlled field through
// Escape; this is the shared pin of that one table.
func TestEscapeNeutralisesEvalOutputText(t *testing.T) {
	hostile := []string{
		"![exfil](https://eval-attacker.example/ping)",
		"# Forged heading",
		"[link](https://eval-attacker.example)",
		"`code` *emphasis* _underline_",
		"line one\nline two\r\nline three",
		"<script>alert(1)</script>",
	}
	for _, text := range hostile {
		escaped := Escape(text)
		if strings.Contains(escaped, "![") || strings.Contains(escaped, "](http") {
			t.Errorf("SECURITY: [markdown-injection] %q escaped to %q; a live image/link reference survives", text, escaped)
		}
		if strings.Contains(escaped, "\n") || strings.Contains(escaped, "\r") {
			t.Errorf("SECURITY: [markdown-injection] %q escaped to %q; embedded newlines add lines to the report", text, escaped)
		}
		for _, structure := range []string{"# ", "[", "]", "*", "`", "<", ">"} {
			if strings.HasPrefix(escaped, structure) {
				t.Errorf("SECURITY: [markdown-injection] %q escaped to %q; a leading %q is live structure", text, escaped, structure)
			}
		}
	}
}
