// Package profile loads and validates harness profile TOML files.
// Per § Profiles, a profile is config not code; new profiles ship as
// new .toml files without touching the engine.
//
// We use a minimal stdlib-only TOML reader covering exactly the
// subset the v0.1 schema requires:
//
//   - top-level scalars: schema_version, name, default_model,
//     prompt_header, permissions, allow_project_hooks
//   - top-level string arrays: context_sources, skill_packs, tool_packs
//   - inline tables in an array: mcp_servers = [{...}, {...}]
//
// More complex TOML (deeply nested tables, table arrays with multiple
// lines, datetime types) is intentionally rejected — keep the
// surface tight, fail loudly on overreach.
package profile

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// Profile is the parsed schema.
type Profile struct {
	SchemaVersion     int
	Name              string
	DefaultModel      string
	PromptHeader      string
	ContextSources    []string
	SkillPacks        []string
	MCPServers        []MCPServerSpec
	ToolPacks         []string
	PermissionsPreset string
	AllowProjectHooks bool
}

// MCPServerSpec describes one MCP server the profile auto-starts.
type MCPServerSpec struct {
	Name      string
	Cmd       string
	Args      []string
	SHA256    string // required when loaded from project scope
	Discovery string // "eager" | "lazy"
}

// Validate checks invariants the loader enforces beyond syntax.
func (p *Profile) Validate() error {
	if p.SchemaVersion != 1 {
		return fmt.Errorf("profile: schema_version %d unsupported (this binary supports 1)", p.SchemaVersion)
	}
	if p.Name == "" {
		return errors.New("profile: name is required")
	}
	if p.DefaultModel == "" {
		return errors.New("profile: default_model is required")
	}
	for _, s := range p.MCPServers {
		if s.Name == "" || s.Cmd == "" {
			return fmt.Errorf("profile: mcp_server entry needs name and cmd")
		}
		switch s.Discovery {
		case "", "eager", "lazy":
		default:
			return fmt.Errorf("profile: mcp_server %q discovery=%q (want eager or lazy)", s.Name, s.Discovery)
		}
	}
	return nil
}

// Load reads and parses a profile from disk.
func Load(path string) (*Profile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("profile: open %s: %w", path, err)
	}
	defer f.Close()
	return Parse(f)
}

// Parse reads a profile from any io.Reader.
func Parse(r io.Reader) (*Profile, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	p := &Profile{}
	if err := parseTOML(string(data), p); err != nil {
		return nil, err
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return p, nil
}

// --- Minimal TOML parser (single-pass, no allocations beyond results) ---

func parseTOML(src string, p *Profile) error {
	lineNo := 0
	i := 0
	for i < len(src) {
		// Skip whitespace and comments.
		i = skipWS(src, i)
		if i >= len(src) {
			break
		}
		if src[i] == '\n' {
			lineNo++
			i++
			continue
		}
		if src[i] == '#' {
			for i < len(src) && src[i] != '\n' {
				i++
			}
			continue
		}
		// Key = ...
		key, rest, err := readKey(src, i)
		if err != nil {
			return fmt.Errorf("line %d: %w", lineNo+1, err)
		}
		i = rest
		i = skipWS(src, i)
		if i >= len(src) || src[i] != '=' {
			return fmt.Errorf("line %d: expected '=' after key %q", lineNo+1, key)
		}
		i++
		i = skipWS(src, i)
		// Determine value type by leading character.
		if i >= len(src) {
			return fmt.Errorf("line %d: missing value for %q", lineNo+1, key)
		}
		switch src[i] {
		case '"':
			s, next, err := readString(src, i)
			if err != nil {
				return fmt.Errorf("line %d: %w", lineNo+1, err)
			}
			i = next
			if err := assignScalar(p, key, s); err != nil {
				return err
			}
		case 't', 'f':
			b, next, err := readBool(src, i)
			if err != nil {
				return fmt.Errorf("line %d: %w", lineNo+1, err)
			}
			i = next
			if err := assignBool(p, key, b); err != nil {
				return err
			}
		case '[':
			// Could be an array of strings or an array of inline tables.
			next, err := readArray(src, i, key, p)
			if err != nil {
				return fmt.Errorf("line %d: %w", lineNo+1, err)
			}
			i = next
		default:
			// Numbers (only schema_version uses one).
			n, next, err := readInt(src, i)
			if err != nil {
				return fmt.Errorf("line %d: %w", lineNo+1, err)
			}
			i = next
			if err := assignInt(p, key, n); err != nil {
				return err
			}
		}
		// Skip rest of line.
		for i < len(src) && src[i] != '\n' {
			i++
		}
	}
	return nil
}

func skipWS(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\r') {
		i++
	}
	return i
}

