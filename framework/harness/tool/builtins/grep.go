package builtins

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/harness/tool"
)

// Grep finds lines matching a regex across files. Tiny ripgrep
// substitute — exact line matches per file.
type Grep struct{}

func (Grep) Name() string        { return "Grep" }
func (Grep) Description() string { return "Search files for lines matching a regular expression." }
func (Grep) Mutating() bool      { return false }
func (Grep) InputSchema() []byte {
	return []byte(`{
  "type": "object",
  "properties": {
    "pattern": {"type": "string", "description": "Go regexp pattern"},
    "path":    {"type": "string", "description": "File or directory to search (default: working dir)"},
    "glob":    {"type": "string", "description": "Optional filename glob filter (e.g. *.go)"}
  },
  "required": ["pattern"],
  "additionalProperties": false
}`)
}

type grepArgs struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`
	Glob    string `json:"glob,omitempty"`
}

func (Grep) Run(ctx context.Context, call tool.ToolCall, _ tool.EventSink) (*tool.ToolResult, error) {
	var args grepArgs
	if err := json.Unmarshal(call.Input, &args); err != nil {
		return nil, fmt.Errorf("Grep: invalid arguments: %w", err)
	}
	if args.Pattern == "" {
		return nil, errors.New("Grep: pattern is required")
	}
	re, err := regexp.Compile(args.Pattern)
	if err != nil {
		return errorResult(fmt.Sprintf("Grep: invalid regexp: %v", err)), nil
	}
	root := args.Path
	if root == "" {
		root = "."
	}

	var out strings.Builder
	matched := 0
	walk := func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if args.Glob != "" {
			ok, _ := filepath.Match(args.Glob, filepath.Base(path))
			if !ok {
				return nil
			}
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 1024*64), 1024*1024)
		lineNo := 0
		for scanner.Scan() {
			lineNo++
			if re.MatchString(scanner.Text()) {
				fmt.Fprintf(&out, "%s:%d: %s\n", path, lineNo, scanner.Text())
				matched++
				if matched >= 1000 { // cap output
					return fs.SkipAll
				}
			}
		}
		return nil
	}

	info, err := os.Stat(root)
	if err != nil {
		return errorResult(fmt.Sprintf("Grep: stat %s: %v", root, err)), nil
	}
	if info.IsDir() {
		_ = filepath.WalkDir(root, walk)
	} else {
		_ = walk(root, fs.FileInfoToDirEntry(info), nil)
	}
	if matched == 0 {
		return textResult("(no matches)"), nil
	}
	return textResult(out.String()), nil
}
