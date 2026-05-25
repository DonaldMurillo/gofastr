// Package builtins implements the v0.1 default tool set: Read, Write,
// Edit, Bash, Grep, Glob, Ls, WebFetch. Each tool is small, focused,
// and declares is_mutating per the persistence layer's contract.
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

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/tool"
)

// Read is the file-read tool. Reads up to MaxBytes from a file (default
// 1 MiB) and returns the text. Optional Offset (byte) and Limit (lines)
// for partial reads.
type Read struct {
	// MaxBytes caps a single Read response. 0 means use the default (1 MiB).
	MaxBytes int64
}

const defaultReadMax int64 = 1 << 20

func (Read) Name() string        { return "Read" }
func (Read) Description() string { return "Read a file from the local filesystem and return its content as text." }
func (Read) Mutating() bool      { return false }
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
	f, err := os.Open(args.Path)
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
		// Line-limited read.
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
	// Byte-limited read.
	data, err := io.ReadAll(io.LimitReader(f, maxBytes))
	if err != nil {
		return errorResult(fmt.Sprintf("Read: %v", err)), nil
	}
	return textResult(string(data)), nil
}

// textResult wraps a string into a successful ToolResult.
func textResult(s string) *tool.ToolResult {
	return &tool.ToolResult{
		Content: []control.ContentBlock{{Type: "text", Text: s}},
	}
}

// errorResult wraps a string into a failed ToolResult.
func errorResult(s string) *tool.ToolResult {
	return &tool.ToolResult{
		Content: []control.ContentBlock{{Type: "text", Text: s}},
		IsError: true,
	}
}