func readKey(s string, i int) (key string, next int, err error) {
	start := i
	for i < len(s) {
		c := s[i]
		if c == '=' || c == ' ' || c == '\t' || c == '\n' {
			break
		}
		if !(c >= 'a' && c <= 'z') && !(c >= 'A' && c <= 'Z') && c != '_' && !(c >= '0' && c <= '9') && c != '-' {
			return "", i, fmt.Errorf("invalid key character %q", c)
		}
		i++
	}
	if i == start {
		return "", i, errors.New("empty key")
	}
	return s[start:i], i, nil
}

func readString(s string, i int) (string, int, error) {
	if i >= len(s) || s[i] != '"' {
		return "", i, errors.New("expected string")
	}
	// Triple-quoted multi-line.
	if i+2 < len(s) && s[i+1] == '"' && s[i+2] == '"' {
		i += 3
		// Skip optional immediate newline.
		if i < len(s) && s[i] == '\n' {
			i++
		}
		start := i
		for i+2 < len(s) {
			if s[i] == '"' && s[i+1] == '"' && s[i+2] == '"' {
				return s[start:i], i + 3, nil
			}
			i++
		}
		return "", i, errors.New("unterminated triple-quoted string")
	}
	i++
	var b strings.Builder
	for i < len(s) {
		c := s[i]
		if c == '"' {
			return b.String(), i + 1, nil
		}
		if c == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case '"':
				b.WriteByte('"')
			case '\\':
				b.WriteByte('\\')
			default:
				b.WriteByte(s[i+1])
			}
			i += 2
			continue
		}
		if c == '\n' {
			return "", i, errors.New("unterminated string")
		}
		b.WriteByte(c)
		i++
	}
	return "", i, errors.New("unterminated string")
}

func readBool(s string, i int) (bool, int, error) {
	if strings.HasPrefix(s[i:], "true") {
		return true, i + 4, nil
	}
	if strings.HasPrefix(s[i:], "false") {
		return false, i + 5, nil
	}
	return false, i, errors.New("expected true or false")
}

func readInt(s string, i int) (int, int, error) {
	start := i
	for i < len(s) && (s[i] >= '0' && s[i] <= '9') {
		i++
	}
	if start == i {
		return 0, i, errors.New("expected integer")
	}
	n, err := strconv.Atoi(s[start:i])
	return n, i, err
}

func readArray(s string, i int, key string, p *Profile) (int, error) {
	if s[i] != '[' {
		return i, errors.New("expected [")
	}
	i++
	// Look ahead: an array of inline tables starts with `{`.
	j := skipArrayWS(s, i)
	if j < len(s) && s[j] == '{' {
		// Array of inline tables.
		for {
			j = skipArrayWS(s, j)
			if j >= len(s) {
				return j, errors.New("unterminated array")
			}
			if s[j] == ']' {
				return j + 1, nil
			}
			if s[j] == '{' {
				m := map[string]any{}
				next, err := readInlineTable(s, j, m)
				if err != nil {
					return next, err
				}
				j = next
				if err := appendInlineTable(p, key, m); err != nil {
					return j, err
				}
				j = skipArrayWS(s, j)
				if j < len(s) && s[j] == ',' {
					j++
				}
				continue
			}
			return j, fmt.Errorf("unexpected character %q in array", s[j])
		}
	}
	// Array of strings.
	var out []string
	for {
		j = skipArrayWS(s, j)
		if j >= len(s) {
			return j, errors.New("unterminated array")
		}
		if s[j] == ']' {
			if err := assignStringArray(p, key, out); err != nil {
				return j, err
			}
			return j + 1, nil
		}
		if s[j] == '"' {
			str, next, err := readString(s, j)
			if err != nil {
				return next, err
			}
			out = append(out, str)
			j = next
			j = skipArrayWS(s, j)
			if j < len(s) && s[j] == ',' {
				j++
			}
			continue
		}
		return j, fmt.Errorf("unexpected character %q in array of strings", s[j])
	}
}

