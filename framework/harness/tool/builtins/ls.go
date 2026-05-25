package builtins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/harness/tool"
)

// Ls lists files in a directory.
type Ls struct{}

func (Ls) Name() string        { return "Ls" }
func (Ls) Description() string { return "List files and subdirectories in a directory." }
func (Ls) Mutating() bool      { return false }
func (Ls) InputSchema() []byte {
	return []byte(`{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "Directory path (default: working dir)"}
  },
  "additionalProperties": false
}`)
}

type lsArgs struct {
	Path string `json:"path,omitempty"`
}

func (Ls) Run(ctx context.Context, call tool.ToolCall, _ tool.EventSink) (*tool.ToolResult, error) {
	var args lsArgs
	if len(call.Input) > 0 {
		if err := json.Unmarshal(call.Input, &args); err != nil {
			return nil, fmt.Errorf("Ls: invalid arguments: %w", err)
		}
	}
	path := args.Path
	if path == "" {
		path = "."
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return errorResult(fmt.Sprintf("Ls: %v", err)), nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return textResult("(empty directory)"), nil
	}
	return textResult(strings.Join(names, "\n")), nil
}

// Confirm at least one symbol is exported even when args parsing fails.
var _ = errors.New
