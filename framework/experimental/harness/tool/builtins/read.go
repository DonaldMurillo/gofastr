package builtins

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool"
)

// Read returns a regular file's text, from an optional byte offset and up to an optional line count.
type Read struct {
	// MaxBytes caps the bytes read; 0 means 1 MiB.
	MaxBytes int64
}

const defaultReadMax int64 = 1 << 20

func (Read) Name() string { return "Read" }
func (Read) Description() string {
	return "Read a file from the local filesystem and return its content as text."
}
func (Read) Mutating() bool { return false }
func (Read) InputSchema() []byte {
	return []byte(`{
  "type": "object",
  "properties": {
    "path":   {"type": "string", "description": "Absolute or working-directory-relative path"},
    "offset": {"type": "integer", "minimum": 0, "description": "Byte offset to start reading from"},
    "limit":  {"type": "integer", "minimum": 1, "description": "Maximum number of lines to return"}
  },
  "required": ["path"],
  "additionalProperties": false
}`)
}

type readArgs struct {
	Path   string `json:"path"`
	Offset int64  `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

func (r Read) Run(ctx context.Context, call tool.ToolCall, _ tool.EventSink) (*tool.ToolResult, error) {
	var args readArgs
	if err := json.Unmarshal(call.Input, &args); err != nil {
		return nil, fmt.Errorf("Read: invalid arguments: %w", err)
	}
	if args.Path == "" {
		return nil, errors.New("Read: path is required")
	}
	maxBytes := r.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultReadMax
	}
	f, err := openRegularPath(args.Path)
	if err != nil {
		return errorResult(fmt.Sprintf("Read: open %s: %v", args.Path, err)), nil
	}
	defer f.Close()
	if args.Offset > 0 {
		if _, err := f.Seek(args.Offset, io.SeekStart); err != nil {
			return errorResult(fmt.Sprintf("Read: seek %d: %v", args.Offset, err)), nil
		}
	}
	if args.Limit > 0 {
		var b strings.Builder
		scanner := bufio.NewScanner(io.LimitReader(f, maxBytes))
		scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024)
		count := 0
		for scanner.Scan() && count < args.Limit {
			b.WriteString(scanner.Text())
			b.WriteByte('\n')
			count++
		}
		if err := scanner.Err(); err != nil {
			return errorResult(fmt.Sprintf("Read: scan: %v", err)), nil
		}
		return textResult(b.String()), nil
	}
	data, err := io.ReadAll(io.LimitReader(f, maxBytes))
	if err != nil {
		return errorResult(fmt.Sprintf("Read: %v", err)), nil
	}
	return textResult(string(data)), nil
}

var errNotRegular = errors.New("not a regular file")

func openRegularPath(path string) (*os.File, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errNotRegular
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, errNotRegular
	}
	return f, nil
}

func textResult(s string) *tool.ToolResult {
	return &tool.ToolResult{
		Content: []control.ContentBlock{{Type: "text", Text: s}},
	}
}

func errorResult(s string) *tool.ToolResult {
	return &tool.ToolResult{
		Content: []control.ContentBlock{{Type: "text", Text: s}},
		IsError: true,
	}
}
