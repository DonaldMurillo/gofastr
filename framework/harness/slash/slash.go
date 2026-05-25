// Package slash implements the slash-command parser + the built-in
// command catalog.
//
// Per § Slash commands, parsing happens at the client: a /-prefixed
// input is converted to a typed wire Command before being sent to the
// engine. Built-in commands (bare /foo, no namespace) ship with the
// harness; user/plugin commands live under namespaces like
// /custom:bar, /super-skill:baz.
package slash

import (
	"errors"
	"fmt"
	"strings"
)

// Cmd is a parsed slash-command invocation.
type Cmd struct {
	// IsBuiltin reports whether Namespace is empty.
	IsBuiltin bool
	// Namespace is the prefix before the colon ("skills" in
	// "/skills:foo"). Empty for built-ins.
	Namespace string
	// Name is the verb after the colon (or after the slash for
	// built-ins). "foo" in "/skills:foo".
	Name string
	// Args is the remaining whitespace-delimited tokens, preserving
	// quoted segments as single tokens.
	Args []string
	// Raw is the original input including leading slash.
	Raw string
}

// Parse converts a slash-prefixed input to a Cmd. Returns
// ErrNotSlashCommand if the input doesn't start with '/'.
//
// Quoting: arguments may be wrapped in double quotes to preserve
// whitespace ("path with spaces"). Escapes inside quotes: \\ → \, \"
// → ", \n → newline.
func Parse(input string) (Cmd, error) {
	if !strings.HasPrefix(input, "/") {
		return Cmd{}, ErrNotSlashCommand
	}
	body := strings.TrimPrefix(input, "/")
	if body == "" {
		return Cmd{}, errors.New("slash: empty command")
	}
	// Split head from args at the first space.
	head, rest := splitHead(body)
	c := Cmd{Raw: input, Args: splitArgs(rest)}
	if i := strings.Index(head, ":"); i >= 0 {
		c.Namespace = head[:i]
		c.Name = head[i+1:]
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

// splitArgs tokenizes a string into args, respecting double quotes.
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

// Builtin is one shipped command's metadata.
type Builtin struct {
	Name        string
	Description string
	ArgsHelp    string // one-line argument summary
}

// AllBuiltins returns the v0.1 built-in command catalog, as documented
// at § Slash commands → Built-in commands shipped in v0.1.
//
// The handlers are wired by the harness composition layer; this
// catalog is what clients display via GET /v1/slash-commands.
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

// AllNamespaces returns the reserved namespaces shipped in v0.1.
// Plugins claim additional namespaces via Plugin.Register.
func AllNamespaces() []string {
	return []string{
		"skills", "agents", "profiles", "sessions", "mcp", "permissions",
	}
}

// ErrNotSlashCommand signals an input didn't start with '/'.
var ErrNotSlashCommand = errors.New("slash: not a slash command")

// ErrUnknownNamespace is returned by the resolver when a namespace
// isn't claimed by built-ins or any plugin.
var ErrUnknownNamespace = errors.New("slash: unknown namespace")
