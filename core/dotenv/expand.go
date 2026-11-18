package dotenv

import (
	"errors"
	"strings"
)

// Expand substitutes ${NAME} in s from local, then envFn (which may be nil); undefined names are empty,
// looked-up values go in verbatim, \$ is a literal $, and bare $NAME or an unclosed ${ is left as is.
func Expand(s string, local map[string]string, envFn func(string) (string, bool)) string {
	e := &expander{local: local, envFn: envFn, limit: -1}
	e.expand(s)
	return e.b.String()
}

var errTrailingBackslash = errors.New("trailing backslash in double-quoted value")

func expandDouble(s string, src *sources, limit int) (out string, ok bool, err error) {
	e := &expander{local: src.local, limit: limit, quoted: true, src: src}
	e.expand(s)
	if e.err != nil {
		return "", true, e.err
	}
	if e.over {
		return "", false, nil
	}
	return e.b.String(), true, nil
}

type expander struct {
	local  map[string]string
	envFn  func(string) (string, bool)
	limit  int
	quoted bool
	src    *sources
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
		if i+1 < len(s) && s[i] == '\\' && s[i+1] == '$' {
			e.write(s[lit:i])
			e.write("$")
			i += 2
			lit = i
			continue
		}
		if i+1 < len(s) && s[i] == '$' && s[i+1] == '{' {
			end := strings.IndexByte(s[i+2:], '}')
			if end < 0 {
				if !e.quoted {
					break
				}
				i++
				continue
			}
			e.write(s[lit:i])
			name := s[i+2 : i+2+end]
			if name == "" {
				e.write("${}")
			} else {
				e.lookup(name)
			}
			i = i + 2 + end + 1
			lit = i
			continue
		}
		i++
	}
	if e.err == nil {
		e.write(s[lit:])
	}
}

func (e *expander) lookup(name string) {
	if e.src != nil {
		e.lookupSources(name)
		return
	}
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

func (e *expander) lookupSources(name string) {
	src := e.src
	outside := func(v string) {
		if !src.external[name] {
			src.external[name] = true
			src.credited += len(v)
			if e.limit >= 0 {
				e.limit += expansionFactor * len(v)
			}
		}
		e.write(v)
	}
	if src.envFn != nil {
		//gofastr:allow(recovercallback) envFn is os.LookupEnv from Load, called synchronously on the caller's goroutine; no dispatch loop runs it
		if v, ok := src.envFn(name); ok {
			outside(v)
			return
		}
	}
	if v, ok := src.shadow[name]; ok {
		outside(v)
		return
	}
	if v, ok := src.local[name]; ok {
		e.write(v)
	}
}
