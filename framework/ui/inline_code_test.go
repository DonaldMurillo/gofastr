package ui

import (
	"strings"
	"testing"
)

func TestInlineCodeEscapesText(t *testing.T) {
	out := string(InlineCode(`a < "b"`))
	for _, want := range []string{`<code`, `data-cui-comp="ui-code"`, `a &lt; &quot;b&quot;`} {
		if !strings.Contains(out, want) {
			t.Errorf("InlineCode is missing %q: %s", want, out)
		}
	}
}

func TestInlineCodeDangerTone(t *testing.T) {
	out := string(InlineCodeDanger(`smtp: 451 <greylisted>`))
	if !strings.Contains(out, `class="fui-code fui-code--danger"`) || !strings.Contains(out, "&lt;greylisted&gt;") {
		t.Errorf("InlineCodeDanger: %s", out)
	}
}
