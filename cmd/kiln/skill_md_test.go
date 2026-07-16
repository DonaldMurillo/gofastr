package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// The skill is injected into every agent turn; a malformed example teaches
// agents to send invalid payloads. Gate every fenced json block.
func TestSkillJSONExamplesParse(t *testing.T) {
	blocks := strings.Split(kilnSkillContent, "```json")
	if len(blocks) < 2 {
		t.Fatal("skill.md lost its json examples")
	}
	for i, rest := range blocks[1:] {
		body, _, ok := strings.Cut(rest, "```")
		if !ok {
			t.Fatalf("json block %d is unterminated", i+1)
		}
		var v any
		if err := json.Unmarshal([]byte(body), &v); err != nil {
			t.Errorf("json block %d does not parse: %v\n%s", i+1, err, body)
		}
	}
}
