package dotenv

import "strings"

// Expand performs ${VAR} substitution in s, drawing values from local
// first then envFn. Bracket form ONLY. Bare $VAR is left verbatim
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
//   - Malformed `${...` without closing `}` is left verbatim, better
//     than silently dropping bytes.
//
// envFn may be nil. In that case the only lookup source is local.
func Expand(s string, local map[string]string, envFn func(string) (string, bool)) string {
	out, _ := expandLimited(s, local, envFn, -1)
	return out
}

// expandLimited is Expand with an output cap: it stops writing as soon
// as the value would pass limit bytes and reports ok=false. A negative
// limit means no cap. Parse uses it so its expansion budget refuses a
// value while it is being built, not after a single line of many
// references to a large key has already allocated gigabytes.
func expandLimited(s string, local map[string]string, envFn func(string) (string, bool), limit int) (string, bool) {
	e := &expander{local: local, envFn: envFn, visited: map[string]struct{}{}, limit: limit}
	e.expand(s, 0)
	if e.over {
		return "", false
	}
	return e.b.String(), true
}

const maxExpandDepth = 16

// expander writes every level of a nested expansion into one builder, so
// the cap counts each output byte once, at the point it is written.
type expander struct {
	local   map[string]string
	envFn   func(string) (string, bool)
	visited map[string]struct{}
	limit   int
	over    bool
	b       strings.Builder
}

func (e *expander) write(s string) {
	if e.over {
		return
	}
	if e.limit >= 0 && e.b.Len()+len(s) > e.limit {
		e.over = true
		return
	}
	e.b.WriteString(s)
}

func (e *expander) expand(s string, depth int) {
	if depth >= maxExpandDepth {
		e.write(s)
		return
	}
	i, lit := 0, 0
	for i < len(s) && !e.over {
		// `\$` is an escape: emit a literal $ and DO NOT treat the
		// next char as part of an expansion marker. Order matters:
		// check this before the ${ probe below.
		if i+1 < len(s) && s[i] == '\\' && s[i+1] == '$' {
			e.write(s[lit:i])
			e.write("$")
			i += 2
			lit = i
			continue
		}
		// Look for the next ${
		if i+1 < len(s) && s[i] == '$' && s[i+1] == '{' {
			end := strings.IndexByte(s[i+2:], '}')
			if end < 0 {
				// No closing brace: emit the rest verbatim.
				break
			}
			e.write(s[lit:i])
			name := s[i+2 : i+2+end]
			if name == "" {
				// Empty ${}: keep literal.
				e.write("${}")
			} else {
				e.lookup(name, depth)
			}
			i = i + 2 + end + 1 // past the '}'
			lit = i
			continue
		}
		i++
	}
	e.write(s[lit:])
}

func (e *expander) lookup(name string, depth int) {
	if _, cycle := e.visited[name]; cycle {
		return
	}
	e.visited[name] = struct{}{}
	defer delete(e.visited, name)

	if v, ok := e.local[name]; ok {
		// Local values may themselves contain ${...} that wasn't
		// recursively expanded at parse-time (e.g. forward refs in
		// preceding lines). Expand again with the visited set so
		// cycles don't loop forever.
		e.expand(v, depth+1)
		return
	}
	if e.envFn != nil {
		//gofastr:allow(recovercallback) envFn is Expand/Parse's caller-supplied lookup (os.LookupEnv in practice), called synchronously on the caller's goroutine exactly as it was as a parameter; no dispatch loop runs it
		if v, ok := e.envFn(name); ok {
			e.expand(v, depth+1)
		}
	}
}
