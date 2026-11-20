package builtins

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool"
)

// Grep returns the lines matching a Go regexp in a file or in the regular files under a directory.
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
	capped := false
	walk := func(path string, d fs.DirEntry, walkErr error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
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
		f, ok := openRegular(path, d)
		if !ok {
			return nil
		}
		defer f.Close()
		limited := &io.LimitedReader{R: f, N: maxGrepFileBytes}
		scanner := bufio.NewScanner(limited)
		scanner.Buffer(make([]byte, 1024*64), 1024*1024)
		lineNo := 0
		defer func() {
			if capped {
				return
			}
			if err := scanner.Err(); err != nil {
				fmt.Fprintf(&out, "%s: results truncated: %v\n", path, err)
			} else if limited.N == 0 && moreToRead(f) {
				fmt.Fprintf(&out, "%s: results truncated: read the first %d bytes\n", path, int64(maxGrepFileBytes))
			}
		}()
		for scanner.Scan() {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			lineNo++
			if re.MatchString(scanner.Text()) {
				fmt.Fprintf(&out, "%s:%d: %s\n", path, lineNo, scanner.Text())
				matched++
				if matched >= maxGrepMatches {
					capped = true
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
	switch {
	case info.IsDir():
		if resolved, err := filepath.EvalSymlinks(root); err == nil {
			root = resolved
		}
		_ = filepath.WalkDir(root, walk)
	case info.Mode().IsRegular():
		_ = walk(root, fs.FileInfoToDirEntry(info), nil)
	default:
		return errorResult(fmt.Sprintf("Grep: %s is not a regular file or directory", root)), nil
	}
	if ctx.Err() != nil {
		return errorResult(fmt.Sprintf("Grep: cancelled.\nPartial output:\n%s", out.String())), nil
	}
	if capped {
		fmt.Fprintf(&out, "results truncated: stopped at %d matches; narrow the pattern or the path\n", maxGrepMatches)
	}
	if out.Len() == 0 {
		return textResult("(no matches)"), nil
	}
	return textResult(out.String()), nil
}

const maxGrepFileBytes = 8 << 20

const maxGrepMatches = 1000

func moreToRead(f *os.File) bool {
	var one [1]byte
	n, err := f.Read(one[:])
	return err == nil && n > 0
}

func openRegular(path string, d fs.DirEntry) (*os.File, bool) {
	mode := d.Type()
	if mode&fs.ModeSymlink != 0 {
		info, err := os.Stat(path)
		if err != nil {
			return nil, false
		}
		mode = info.Mode().Type()
	}
	if !mode.IsRegular() {
		return nil, false
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, false
	}
	return f, true
}
