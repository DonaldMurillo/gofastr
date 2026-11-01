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

// Grep finds lines matching a regex across files. Tiny ripgrep
// substitute, exact line matches per file.
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
		// A cancelled turn (CancelTurn, shutdown) ends the walk: Grep
		// over "/" otherwise keeps the turn busy until it finishes.
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
		scanner := bufio.NewScanner(io.LimitReader(f, maxGrepFileBytes))
		scanner.Buffer(make([]byte, 1024*64), 1024*1024)
		lineNo := 0
		var read int64
		defer func() {
			// Scan stops on a line past the buffer cap and reports it
			// only through Err. Silently returning the matches found so
			// far tells the caller this file held nothing more.
			if err := scanner.Err(); err != nil {
				fmt.Fprintf(&out, "%s: results truncated: %v\n", path, err)
			} else if read >= maxGrepFileBytes {
				fmt.Fprintf(&out, "%s: results truncated: read the first %d bytes\n", path, int64(maxGrepFileBytes))
			}
		}()
		for scanner.Scan() {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			read += int64(len(scanner.Bytes())) + 1
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
	switch {
	case info.IsDir():
		_ = filepath.WalkDir(root, walk)
	case info.Mode().IsRegular():
		_ = walk(root, fs.FileInfoToDirEntry(info), nil)
	default:
		return errorResult(fmt.Sprintf("Grep: %s is not a regular file or directory", root)), nil
	}
	if ctx.Err() != nil {
		return errorResult(fmt.Sprintf("Grep: cancelled.\nPartial output:\n%s", out.String())), nil
	}
	if out.Len() == 0 {
		return textResult("(no matches)"), nil
	}
	return textResult(out.String()), nil
}

// maxGrepFileBytes caps how much of one file Grep reads, so a huge log
// or a sparse file cannot hold the turn.
const maxGrepFileBytes = 8 << 20

// openRegular opens path only when it is a regular file. FIFOs, devices
// and sockets are skipped: opening a FIFO blocks until a writer appears,
// and /dev/tty or /dev/urandom never end. A symlink counts when its
// target is a regular file. The open handle is checked again so a path
// swapped after the first check is still refused.
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
