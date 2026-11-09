package uihost

import (
	"encoding/json"
	"strings"
	"testing"
)

// #669 (was #659): the A2A AgentSkill schema marks description and tags
// as REQUIRED, but AgentSkill carried omitempty on both, so a skill
// with no description and no tags serialized without the keys and the
// card failed schema validation downstream. Both keys are always
// present now; an empty tags list serializes as [], never null.
func TestAgentSkillAlwaysEmitsRequiredKeys(t *testing.T) {
	body, err := json.Marshal(AgentSkill{ID: "s1", Name: "Draft"})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["description"]; !ok {
		t.Errorf("SECURITY: [a2a-schema] description key missing from %s", body)
	}
	tags, ok := m["tags"]
	if !ok {
		t.Errorf("SECURITY: [a2a-schema] tags key missing from %s", body)
	} else if _, isList := tags.([]any); !isList {
		t.Errorf("tags = %T (%s), want a JSON array", tags, body)
	}
	if strings.Contains(string(body), "null") {
		t.Errorf("skill serialized a null: %s", body)
	}
}
