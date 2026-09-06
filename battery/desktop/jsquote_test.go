package desktop

import (
	"strings"
	"testing"
)

// A non-printable astral rune (U+E0001, a TAG character) is the case
// that separates Go's %q / strconv.Quote from a JavaScript string
// literal: Go emits \U000E0001, which JavaScript does not parse. Every
// string the battery splices into evaluated or served JavaScript must
// therefore be JSON-quoted, never Go-quoted.
func TestJSStringsNeverUseGoUnicodeEscapes(t *testing.T) {
	const tag = "x\U000E0001y"

	b, _ := newTestBattery(t)
	win := &fakeWindow{}
	b.windowMu.Lock()
	b.window = win
	b.windowMu.Unlock()
	if err := b.Emit("evt", map[string]any{"text": tag}); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	evals := win.evals()
	if len(evals) == 0 {
		t.Fatal("Emit recorded no eval")
	}
	if js := evals[len(evals)-1]; strings.Contains(js, `\U`) {
		t.Fatalf("Emit produced a Go-only \\U escape, invalid in JavaScript:\n%s", js)
	}

	m := Manifest{Schema: 1, Capabilities: []CapabilityInfo{{
		Name: "demo", Version: 1, Description: tag,
		Methods: []MethodInfo{{Name: "ping", Description: tag}},
	}}}
	if js := string(BridgeJS(m)); strings.Contains(js, `\U`) {
		t.Fatalf("BridgeJS produced a Go-only \\U escape, invalid in JavaScript:\n%s", js)
	}
}
