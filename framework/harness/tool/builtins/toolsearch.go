package builtins

// ToolSearch — the harness's tool-discovery tool. Always available in
// the base toolset; lets the model ask "what tools do I have for
// editing files?" instead of being primed with every schema up front.
//
// Why this exists:
//
//   - As MCP servers and plugin packs grow, shipping every tool's
//     full JSON schema in every Request burns input tokens fast.
//     ToolSearch lets the model narrow to relevant tools on demand.
//   - When the model encounters a problem it hasn't solved before,
//     it can search for capabilities by intent ("compress files",
//     "send email") rather than guessing which tool exists.
//   - Plugin or runtime-added tools that the system-prompt template
//     can't enumerate at startup are still discoverable.
//
// Behavior:
//
//   - Substring + word-overlap ranking against name + description.
//     Cheap, deterministic, no dep on a fuzzy-match library.
//   - Returns top N (default 8) matches each carrying name, mutating
//     flag, description, and the full inputSchema bytes. The model
//     can call the discovered tool the very next round.
//   - Always-on: registered in every session regardless of pack
//     filters so the model can never end up tool-less.

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/tool"
)

// ToolSearch tool. Stateless — pulls the registry from the dispatch
// ctx via tool.RegistryFromContext.
type ToolSearch struct{}

func (ToolSearch) Name() string { return "ToolSearch" }
func (ToolSearch) Description() string {
	return "Discover tools available in this harness. Pass a query (a few words describing the capability you want, e.g. 'read file', 'spawn sub-agent', 'list directory'). Returns the top-matching tools with their schemas so you can call them directly. Use when you're unsure whether a capability exists or need a tool you don't see in your immediate context."
}
func (ToolSearch) Mutating() bool { return false }
func (ToolSearch) InputSchema() []byte {
	return []byte(`{
  "type": "object",
  "properties": {
    "query": {"type": "string", "description": "Free-text description of the capability you want."},
    "limit": {"type": "integer", "minimum": 1, "maximum": 50, "description": "Max results (default 8)."}
  },
  "required": ["query"],
  "additionalProperties": false
}`)
}

type toolSearchArgs struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
}

type toolMatch struct {
	Name        string          `json:"name"`
	Mutating    bool            `json:"mutating"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Score       int             `json:"score"`
}

func (ToolSearch) Run(ctx context.Context, call tool.ToolCall, _ tool.EventSink) (*tool.ToolResult, error) {
	var args toolSearchArgs
	if err := json.Unmarshal(call.Input, &args); err != nil {
		return nil, fmt.Errorf("ToolSearch: invalid arguments: %w", err)
	}
	if strings.TrimSpace(args.Query) == "" {
		return errorResult("ToolSearch: query is required"), nil
	}
	limit := args.Limit
	if limit <= 0 {
		limit = 8
	}
	reg, ok := tool.RegistryFromContext(ctx)
	if !ok || reg == nil {
		return errorResult("ToolSearch: no registry available in dispatch context"), nil
	}

	terms := tokenize(args.Query)
	all := reg.List()
	matches := make([]toolMatch, 0, len(all))
	for _, t := range all {
		// Skip ToolSearch itself — model already knows about it.
		if t.Name() == "ToolSearch" {
			continue
		}
		score := scoreMatch(terms, t.Name(), t.Description())
		if score == 0 {
			continue
		}
		matches = append(matches, toolMatch{
			Name:        t.Name(),
			Mutating:    t.Mutating(),
			Description: t.Description(),
			InputSchema: t.InputSchema(),
			Score:       score,
		})
	}
	sort.SliceStable(matches, func(i, j int) bool {
		return matches[i].Score > matches[j].Score
	})
	if len(matches) > limit {
		matches = matches[:limit]
	}
	if len(matches) == 0 {
		return &tool.ToolResult{
			Content: []control.ContentBlock{{Type: "text",
				Text: fmt.Sprintf("ToolSearch: no tools matched %q. Try broader keywords (e.g. 'file', 'web', 'shell', 'plan', 'agent').",
					args.Query)}},
		}, nil
	}
	raw, err := json.MarshalIndent(matches, "", "  ")
	if err != nil {
		return nil, err
	}
	return &tool.ToolResult{
		Content: []control.ContentBlock{{Type: "text",
			Text: fmt.Sprintf("Found %d tool%s matching %q:\n%s",
				len(matches), plural(len(matches)), args.Query, string(raw))}},
	}, nil
}

// tokenize splits a query into lowercase words, dropping common
// stopwords that don't help matching.
func tokenize(q string) []string {
	stop := map[string]bool{
		"a": true, "an": true, "the": true, "to": true, "for": true,
		"of": true, "in": true, "on": true, "with": true, "and": true,
		"or": true, "i": true, "want": true, "need": true,
	}
	var out []string
	for _, w := range strings.Fields(strings.ToLower(q)) {
		w = strings.Trim(w, ".,!?:;()[]{}\"'")
		if w == "" || stop[w] {
			continue
		}
		out = append(out, w)
	}
	return out
}

// scoreMatch returns a relevance score: name match weighs more than
// description; multi-term overlap accumulates.
func scoreMatch(terms []string, name, desc string) int {
	if len(terms) == 0 {
		return 0
	}
	nameL := strings.ToLower(name)
	descL := strings.ToLower(desc)
	score := 0
	for _, t := range terms {
		if strings.Contains(nameL, t) {
			score += 10
		}
		if strings.Contains(descL, t) {
			score += 3
		}
	}
	return score
}
