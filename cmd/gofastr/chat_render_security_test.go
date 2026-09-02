package main

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// Property: model output rendered into the harness chat page cannot become
// markup. renderChat embeds the bearer token in a meta tag, and chatJS pipes
// every assistant/tool text through renderMarkdown into innerHTML — the one
// sink where a hostile tool result or fetched-document body (the a2a/webfetch
// threat model) could otherwise execute script in the token-bearing page.
//
// renderMarkdown escapes (&<>") BEFORE any formatting, and the autolinker
// only wraps bare http(s) URLs. The escaping order is the whole defence, so
// this test runs the REAL shipped functions (extracted from chatJS) under
// node and asserts the payloads stay inert at every formatting stage:
// element markup, script markup, markdown-link scheme smuggling, and
// attribute break-out through an autolinked URL.
func TestChatMarkdownKeepsModelOutputInert(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available; TestChatJSParsesAsValidJS carries the same environment gate")
	}

	start := strings.Index(chatJS, "function esc(")
	end := strings.Index(chatJS, "function makeCopyBtn(")
	if start < 0 || end < 0 || end <= start {
		// chatJS lost or renamed its escaper: the sink this test pins would
		// then be defended (or not) by code this file has never seen.
		t.Fatalf("could not extract esc/renderMarkdown from chatJS (start=%d end=%d)", start, end)
	}
	chunk := chatJS[start:end]

	payloads := []string{
		`<img src=x onerror=PWN()>`,
		`<script>PWN()</script>`,
		`[click me](javascript:PWN())`,
		`https://ok.test/a" onmouseover="PWN()`,
		"```html\n<img src=x onerror=PWN()>\n```",
	}
	script := `
const chunk = ` + mustChatJSON(t, chunk) + `;
eval(chunk);
const payloads = ` + mustChatJSON(t, payloads) + `;
// Every tag the renderer itself is allowed to emit.
const allowed = /<\/?(a|code|pre|strong|em|h[1-6]|ul|li|p|br)(\s[^>]*)?>/g;
const problems = [];
for (const p of payloads) {
  const out = renderMarkdown(p);
  // Strip the renderer's own tags; whatever '<' remains came from the
  // payload unescaped.
  const residual = out.replace(allowed, '');
  if (residual.includes('<')) problems.push({payload: p, out, why: 'unescaped < from payload'});
  for (const href of out.matchAll(/href="([^"]*)"/g)) {
    if (!/^https?:\/\//.test(href[1])) problems.push({payload: p, out, why: 'href not http(s): ' + href[1]});
  }
}
if (problems.length) { console.error(JSON.stringify(problems)); process.exit(1); }
console.error('OK');
`
	cmd := exec.Command(node, "-e", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("SECURITY: [chat-xss] model output reached markup in the token-bearing chat page: %s\n%s", err, out)
	}
	if !strings.Contains(string(out), "OK") {
		t.Fatalf("node harness did not report success: %s", out)
	}
}

func mustChatJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
