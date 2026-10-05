package devloop

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
)

// toolEvent is one tool call from a Claude Code stream-json transcript,
// in the order the agent made them. Command is a Bash call's shell line;
// Path is the file or directory a Read, Grep or Glob call named.
type toolEvent struct {
	Name    string `json:"name"`
	Command string `json:"command,omitempty"`
	Path    string `json:"path,omitempty"`
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
						Command  string `json:"command"`
						FilePath string `json:"file_path"`
						Path     string `json:"path"`
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
				p := c.Input.FilePath
				if p == "" {
					p = c.Input.Path
				}
				out.Events = append(out.Events, toolEvent{
					Name:    c.Name,
					Command: strings.TrimSpace(c.Input.Command),
					Path:    p,
				})
			}
		case "result":
			out.Final, out.CostUSD, out.IsError = line.Result, line.CostUSD, line.IsError
		}
	}
	return out, scanner.Err()
}
