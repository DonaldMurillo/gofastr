package provider

// EstimateTokens returns a best-effort token estimate for msgs using
// the 4-chars-per-token heuristic (the rule of thumb across English
// text for most modern tokenizers). It replaces the two identical
// TokenCount bodies formerly duplicated in the openrouter and zai
// adapters, which have no tokenization endpoint to call; the +3 rounds
// up so a partial token counts as one.
func EstimateTokens(msgs []Message) int {
	total := 0
	for _, m := range msgs {
		for _, b := range m.Content {
			total += len(b.Text)
			if b.ToolUse != nil {
				total += len(b.ToolUse.Name) + len(b.ToolUse.Input)
			}
			if b.ToolResult != nil {
				for _, c := range b.ToolResult.Content {
					total += len(c.Text)
				}
			}
		}
	}
	return (total + 3) / 4
}
