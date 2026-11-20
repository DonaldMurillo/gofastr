package slash

import (
	"errors"
	"fmt"
	"strings"
)

// Cmd is a parsed command: "/skills:foo a b" has Namespace "skills", Name "foo", Args [a b]; "/help" is a built-in.
type Cmd struct {
	IsBuiltin bool
	Namespace string
	Name      string
	Args      []string
	Raw       string
}

// Parse parses "/[namespace:]name args..." with double-quoted args and \\, \", \n, \t escapes; ErrNotSlashCommand without a leading "/".
func Parse(input string) (Cmd, error) {
	if !strings.HasPrefix(input, "/") {
		return Cmd{}, ErrNotSlashCommand
	}
	body := strings.TrimPrefix(input, "/")
	if body == "" {
		return Cmd{}, errors.New("slash: empty command")
	}
	head, rest := splitHead(body)
	c := Cmd{Raw: input, Args: splitArgs(rest)}
	if before, after, ok := strings.Cut(head, ":"); ok {
		c.Namespace = before
		c.Name = after
		if c.Name == "" {
			return Cmd{}, fmt.Errorf("slash: missing name after %q:", c.Namespace)
		}
	} else {
		c.Name = head
		c.IsBuiltin = true
	}
	return c, nil
}

func splitHead(s string) (head, rest string) {
	for i, r := range s {
		if r == ' ' || r == '\t' {
			return s[:i], strings.TrimLeft(s[i:], " \t")
		}
	}
	return s, ""
}

func splitArgs(s string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	i := 0
	for i < len(s) {
		c := s[i]
		if c == '\\' && i+1 < len(s) {
			next := s[i+1]
			switch next {
			case '\\':
				cur.WriteByte('\\')
			case '"':
				cur.WriteByte('"')
			case 'n':
				cur.WriteByte('\n')
			case 't':
				cur.WriteByte('\t')
			default:
				cur.WriteByte(next)
			}
			i += 2
			continue
		}
		if c == '"' {
			inQuote = !inQuote
			i++
			continue
		}
		if !inQuote && (c == ' ' || c == '\t') {
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
			i++
			continue
		}
		cur.WriteByte(c)
		i++
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// Builtin describes one built-in command.
type Builtin struct {
	Name        string
	Description string
	ArgsHelp    string
}

// AllBuiltins returns the built-in command catalog clients list; the handlers live elsewhere.
func AllBuiltins() []Builtin {
	return []Builtin{
		{"help", "List all commands grouped by namespace.", ""},
		{"clear", "Clear scrollback (does not affect log).", ""},
		{"compact", "Trigger history compaction now.", ""},
		{"cost", "Detailed cost breakdown for the current session.", ""},
		{"model", "Set the active model.", "<model-id>"},
		{"profile", "Show the active profile + skills/MCP/tools loaded.", ""},
		{"quit", "Exit the harness (two-step confirm).", ""},
		{"web", "Show the --web URL (and copy to clipboard).", ""},
		{"health", "Status of every subsystem.", ""},
		{"tasks", "Show the agent's current TaskList plan in a modal.", ""},
	}
}

// AllNamespaces returns the namespaces reserved by the harness.
func AllNamespaces() []string {
	return []string{
		"skills", "agents", "profiles", "sessions", "mcp", "permissions",
	}
}

// ErrNotSlashCommand is returned by Parse for input without a leading "/".
var ErrNotSlashCommand = errors.New("slash: not a slash command")

// ErrUnknownNamespace reports a namespace nothing has claimed.
var ErrUnknownNamespace = errors.New("slash: unknown namespace")
