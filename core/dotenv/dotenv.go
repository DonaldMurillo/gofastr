// Package dotenv parses .env files into a map and (optionally) applies
// them to the process environment. Designed for the framework's
// startup path so apps get familiar .env behavior without taking on a
// third-party dependency.
//
// Format (a strict subset of the de facto dotenv spec):
//
//	# comments and blank lines are allowed
//	FOO=bar                       # bareword value
//	QUOTED="hello world"          # double-quoted: escapes interpreted
//	LITERAL='hello\nworld'        # single-quoted: VERBATIM, no escapes
//	export PORT=8080              # optional `export` prefix tolerated
//	PATH_TPL="${HOME}/bin"        # ${VAR} expansion (double-quoted only)
//
// Hard rules:
//   - Keys must start with a letter or underscore; rest is [A-Za-z0-9_].
//   - Multi-line values are NOT supported. Use \n inside a double-quoted
//     value if you need a newline character.
//   - Inline comments after an UNQUOTED value are preserved as part of
//     the value. Write a quoted value if you need to embed `#`.
//   - On a malformed line the parser returns an error with line number.
//
// See Expand for the variable-expansion semantics (values substitute
// verbatim, undefined-as-empty, \$ escape).
package dotenv

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
)

// errExpansionBudget reports a double-quoted value whose ${...}
// expansion would pass Parse's output budget.
var errExpansionBudget = errors.New("dotenv: expansion budget exceeded")

// expansionFactor is how many output bytes Parse admits per input byte;
// see the budget comment in parse.
const expansionFactor = 10

// Parse reads .env content from r and returns the parsed key/value
// map. Variable expansion is performed on double-quoted values using
// already-parsed keys (later lines see earlier lines) and nothing else:
// Parse reads no environment. Load adds the earlier files and the
// os.Environ fallback; Expand takes a custom lookup.
//
// Duplicate keys: last wins.
func Parse(r io.Reader) (map[string]string, error) {
	return parse(r, nil, nil)
}

// parse is Parse with the two lookup sources Load adds around the
// file's own keys. A ${NAME} reads shadow first (the values a
// higher-precedence file already set, which are the values Load keeps),
// then the keys parsed earlier in this file, then envFn (os.LookupEnv).
// Either may be nil.
//
// A value read from shadow or envFn did not come from this file, so it
// counts as input for the expansion budget, once per name: the budget
// stops this file's lines multiplying each other, and one ${PATH} in a
// two-line file is not that. Every copy written still counts as output.
func parse(r io.Reader, shadow map[string]string, envFn func(string) (string, bool)) (map[string]string, error) {
	out := map[string]string{}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024), 1<<20) // up to 1MB lines
	lineNum := 0
	// Expansion budget. ${VAR} references resolve against keys defined
	// earlier in the same file, so each line can multiply the one before
	// it: k references per line over n lines is k^n bytes of output from
	// an input where every line is far under the scanner's 1 MiB cap.
	// Twelve lines of three references is already half a megabyte from
	// ~200 bytes; twenty lines is the OOM. Every app boot parses .env
	// files from the process working directory, so this decides how long
	// startup takes. A legitimate chain of references stays under 3x its
	// input, and a literal value can never exceed the line that carries
	// it, so 10x total is generous headroom that a bomb cannot fit in.
	consumed, expanded := 0, 0
	external := map[string]bool{}
	for scanner.Scan() {
		lineNum++
		raw := scanner.Text()
		if strings.ContainsRune(raw, '\x00') {
			return nil, fmt.Errorf("dotenv: line %d: NUL bytes cannot be stored in the process environment", lineNum)
		}
		consumed += len(raw) + 1
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
			// The line number locates the problem; the line itself is a
			// secret. A .env holds credentials by definition, and hosts
			// wrap LoadAndApply in log.Fatal, so echoing the raw line
			// puts "API_TOKEN sk-live-..." (the exact typo this branch
			// catches) into stderr. The sibling branches below already
			// name only the key or the shape; match them.
			return nil, fmt.Errorf("dotenv: line %d: missing '='", lineNum)
		}
		key := strings.TrimSpace(trimmed[:eq])
		if !isValidKey(key) {
			return nil, fmt.Errorf("dotenv: line %d: invalid key (want [A-Za-z_][A-Za-z0-9_]*)", lineNum)
		}
		valRaw := strings.TrimSpace(trimmed[eq+1:])
		// The budget caps the value while it is built: checked only
		// afterwards, one line of 65536 ${A} references to a 64 KiB A
		// allocated 4 GiB before the comparison ran.
		src := &sources{local: out, shadow: shadow, envFn: envFn, external: external}
		budget := expansionFactor*consumed - expanded
		val, err := parseValue(valRaw, src, budget)
		consumed += src.credited
		budget += expansionFactor * src.credited
		if errors.Is(err, errExpansionBudget) || err == nil && len(val) > budget {
			return nil, fmt.Errorf("dotenv: line %d: ${...} expansion exceeded %dx the input size (over %d bytes of values from %d bytes read); a reference chain is multiplying",
				lineNum, expansionFactor, expansionFactor*consumed, consumed)
		}
		if err != nil {
			return nil, fmt.Errorf("dotenv: line %d: %w", lineNum, err)
		}
		out[key] = val
		expanded += len(val)
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
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '_') {
				return false
			}
			continue
		}
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_') {
			return false
		}
	}
	return true
}

// sources is where one value's ${NAME} lookups resolve; see parse.
type sources struct {
	local    map[string]string // keys parsed earlier in this file
	shadow   map[string]string // a higher-precedence file's values (Load)
	envFn    func(string) (string, bool)
	external map[string]bool // names already counted as input
	credited int             // input bytes credited during this value
}

// parseValue interprets a value clause based on its leading quote (if
// any) and applies ${VAR} expansion in double-quoted strings. src holds
// the running map of previously-parsed keys from the same file and the
// lookups around it.
//
// budget caps the expanded length of a double-quoted value; past it
// parseValue stops building and returns errExpansionBudget.
func parseValue(v string, src *sources, budget int) (string, error) {
	if v == "" {
		return "", nil
	}
	switch v[0] {
	case '"':
		if len(v) < 2 || v[len(v)-1] != '"' {
			return "", fmt.Errorf("unterminated double-quoted value")
		}
		out, ok, err := expandDouble(v[1:len(v)-1], src, max(budget, 0))
		if err != nil {
			return "", err
		}
		if !ok {
			return "", errExpansionBudget
		}
		return out, nil
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
