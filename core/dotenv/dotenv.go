// Package dotenv parses .env files into a map and (optionally) applies
// them to the process environment. Designed for the framework's
// startup path so apps get familiar .env behavior without taking on a
// third-party dependency.
//
// Format (a strict subset of the de facto dotenv spec):
//
//   # comments and blank lines are allowed
//   FOO=bar                       # bareword value
//   QUOTED="hello world"          # double-quoted: escapes interpreted
//   LITERAL='hello\nworld'        # single-quoted: VERBATIM, no escapes
//   export PORT=8080              # optional `export` prefix tolerated
//   PATH_TPL="${HOME}/bin"        # ${VAR} expansion (double-quoted only)
//
// Hard rules:
//   - Keys must start with a letter or underscore; rest is [A-Za-z0-9_].
//   - Multi-line values are NOT supported. Use \n inside a double-quoted
//     value if you need a newline character.
//   - Inline comments after an UNQUOTED value are preserved as part of
//     the value — write a quoted value if you need to embed `#`.
//   - On a malformed line the parser returns an error with line number.
//
// See Expand for the variable-expansion semantics and the hardening
// (cycle detection, depth cap, undefined-as-empty, \$ escape).
package dotenv

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"unicode"
)

// Parse reads .env content from r and returns the parsed key/value
// map. Variable expansion is performed on double-quoted values using
// already-parsed keys (later lines see earlier lines) — for the full
// rules including os.Environ fallback, use Expand explicitly.
//
// Duplicate keys: last wins.
func Parse(r io.Reader) (map[string]string, error) {
	out := map[string]string{}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024), 1<<20) // up to 1MB lines
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		raw := scanner.Text()
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// Optional `export` prefix (so .env can also be sourced from a shell).
		if rest, ok := strings.CutPrefix(trimmed, "export "); ok {
			trimmed = strings.TrimSpace(rest)
		} else if rest, ok := strings.CutPrefix(trimmed, "export\t"); ok {
			trimmed = strings.TrimSpace(rest)
		}
		eq := strings.IndexByte(trimmed, '=')
		if eq < 0 {
			return nil, fmt.Errorf("dotenv: line %d: missing '=' in %q", lineNum, raw)
		}
		key := strings.TrimSpace(trimmed[:eq])
		if !isValidKey(key) {
			return nil, fmt.Errorf("dotenv: line %d: invalid key %q", lineNum, key)
		}
		valRaw := strings.TrimSpace(trimmed[eq+1:])
		val, err := parseValue(valRaw, out)
		if err != nil {
			return nil, fmt.Errorf("dotenv: line %d: %w", lineNum, err)
		}
		out[key] = val
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("dotenv: scanner: %w", err)
	}
	return out, nil
}

func isValidKey(k string) bool {
	if k == "" {
		return false
	}
	for i, r := range k {
		if i == 0 {
			if !(unicode.IsLetter(r) || r == '_') {
				return false
			}
			continue
		}
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_') {
			return false
		}
	}
	return true
}

// parseValue interprets a value clause based on its leading quote (if
// any) and applies ${VAR} expansion in double-quoted strings. seen is
// the running map of previously-parsed keys from the same file.
func parseValue(v string, seen map[string]string) (string, error) {
	if v == "" {
		return "", nil
	}
	switch v[0] {
	case '"':
		if len(v) < 2 || v[len(v)-1] != '"' {
			return "", fmt.Errorf("unterminated double-quoted value")
		}
		inner, err := unescapeDouble(v[1 : len(v)-1])
		if err != nil {
			return "", err
		}
		return Expand(inner, seen, nil), nil
	case '\'':
		if len(v) < 2 || v[len(v)-1] != '\'' {
			return "", fmt.Errorf("unterminated single-quoted value")
		}
		// Single quotes: verbatim, no escapes, no expansion.
		return v[1 : len(v)-1], nil
	default:
		return v, nil
	}
}

// unescapeDouble interprets the standard double-quoted escapes:
//
//	\n  → newline
//	\t  → tab
//	\r  → carriage return
//	\"  → literal "
//	\\  → literal \
//
// `\$` is INTENTIONALLY left as-is; the expander downstream knows to
// treat `\$` as a literal `$` that does NOT start a `${...}` lookup.
// Doing the strip here would let `\${VAR}` get expanded a moment
// later — exactly the opposite of what the author meant.
//
// Anything else after `\` is left verbatim (the `\` is kept).
func unescapeDouble(s string) (string, error) {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' {
			b.WriteByte(c)
			continue
		}
		if i == len(s)-1 {
			return "", fmt.Errorf("trailing backslash in double-quoted value")
		}
		nxt := s[i+1]
		switch nxt {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case '"', '\\':
			b.WriteByte(nxt)
		default:
			// Includes '$' — preserved verbatim (with leading \) so
			// the expander can recognise it as an escape.
			b.WriteByte('\\')
			b.WriteByte(nxt)
		}
		i++ // consumed the escape char
	}
	return b.String(), nil
}
