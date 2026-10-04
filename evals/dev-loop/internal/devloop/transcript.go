package devloop

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
)

// toolEvent is one tool call from a Claude Code stream-json transcript,
// in the order the agent made them.
type toolEvent struct {
	Name       string `json:"name"`
	Command    string `json:"command,omitempty"`
	Background bool   `json:"background,omitempty"`
}

// transcriptSummary is what grading needs from the transcript: the tool
// calls in order, and the final result line's text and cost.
type transcriptSummary struct {
	Events  []toolEvent
	Final   string
	CostUSD float64
	IsError bool
}

// readTranscript parses `claude -p --output-format stream-json --verbose`
// output. Lines that are not JSON, or not an assistant tool_use or the
// result record, are skipped: the stream carries system and user events
// grading has no use for.
func readTranscript(path string) (transcriptSummary, error) {
	var out transcriptSummary
	f, err := os.Open(path)
	if err != nil {
		return out, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 64*1024*1024)
	for scanner.Scan() {
		var line struct {
			Type    string `json:"type"`
			Message struct {
				Content []struct {
					Type  string `json:"type"`
					Name  string `json:"name"`
					Input struct {
						Command         string `json:"command"`
						RunInBackground bool   `json:"run_in_background"`
					} `json:"input"`
				} `json:"content"`
			} `json:"message"`
			Result  string  `json:"result"`
			CostUSD float64 `json:"total_cost_usd"`
			IsError bool    `json:"is_error"`
		}
		if json.Unmarshal(scanner.Bytes(), &line) != nil {
			continue
		}
		switch line.Type {
		case "assistant":
			for _, c := range line.Message.Content {
				if c.Type != "tool_use" {
					continue
				}
				out.Events = append(out.Events, toolEvent{
					Name:       c.Name,
					Command:    strings.TrimSpace(c.Input.Command),
					Background: c.Input.RunInBackground,
				})
			}
		case "result":
			out.Final, out.CostUSD, out.IsError = line.Result, line.CostUSD, line.IsError
		}
	}
	return out, scanner.Err()
}
