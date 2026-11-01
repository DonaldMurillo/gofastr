package dotenv

import (
	"errors"
	"strings"
)

// Expand performs ${VAR} substitution in s, drawing values from local
// first then envFn. Bracket form ONLY. Bare $VAR is left verbatim
// (less ambiguous, fewer footguns).
//
// Rules:
//   - A looked-up value goes in verbatim, the way a shell substitutes
//     a variable: it is never expanded again. Parse stores each value
//     already resolved, so re-expanding it would turn a single-quoted
//     literal or an escaped `\$` back into a lookup; an environment
//     value holding `${x}` is data, not a template. A self-reference
//     (`A=${A}`) therefore yields A's stored text, and no cycle can form.
//   - Undefined names → empty string (consistent with shell behavior).
//   - `\$` is an escape: it emits a literal `$` that starts no lookup.
//   - Malformed `${...` without closing `}` is left verbatim, better
//     than silently dropping bytes.
//
// envFn may be nil. In that case the only lookup source is local.
func Expand(s string, local map[string]string, envFn func(string) (string, bool)) string {
	e := &expander{local: local, envFn: envFn, limit: -1}
	e.expand(s)
	return e.b.String()
}

// errTrailingBackslash reports a double-quoted value ending in an
// unpaired backslash.
var errTrailingBackslash = errors.New("trailing backslash in double-quoted value")

// expandDouble interprets the inside of a double-quoted .env value in
// one pass: the escapes \n \t \r \" \\ \$ and ${VAR} lookups against
// local. One pass is what keeps `\\${DIR}` a backslash followed by an
// expansion: unescaping first turned `\\` into `\`, which the expander
// then paired with the `$` as an escape. Any other character after `\`
// is kept with its backslash.
//
// It stops writing as soon as the value would pass limit bytes and
// reports ok=false: Parse's expansion budget refuses a value while it is
// being built, not after a single line of many references to a large
// key has already allocated gigabytes.
func expandDouble(s string, local map[string]string, limit int) (out string, ok bool, err error) {
	e := &expander{local: local, limit: limit, quoted: true}
	e.expand(s)
	if e.err != nil {
		return "", true, e.err
	}
	if e.over {
		return "", false, nil
	}
	return e.b.String(), true, nil
}

// expander writes an expansion into one builder, so the cap counts each
// output byte once, at the point it is written.
type expander struct {
	local  map[string]string
	envFn  func(string) (string, bool)
	limit  int
	quoted bool // interpret the double-quoted escapes (expandDouble)
	over   bool
	err    error
	b      strings.Builder
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

func (e *expander) expand(s string) {
	i, lit := 0, 0
	for i < len(s) && !e.over {
		if s[i] == '\\' && e.quoted {
			if i+1 == len(s) {
				e.err = errTrailingBackslash
				return
			}
			e.write(s[lit:i])
			switch nxt := s[i+1]; nxt {
			case 'n':
				e.write("\n")
			case 't':
				e.write("\t")
			case 'r':
				e.write("\r")
			case '"', '\\', '$':
				e.write(string(nxt))
			default:
				e.write(s[i : i+2])
			}
			i += 2
			lit = i
			continue
		}
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
				// No closing brace: the rest is verbatim, except that a
				// double-quoted value still has its escapes to read.
				if !e.quoted {
					break
				}
				i++
				continue
			}
			e.write(s[lit:i])
			name := s[i+2 : i+2+end]
			if name == "" {
				// Empty ${}: keep literal.
				e.write("${}")
			} else {
				e.lookup(name)
			}
			i = i + 2 + end + 1 // past the '}'
			lit = i
			continue
		}
		i++
	}
	if e.err == nil {
		e.write(s[lit:])
	}
}

// lookup writes name's value verbatim (see Expand).
func (e *expander) lookup(name string) {
	if v, ok := e.local[name]; ok {
		e.write(v)
		return
	}
	if e.envFn != nil {
		//gofastr:allow(recovercallback) envFn is Expand's caller-supplied lookup (os.LookupEnv in practice), called synchronously on the caller's goroutine exactly as it was as a parameter; no dispatch loop runs it
		if v, ok := e.envFn(name); ok {
			e.write(v)
		}
	}
}
