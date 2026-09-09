package provider

import (
	"encoding/json"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
)

func TestEstimateTokens(t *testing.T) {
	if got := EstimateTokens(nil); got != 0 {
		t.Fatalf("empty: got %d, want 0", got)
	}
	msgs := []Message{
		{Role: RoleUser, Content: []control.ContentBlock{{Type: "text", Text: "12345678"}}}, // 8 chars
		{Role: RoleAssistant, Content: []control.ContentBlock{{
			Type:    "tool_use",
			ToolUse: &control.ToolUse{ID: "t1", Name: "grep", Input: json.RawMessage(`{"q":1}`)}, // 4 + 7
		}}},
		{Role: RoleUser, Content: []control.ContentBlock{{
			Type:       "tool_result",
			ToolResult: &control.ToolResultBlk{ToolUseID: "t1", Content: []control.ContentBlock{{Text: "abcde"}}}, // 5
		}}},
	}
	// 8 + 11 + 5 = 24 characters, rounded up by the +3 before dividing by 4.
	if got := EstimateTokens(msgs); got != (24+3)/4 {
		t.Fatalf("got %d, want %d", got, (24+3)/4)
	}
	if got := EstimateTokens([]Message{{Content: []control.ContentBlock{{Text: "a"}}}}); got != 1 {
		t.Fatalf("one char must count as one token, got %d", got)
	}
}
