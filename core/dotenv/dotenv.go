// Package dotenv parses .env files into a map and applies them to the process environment without overwriting it.
package dotenv

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
)

var errExpansionBudget = errors.New("dotenv: expansion budget exceeded")

const expansionFactor = 10

// Parse reads .env content from r; double-quoted ${NAME} reads only keys set earlier in r, and a duplicate key keeps its last value.
func Parse(r io.Reader) (map[string]string, error) {
	return parse(r, nil, nil)
}

func parse(r io.Reader, shadow map[string]string, envFn func(string) (string, bool)) (map[string]string, error) {
	out := map[string]string{}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024), 1<<20)
	lineNum := 0
	consumed, expanded := 0, 0
	external := map[string]bool{}
	for scanner.Scan() {
		lineNum++
		raw := scanner.Text()
		if lineNum == 1 {
			raw = strings.TrimPrefix(raw, "\ufeff")
		}
		if strings.ContainsRune(raw, '\x00') {
			return nil, fmt.Errorf("dotenv: line %d: NUL bytes cannot be stored in the process environment", lineNum)
		}
		consumed += len(raw) + 1
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if rest, ok := strings.CutPrefix(trimmed, "export "); ok {
			trimmed = strings.TrimSpace(rest)
		} else if rest, ok := strings.CutPrefix(trimmed, "export\t"); ok {
			trimmed = strings.TrimSpace(rest)
		}
		eq := strings.IndexByte(trimmed, '=')
		if eq < 0 {
			return nil, fmt.Errorf("dotenv: line %d: missing '='", lineNum)
		}
		key := strings.TrimSpace(trimmed[:eq])
		if !isValidKey(key) {
			return nil, fmt.Errorf("dotenv: line %d: invalid key (want [A-Za-z_][A-Za-z0-9_]*)", lineNum)
		}
		valRaw := strings.TrimSpace(trimmed[eq+1:])
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

type sources struct {
	local    map[string]string
	shadow   map[string]string
	envFn    func(string) (string, bool)
	external map[string]bool
	credited int
}

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
		return v[1 : len(v)-1], nil
	default:
		return v, nil
	}
}
