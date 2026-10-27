package acp

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestEmptyRequiredFieldsKept: the ACP schema requires "text" on a text
// block, "uri" and "name" on a resource link, "path" and "newText" on a
// diff and "terminalId" on a terminal item. omitempty dropped them when
// empty, so an empty tool output or a diff that clears a file was a
// frame a strict client refuses.
func TestEmptyRequiredFieldsKept(t *testing.T) {
	cases := []struct {
		v    any
		want []string
	}{
		{TextToolContent(""), []string{`"text":""`}},
		{ContentBlock{Type: ContentResourceLink}, []string{`"uri":""`, `"name":""`}},
		{ToolCallContent{Type: "diff"}, []string{`"path":""`, `"newText":""`}},
		{ToolCallContent{Type: "terminal"}, []string{`"terminalId":""`}},
	}
	for _, c := range cases {
		b, err := json.Marshal(c.v)
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range c.want {
			if !strings.Contains(string(b), w) {
				t.Errorf("%s lacks %s", b, w)
			}
		}
	}
	// The required set is per type: a text block carries no link fields.
	if b, _ := json.Marshal(ContentBlock{Type: ContentText, Text: "hi"}); string(b) != `{"type":"text","text":"hi"}` {
		t.Errorf("text block = %s", b)
	}
}