func skipArrayWS(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
		i++
	}
	// Comments inside arrays.
	for i < len(s) && s[i] == '#' {
		for i < len(s) && s[i] != '\n' {
			i++
		}
		for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
			i++
		}
	}
	return i
}

func readInlineTable(s string, i int, dst map[string]any) (int, error) {
	if s[i] != '{' {
		return i, errors.New("expected {")
	}
	i++
	for {
		i = skipArrayWS(s, i)
		if i >= len(s) {
			return i, errors.New("unterminated inline table")
		}
		if s[i] == '}' {
			return i + 1, nil
		}
		key, next, err := readKey(s, i)
		if err != nil {
			return next, err
		}
		i = next
		i = skipArrayWS(s, i)
		if i >= len(s) || s[i] != '=' {
			return i, errors.New("inline table: expected =")
		}
		i++
		i = skipArrayWS(s, i)
		if i >= len(s) {
			return i, errors.New("inline table: expected value")
		}
		switch s[i] {
		case '"':
			str, next, err := readString(s, i)
			if err != nil {
				return next, err
			}
			dst[key] = str
			i = next
		case '[':
			j := i + 1
			var arr []string
			for {
				j = skipArrayWS(s, j)
				if j >= len(s) {
					return j, errors.New("unterminated inline array")
				}
				if s[j] == ']' {
					j++
					break
				}
				if s[j] != '"' {
					return j, errors.New("inline array: expected string")
				}
				str, next, err := readString(s, j)
				if err != nil {
					return next, err
				}
				arr = append(arr, str)
				j = next
				j = skipArrayWS(s, j)
				if j < len(s) && s[j] == ',' {
					j++
				}
			}
			dst[key] = arr
			i = j
		default:
			return i, fmt.Errorf("inline table: unsupported value type starting %q", s[i])
		}
		i = skipArrayWS(s, i)
		if i < len(s) && s[i] == ',' {
			i++
		}
	}
}

// --- Field assignment ---

func assignScalar(p *Profile, key, val string) error {
	switch key {
	case "name":
		p.Name = val
	case "default_model":
		p.DefaultModel = val
	case "prompt_header":
		p.PromptHeader = val
	case "permissions":
		p.PermissionsPreset = val
	default:
		return fmt.Errorf("profile: unknown string key %q", key)
	}
	return nil
}

func assignBool(p *Profile, key string, v bool) error {
	switch key {
	case "allow_project_hooks":
		p.AllowProjectHooks = v
	default:
		return fmt.Errorf("profile: unknown bool key %q", key)
	}
	return nil
}

func assignInt(p *Profile, key string, n int) error {
	switch key {
	case "schema_version":
		p.SchemaVersion = n
	default:
		return fmt.Errorf("profile: unknown int key %q", key)
	}
	return nil
}

func assignStringArray(p *Profile, key string, vs []string) error {
	switch key {
	case "context_sources":
		p.ContextSources = vs
	case "skill_packs":
		p.SkillPacks = vs
	case "tool_packs":
		p.ToolPacks = vs
	default:
		return fmt.Errorf("profile: unknown array key %q", key)
	}
	return nil
}

func appendInlineTable(p *Profile, key string, m map[string]any) error {
	switch key {
	case "mcp_servers":
		spec := MCPServerSpec{}
		if v, _ := m["name"].(string); v != "" {
			spec.Name = v
		}
		if v, _ := m["cmd"].(string); v != "" {
			spec.Cmd = v
		}
		if args, _ := m["args"].([]string); args != nil {
			spec.Args = args
		}
		if v, _ := m["sha256"].(string); v != "" {
			spec.SHA256 = v
		}
		if v, _ := m["discovery"].(string); v != "" {
			spec.Discovery = v
		}
		p.MCPServers = append(p.MCPServers, spec)
	default:
		return fmt.Errorf("profile: unknown inline-table array key %q", key)
	}
	return nil
}
