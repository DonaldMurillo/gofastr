package uihost

import (
	_ "embed"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed fullbundle.js
var fullBundleJS string

var _ = registry.RegisterBehavior("fullbundle", fullBundleJS, registry.Markers("[data-cui-bundle-href]"))

const subsetPrefix = "fui-"

type subsetKey struct {
	bundleKey
	present string
}

func (ds *UIHost) subsetBlockFor(b bundleBlock, markup ...string) (bundleBlock, bool) {
	if len(b.droppable) == 0 || b.fullURL == "" {
		return bundleBlock{}, false
	}
	have := ds.behaviorClassTokens()
	for _, m := range markup {
		have = classTokens(m, have)
	}
	present := make([]byte, len(b.droppable))
	missing := false
	for i, t := range b.droppable {
		if _, ok := have[t]; ok {
			present[i] = '1'
		} else {
			present[i], missing = '0', true
		}
	}
	if !missing {
		return bundleBlock{}, false
	}
	key := subsetKey{bundleKey: b.key, present: string(present)}
	c := &ds.firstPaint
	if v, ok := c.subsets.Load(key); ok {
		blk := v.(bundleBlock)
		return blk, blk.ok
	}
	css, ok := pruneCSS(b.body, func(t string) bool { _, ok := have[t]; return ok })
	out := bundleBlock{names: b.names, key: b.key, href: b.fullURL}
	if ok {
		out.inlineBlock, out.ok = newInlineBlock(css)
	}
	if c.nSubsets.Load() < maxCachedBundles {
		if _, loaded := c.subsets.LoadOrStore(key, out); !loaded {
			c.nSubsets.Add(1)
		}
	}
	return out, out.ok
}

func (ds *UIHost) behaviorClassTokens() map[string]struct{} {
	c := &ds.firstPaint
	c.behaviorOnce.Do(func() {
		c.behaviorTokens = map[string]struct{}{}
		for _, e := range registry.Behaviors() {
			for t := range classTokens(e.Source, nil) {
				c.behaviorTokens[t] = struct{}{}
			}
		}
	})
	return c.behaviorTokens
}

func classTokens(s string, seed map[string]struct{}) map[string]struct{} {
	out := make(map[string]struct{}, len(seed)+64)
	for t := range seed {
		out[t] = struct{}{}
	}
	for {
		i := strings.Index(s, subsetPrefix)
		if i < 0 {
			return out
		}
		j := i + len(subsetPrefix)
		for j < len(s) && isClassByte(s[j]) {
			j++
		}
		out[s[i:j]] = struct{}{}
		s = s[j:]
	}
}

func isClassByte(c byte) bool {
	return c == '-' || c == '_' || '0' <= c && c <= '9' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}

func droppableClasses(css string) []string {
	seen := map[string]struct{}{}
	var out []string
	if _, ok := pruneCSS(css, func(t string) bool {
		if _, dup := seen[t]; !dup {
			seen[t] = struct{}{}
			out = append(out, t)
		}
		return false
	}); !ok {
		return nil
	}
	return out
}

var groupAtRules = map[string]bool{
	"@media": true, "@supports": true, "@container": true, "@layer": true, "@scope": true,
}

func pruneCSS(css string, has func(string) bool) (string, bool) {
	var b strings.Builder
	end, ok := pruneBlock(css, 0, has, &b)
	if !ok || end != len(css) {
		return "", false
	}
	return b.String(), true
}

func pruneBlock(css string, i int, has func(string) bool, out *strings.Builder) (int, bool) {
	for {
		i = skipSpaceAndComments(css, i)
		if i >= len(css) || css[i] == '}' {
			return i, true
		}
		start := i
		j, ok := scanPrelude(css, i)
		if !ok {
			return 0, false
		}
		if css[j] == ';' { // a statement: @import, @layer a, b;
			out.WriteString(css[start : j+1])
			i = j + 1
			continue
		}
		if j == start {
			return 0, false
		}
		prelude := css[start:j]
		close, ok := matchBrace(css, j)
		if !ok {
			return 0, false
		}
		if prelude[0] == '@' {
			name := prelude
			if k := strings.IndexAny(name, " \t\n\r\f({"); k >= 0 {
				name = name[:k]
			}
			if groupAtRules[strings.ToLower(name)] {
				var inner strings.Builder
				end, ok := pruneBlock(css, j+1, has, &inner)
				if !ok || end != close {
					return 0, false
				}
				if inner.Len() > 0 || strings.EqualFold(name, "@layer") {
					out.WriteString(prelude)
					out.WriteByte('{')
					out.WriteString(inner.String())
					out.WriteByte('}')
				}
			} else {
				out.WriteString(css[start : close+1])
			}
		} else if selectorMayMatch(prelude, has) {
			out.WriteString(css[start : close+1])
		}
		i = close + 1
	}
}

func selectorMayMatch(sel string, has func(string) bool) bool {
	depth, alive := 0, true
	for i := 0; i < len(sel); i++ {
		switch c := sel[i]; c {
		case '\\':
			return true
		case '"', '\'':
			i = skipString(sel, i) - 1
		case '/':
			if i+1 < len(sel) && sel[i+1] == '*' {
				i = skipSpaceAndComments(sel, i) - 1
			}
		case '(', '[':
			depth++
		case ')', ']':
			depth--
		case ',':
			if depth == 0 {
				if alive {
					return true
				}
				alive = true
			}
		case '.':
			if depth > 0 || !strings.HasPrefix(sel[i+1:], subsetPrefix) {
				continue
			}
			j := i + 1 + len(subsetPrefix)
			for j < len(sel) && isClassByte(sel[j]) {
				j++
			}
			if j < len(sel) && (sel[j] == '\\' || sel[j] >= 0x80) {
				return true
			}
			if !has(sel[i+1 : j]) {
				alive = false
			}
			i = j - 1
		}
	}
	return alive
}

func scanPrelude(css string, i int) (int, bool) {
	for i < len(css) {
		switch css[i] {
		case '{', ';':
			return i, true
		case '}':
			return 0, false
		case 'u', 'U':
			i = max(urlEnd(css, i), i+1)
		case '\\':
			i += 2
		case '"', '\'':
			i = skipString(css, i)
		case '/':
			if i+1 < len(css) && css[i+1] == '*' {
				i = skipSpaceAndComments(css, i)
			} else {
				i++
			}
		default:
			i++
		}
	}
	return 0, false
}

func matchBrace(css string, open int) (int, bool) {
	depth := 0
	for i := open; i < len(css); {
		switch css[i] {
		case '{':
			depth++
			i++
		case '}':
			depth--
			if depth == 0 {
				return i, true
			}
			i++
		case 'u', 'U':
			i = max(urlEnd(css, i), i+1)
		case '\\':
			i += 2
		case '"', '\'':
			i = skipString(css, i)
		case '/':
			if i+1 < len(css) && css[i+1] == '*' {
				i = skipSpaceAndComments(css, i)
			} else {
				i++
			}
		default:
			i++
		}
	}
	return 0, false
}

func urlEnd(s string, i int) int {
	if len(s)-i < 4 || !strings.EqualFold(s[i:i+4], "url(") || i > 0 && isClassByte(s[i-1]) {
		return i
	}
	j := skipSpaceAndComments(s, i+4)
	if j < len(s) && (s[j] == '"' || s[j] == '\'') {
		return i
	}
	for j = i + 4; j < len(s); j++ {
		switch s[j] {
		case '\\':
			j++
		case ')':
			return j + 1
		}
	}
	return len(s)
}

func skipString(s string, i int) int {
	q := s[i]
	for i++; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case q:
			return i + 1
		}
	}
	return len(s)
}

func skipSpaceAndComments(s string, i int) int {
	for i < len(s) {
		switch {
		case s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r' || s[i] == '\f':
			i++
		case strings.HasPrefix(s[i:], "/*"):
			k := strings.Index(s[i+2:], "*/")
			if k < 0 {
				return len(s)
			}
			i += k + 4
		default:
			return i
		}
	}
	return i
}
