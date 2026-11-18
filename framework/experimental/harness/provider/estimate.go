package provider

// EstimateTokens estimates the tokens in msgs at four bytes per token, rounded up.
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
