package builtins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/harness/tool"
)

// writeImpl creates or overwrites a file with the given content.
type writeImpl struct{}

func (writeImpl) Name() string        { return "Write" }
func (writeImpl) Description() string { return "Create or overwrite a file with the given content." }
func (writeImpl) Mutating() bool      { return true }
func (writeImpl) InputSchema() []byte {
	return []byte(`{
  "type": "object",
  "properties": {
    "path":    {"type": "string", "description": "Absolute or working-directory-relative path"},
    "content": {"type": "string", "description": "Full file content"},
    "mode":    {"type": "integer", "description": "POSIX file mode (default 0o644)"}
  },
  "required": ["path", "content"],
  "additionalProperties": false
}`)
}

type writeArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Mode    int    `json:"mode,omitempty"`
}

func (writeImpl) Run(ctx context.Context, call tool.ToolCall, _ tool.EventSink) (*tool.ToolResult, error) {
	var args writeArgs
	if err := json.Unmarshal(call.Input, &args); err != nil {
		return nil, fmt.Errorf("Write: invalid arguments: %w", err)
	}
	if args.Path == "" {
		return nil, errors.New("Write: path is required")
	}
	mode := os.FileMode(args.Mode)
	if mode == 0 {
		mode = 0o644
	}
	if err := os.MkdirAll(filepath.Dir(args.Path), 0o755); err != nil {
		return errorResult(fmt.Sprintf("Write: mkdir parent: %v", err)), nil
	}
	if err := os.WriteFile(args.Path, []byte(args.Content), mode); err != nil {
		return errorResult(fmt.Sprintf("Write: %v", err)), nil
	}
	return textResult(fmt.Sprintf("Wrote %d bytes to %s", len(args.Content), args.Path)), nil
}

// editImpl performs a string-replace edit on a file. Refuses to operate
// when the old_string appears more than once unless replace_all is true.
type editImpl struct{}

func (editImpl) Name() string        { return "Edit" }
func (editImpl) Description() string { return "Apply an exact-string replacement to a file." }
func (editImpl) Mutating() bool      { return true }
func (editImpl) InputSchema() []byte {
	return []byte(`{
  "type": "object",
  "properties": {
    "path":        {"type": "string"},
    "old_string":  {"type": "string"},
    "new_string":  {"type": "string"},
    "replace_all": {"type": "boolean"}
  },
  "required": ["path", "old_string", "new_string"],
  "additionalProperties": false
}`)
}

type editArgs struct {
	Path       string `json:"path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

func (editImpl) Run(ctx context.Context, call tool.ToolCall, _ tool.EventSink) (*tool.ToolResult, error) {
	var args editArgs
	if err := json.Unmarshal(call.Input, &args); err != nil {
		return nil, fmt.Errorf("Edit: invalid arguments: %w", err)
	}
	if args.Path == "" || args.OldString == "" {
		return nil, errors.New("Edit: path and old_string are required")
	}
	if args.OldString == args.NewString {
		return errorResult("Edit: old_string and new_string are identical"), nil
	}
	data, err := os.ReadFile(args.Path)
	if err != nil {
		return errorResult(fmt.Sprintf("Edit: read: %v", err)), nil
	}
	content := string(data)
	count := strings.Count(content, args.OldString)
	if count == 0 {
		return errorResult(fmt.Sprintf("Edit: old_string not found in %s", args.Path)), nil
	}
	if count > 1 && !args.ReplaceAll {
		return errorResult(fmt.Sprintf(
			"Edit: old_string appears %d times in %s. Provide a longer unique substring or set replace_all=true.",
			count, args.Path)), nil
	}
	var updated string
	if args.ReplaceAll {
		updated = strings.ReplaceAll(content, args.OldString, args.NewString)
	} else {
		updated = strings.Replace(content, args.OldString, args.NewString, 1)
	}
	if err := os.WriteFile(args.Path, []byte(updated), 0o644); err != nil {
		return errorResult(fmt.Sprintf("Edit: write: %v", err)), nil
	}
	return textResult(fmt.Sprintf("Replaced %d occurrence(s) in %s", count, args.Path)), nil
}
