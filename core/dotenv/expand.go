package dotenv

import "strings"

// Expand performs ${VAR} substitution in s, drawing values from local
// first then envFn. Bracket form ONLY — bare $VAR is left verbatim
// (less ambiguous, fewer footguns).
//
// Hardening:
//   - Cycle detection via a visited-set; a self-reference (`A=${A}`)
//     or a multi-key cycle yields empty for the offending name.
//   - Depth cap (maxExpandDepth) to bound worst-case work.
//   - Undefined names → empty string (consistent with shell behavior).
//   - `\${...}` literal-escape is the parser's job, not Expand's:
//     unescapeDouble strips the `\$` to `$` so Expand sees just `$`,
//     which doesn't form a `${`-prefix and is therefore left alone.
//   - Malformed `${...` without closing `}` is left verbatim — better
//     than silently dropping bytes.
//
// envFn may be nil — in that case the only lookup source is local.
func Expand(s string, local map[string]string, envFn func(string) (string, bool)) string {
	return expandWithDepth(s, local, envFn, map[string]struct{}{}, 0)
}

const maxExpandDepth = 16

func expandWithDepth(s string, local map[string]string, envFn func(string) (string, bool), visited map[string]struct{}, depth int) string {
	if depth >= maxExpandDepth {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		// `\$` is an escape: emit a literal $ and DO NOT treat the
		// next char as part of an expansion marker. Order matters —
		// check this before the ${ probe below.
		if i+1 < len(s) && s[i] == '\\' && s[i+1] == '$' {
			b.WriteByte('$')
			i += 2
			continue
		}
		// Look for the next ${
		if i+1 < len(s) && s[i] == '$' && s[i+1] == '{' {
			end := strings.IndexByte(s[i+2:], '}')
			if end < 0 {
				// No closing brace — emit the rest verbatim.
				b.WriteString(s[i:])
				return b.String()
			}
			name := s[i+2 : i+2+end]
			if name == "" {
				// Empty ${} — keep literal.
				b.WriteString("${}")
			} else {
				b.WriteString(lookup(name, local, envFn, visited, depth))
			}
			i = i + 2 + end + 1 // past the '}'
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func lookup(name string, local map[string]string, envFn func(string) (string, bool), visited map[string]struct{}, depth int) string {
	if _, cycle := visited[name]; cycle {
		return ""
	}
	visited[name] = struct{}{}
	defer delete(visited, name)

	if v, ok := local[name]; ok {
		// Local values may themselves contain ${...} that wasn't
		// recursively expanded at parse-time (e.g. forward refs in
		// preceding lines). Expand again with the visited set so
		// cycles don't loop forever.
		return expandWithDepth(v, local, envFn, visited, depth+1)
	}
	if envFn != nil {
		if v, ok := envFn(name); ok {
			return expandWithDepth(v, local, envFn, visited, depth+1)
		}
	}
	return ""
}
