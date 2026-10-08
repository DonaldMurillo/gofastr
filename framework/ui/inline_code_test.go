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
