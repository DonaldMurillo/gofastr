package builtins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/harness/tool"
)

// Glob returns paths matching a glob pattern. Uses doublestar-compatible
// semantics: `**` matches any number of directories.
type Glob struct{}

func (Glob) Name() string { return "Glob" }
func (Glob) Description() string {
	return "Find files matching a glob pattern (supports ** for any depth)."
}
func (Glob) Mutating() bool { return false }
func (Glob) InputSchema() []byte {
	return []byte(`{
  "type": "object",
  "properties": {
    "pattern": {"type": "string", "description": "Glob pattern (supports ** for any depth)"},
    "root":    {"type": "string", "description": "Directory to search from (default: working dir)"}
  },
  "required": ["pattern"],
  "additionalProperties": false
}`)
}

type globArgs struct {
	Pattern string `json:"pattern"`
	Root    string `json:"root,omitempty"`
}

func (Glob) Run(ctx context.Context, call tool.ToolCall, _ tool.EventSink) (*tool.ToolResult, error) {
	var args globArgs
	if err := json.Unmarshal(call.Input, &args); err != nil {
		return nil, fmt.Errorf("Glob: invalid arguments: %w", err)
	}
	if args.Pattern == "" {
		return nil, errors.New("Glob: pattern is required")
	}
	root := args.Root
	if root == "" {
		root = "."
	}
	var matches []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // ignore unreadable subtrees
		}
		// Use a tiny doublestar-style match: split on **/, match each segment with filepath.Match.
		rel, _ := filepath.Rel(root, path)
		if doublestarMatch(args.Pattern, rel) {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		return errorResult(fmt.Sprintf("Glob: %v", err)), nil
	}
	sort.Strings(matches)
	return textResult(strings.Join(matches, "\n")), nil
}

// doublestarMatch is a minimal `**`-aware matcher. The pattern's path
// segments are matched against the path's segments; a `**` segment
// matches zero or more path segments.
func doublestarMatch(pattern, path string) bool {
	pat := strings.Split(filepath.ToSlash(pattern), "/")
	p := strings.Split(filepath.ToSlash(path), "/")
	return doublestarRec(pat, p)
}

func doublestarRec(pat, p []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			// Match zero or more path segments.
			rest := pat[1:]
			if len(rest) == 0 {
				return true // ** at end matches everything
			}
			for i := 0; i <= len(p); i++ {
				if doublestarRec(rest, p[i:]) {
					return true
				}
			}
			return false
		}
		if len(p) == 0 {
			return false
		}
		ok, _ := filepath.Match(pat[0], p[0])
		if !ok {
			return false
		}
		pat = pat[1:]
		p = p[1:]
	}
	return len(p) == 0
}
