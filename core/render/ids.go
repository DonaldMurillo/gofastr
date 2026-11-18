package render

import (
	"context"
	"html"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"
)

type idScopeKey struct{}

type idScope struct {
	mu      sync.Mutex
	taken   map[string]bool
	last    map[string]int
	rec     *regionLog
	ns      string
	outer   context.Context
	settled *settledLog
}

type settledLog struct {
	mu   sync.Mutex
	reps []*strings.Replacer
}

func newIDScope(parent *idScope) *idScope {
	s := &idScope{taken: map[string]bool{}, last: map[string]int{}}
	if parent != nil {
		s.settled = parent.settled
	} else {
		s.settled = &settledLog{}
	}
	return s
}

type regionLog struct {
	prefix string
	ops    []idOp
}

type idOp struct {
	id       string
	children []string
	reserve  bool
	split    bool
}

const tokenPrefix = "gofastr-idref-"

func tokenSafe(id string, children []string) bool {
	for _, s := range append([]string{id}, children...) {
		for i := 0; i < len(s); i++ {
			c := s[i]
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
				return false
			}
		}
	}
	return true
}

// WithIDScope returns ctx carrying a fresh id scope; the host installs one per page render.
func WithIDScope(ctx context.Context) context.Context {
	return context.WithValue(ctx, idScopeKey{}, newIDScope(nil))
}

// WithIDNamespace returns ctx carrying a fresh scope whose derived ids read "<encoded ns>--<id>", suffixed within the namespace and reserved in ctx's scope too; an empty ns gives a plain fresh scope.
func WithIDNamespace(ctx context.Context, ns string) context.Context {
	s := newIDScope(scopeOf(ctx))
	s.ns, s.outer = encodeNamespace(ns), ctx
	return context.WithValue(ctx, idScopeKey{}, s)
}

// WithIDNamespaceKind is [WithIDNamespace] with the namespace "_<kind>-<encoded ns>", apart from every plain one; it panics unless kind is g to z and ns is non-empty.
func WithIDNamespaceKind(ctx context.Context, kind byte, ns string) context.Context {
	if kind < 'g' || kind > 'z' {
		panic("render: WithIDNamespaceKind kind must be a letter from g to z")
	}
	if ns == "" {
		panic("render: WithIDNamespaceKind needs a namespace")
	}
	s := newIDScope(scopeOf(ctx))
	s.ns, s.outer = "_"+string(kind)+"-"+encodeNamespace(ns), ctx
	return context.WithValue(ctx, idScopeKey{}, s)
}

func encodeNamespace(ns string) string {
	alnum := func(c byte) bool {
		return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
	}
	var b strings.Builder
	for i := 0; i < len(ns); i++ {
		c := ns[i]
		switch {
		case alnum(c):
			b.WriteByte(c)
		case c == '-' && i > 0 && alnum(ns[i-1]) && i+1 < len(ns) && alnum(ns[i+1]):
			b.WriteByte(c)
		default:
			const hex = "0123456789abcdef"
			b.WriteByte('_')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&15])
		}
	}
	return b.String()
}

// UniqueID returns id, or id-2, id-3 on a repeat in ctx's scope, taking id and each id+child; with no scope or an empty id it returns id, and in a region a placeholder Settle replaces.
func UniqueID(ctx context.Context, id string, children ...string) string {
	s := scopeOf(ctx)
	if s == nil || id == "" {
		return id
	}
	if s.rec != nil && tokenSafe(id, children) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.rec.ops = append(s.rec.ops, idOp{id: id, children: children})
		return s.rec.prefix + strconv.Itoa(len(s.rec.ops)-1) + "z"
	}
	pre, suf := s.split(id, children)
	return pre + id + suf
}

func (s *idScope) split(id string, children []string) (pre, suf string) {
	s.mu.Lock()
	if s.rec != nil {
		k := strconv.Itoa(len(s.rec.ops))
		s.rec.ops = append(s.rec.ops, idOp{id: id, children: children, split: true})
		s.mu.Unlock()
		return s.rec.prefix + k + "p", s.rec.prefix + k + "s"
	}
	if s.ns != "" {
		pre = s.ns + "--"
	}
	final := s.next(pre+id, children)
	outer := s.outer
	s.mu.Unlock()
	if outer != nil {
		ReserveID(outer, final, children...)
	}
	return pre, final[len(pre)+len(id):]
}

func (s *idScope) free(id string, children []string) bool {
	if s.taken[id] {
		return false
	}
	for _, c := range children {
		if s.taken[id+c] {
			return false
		}
	}
	return true
}

func (s *idScope) take(id string, children []string) {
	s.taken[id] = true
	for _, c := range children {
		s.taken[id+c] = true
	}
}

func (s *idScope) next(id string, children []string) string {
	if s.last[id] == 0 && s.free(id, children) {
		s.last[id] = 1
		s.take(id, children)
		return id
	}
	n := max(s.last[id], 1)
	for {
		n++
		cand := id + "-" + strconv.Itoa(n)
		if s.free(cand, children) {
			s.last[id] = n
			s.take(cand, children)
			return cand
		}
	}
}

// ReserveID records an explicit id and its children in ctx's scope unchanged, so a derived id that would repeat one is suffixed.
func ReserveID(ctx context.Context, id string, children ...string) {
	s := scopeOf(ctx)
	if s == nil || id == "" || containsToken(id) {
		return
	}
	s.mu.Lock()
	s.take(id, children)
	if s.rec != nil {
		s.rec.ops = append(s.rec.ops, idOp{id: id, children: children, reserve: true})
	}
	outer := s.outer
	s.mu.Unlock()
	if outer != nil {
		ReserveID(outer, id, children...)
	}
}

// HasIDScope reports whether ctx carries an id scope.
func HasIDScope(ctx context.Context) bool { return scopeOf(ctx) != nil }

func scopeOf(ctx context.Context) *idScope {
	if ctx == nil {
		return nil
	}
	s, _ := ctx.Value(idScopeKey{}).(*idScope)
	return s
}

var batchSeq atomic.Uint64

// RegionBatch settles the ids of n concurrently rendered regions: render region i with Context(i), then call Settle for each region in order.
type RegionBatch struct {
	parent   context.Context
	regions  []*idScope
	reserved bool
}

// NewRegionBatch prepares n regions under ctx's scope; with no scope every region renders with ctx and Settle changes nothing.
func NewRegionBatch(ctx context.Context, n int) *RegionBatch {
	b := &RegionBatch{parent: ctx}
	if scopeOf(ctx) == nil {
		return b
	}
	seq := strconv.FormatUint(batchSeq.Add(1), 10)
	b.regions = make([]*idScope, n)
	for i := range b.regions {
		s := newIDScope(scopeOf(ctx))
		s.rec = &regionLog{prefix: tokenPrefix + seq + "x" + strconv.Itoa(i) + "x"}
		b.regions[i] = s
	}
	return b
}

// Context returns the render context of region i.
func (b *RegionBatch) Context(i int) context.Context {
	if b.regions == nil {
		return b.parent
	}
	return context.WithValue(b.parent, idScopeKey{}, b.regions[i])
}

// Settle hands region i's ids out from the parent scope, explicit ids of every region first on the first call, and returns the replacer for region i's output; call it once per region, in order, after all have rendered.
func (b *RegionBatch) Settle(i int) func(HTML) HTML {
	keep := func(h HTML) HTML { return h }
	if b.regions == nil {
		return keep
	}
	if !b.reserved {
		b.reserved = true
		for _, r := range b.regions {
			for _, op := range r.record() {
				if op.reserve {
					ReserveID(b.parent, op.id, op.children...)
				}
			}
		}
	}
	r := b.regions[i]
	var pairs []string
	for k, op := range r.record() {
		tok := r.rec.prefix + strconv.Itoa(k)
		switch {
		case op.reserve:
		case op.split:
			pre, suf := scopeOf(b.parent).split(op.id, op.children)
			pairs = append(pairs, tok+"p", pre, tok+"s", suf)
		default:
			pairs = append(pairs, tok+"z", UniqueID(b.parent, op.id, op.children...))
		}
	}
	if len(pairs) == 0 {
		return keep
	}
	rep := strings.NewReplacer(pairs...)
	log := r.settled
	log.mu.Lock()
	log.reps = append(log.reps, rep)
	log.mu.Unlock()
	return func(h HTML) HTML { return HTML(rep.Replace(string(h))) }
}

// ReserveRendered reserves for region i every id attribute written in hs; call it before the first Settle, after which it reserves in the parent scope at once.
func (b *RegionBatch) ReserveRendered(i int, hs ...HTML) {
	if b.regions == nil {
		return
	}
	r := b.regions[i]
	for _, h := range hs {
		for _, id := range writtenIDs(string(h)) {
			if b.reserved {
				ReserveID(b.parent, id)
				continue
			}
			r.mu.Lock()
			r.rec.ops = append(r.rec.ops, idOp{id: id, reserve: true})
			r.mu.Unlock()
		}
	}
}

func writtenIDs(s string) []string {
	var out []string
	for i := 0; i < len(s); {
		lt := strings.IndexByte(s[i:], '<')
		if lt < 0 {
			return out
		}
		i += lt
		switch {
		case strings.HasPrefix(s[i:], "<!--"):
			end := strings.Index(s[i+4:], "-->")
			if end < 0 {
				return out
			}
			i += 4 + end + 3
		case i+1 < len(s) && isASCIILetter(s[i+1]):
			j := i + 1
			for j < len(s) && !isAttrSpace(s[j]) && s[j] != '>' && s[j] != '/' {
				j++
			}
			name := s[i+1 : j]
			var ids []string
			i, ids = readAttrs(s, j)
			out = append(out, ids...)
			if asciiEqualFold(name, "plaintext") {
				return out
			}
			for _, raw := range []string{"script", "style", "textarea", "title", "iframe", "noembed", "noframes", "xmp"} {
				if asciiEqualFold(name, raw) {
					i = skipRawText(s, i, raw)
					break
				}
			}
		default:
			gt := strings.IndexByte(s[i+1:], '>')
			if gt < 0 {
				return out
			}
			i += 1 + gt + 1
		}
	}
	return out
}

func readAttrs(s string, k int) (int, []string) {
	var ids []string
	for k < len(s) {
		for k < len(s) && isAttrSpace(s[k]) {
			k++
		}
		if k >= len(s) {
			break
		}
		if s[k] == '>' {
			return k + 1, ids
		}
		if s[k] == '/' {
			k++
			continue
		}
		start := k
		for k < len(s) && !isAttrSpace(s[k]) && s[k] != '=' && s[k] != '>' && s[k] != '/' {
			k++
		}
		name := s[start:k]
		for k < len(s) && isAttrSpace(s[k]) {
			k++
		}
		val, has := "", false
		if k < len(s) && s[k] == '=' {
			k++
			for k < len(s) && isAttrSpace(s[k]) {
				k++
			}
			has = true
			if k < len(s) && (s[k] == '"' || s[k] == '\'') {
				q := s[k]
				end := strings.IndexByte(s[k+1:], q)
				if end < 0 {
					return len(s), ids
				}
				val, k = s[k+1:k+1+end], k+1+end+1
			} else {
				v := k
				for k < len(s) && !isAttrSpace(s[k]) && s[k] != '>' {
					k++
				}
				val = s[v:k]
			}
		}
		if has && asciiEqualFold(name, "id") && val != "" && !containsToken(val) {
			ids = append(ids, unescapeAttr(val))
		}
	}
	return len(s), ids
}

func unescapeAttr(s string) string {
	if !strings.Contains(s, "&") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '&' {
			b.WriteByte(s[i])
			i++
			continue
		}
		k := i + 1
		if k < len(s) && s[k] == '#' {
			if r, n := numericRef(s[i:]); n > 0 {
				b.WriteRune(r)
				i += n
			} else {
				b.WriteByte('&')
				i++
			}
			continue
		}
		for k < len(s) && (isDigit(s[k]) || (s[k]|0x20) >= 'a' && (s[k]|0x20) <= 'z') {
			k++
		}
		ref, whole := s[i:k], 1
		if k < len(s) && s[k] == ';' {
			ref, whole = s[i:k+1], 2
		} else if k < len(s) && s[k] == '=' {
			whole = 0
		}
		if u := html.UnescapeString(ref); k > i+1 && u != ref && utf8.RuneCountInString(u) <= whole {
			b.WriteString(u)
		} else {
			b.WriteString(ref)
		}
		i += len(ref)
	}
	return b.String()
}

func numericRef(s string) (rune, int) {
	k := 2
	hex := k < len(s) && (s[k] == 'x' || s[k] == 'X')
	if hex {
		k++
	}
	start, x := k, rune(0)
	for ; k < len(s); k++ {
		var d rune
		switch c := s[k]; {
		case isDigit(c):
			d = rune(c - '0')
		case hex && isHexLetter(c):
			d = rune(c|0x20-'a') + 10
		default:
			goto done
		}
		if x <= utf8.MaxRune {
			if hex {
				x = x*16 + d
			} else {
				x = x*10 + d
			}
		}
	}
done:
	if k == start {
		return 0, 0
	}
	if k < len(s) && s[k] == ';' {
		k++
	}
	switch {
	case x == 0 || x >= 0xD800 && x <= 0xDFFF || x > utf8.MaxRune:
		return utf8.RuneError, k
	case x >= 0x80 && x <= 0x9F:
		r, _ := utf8.DecodeRuneInString(html.UnescapeString("&#" + strconv.Itoa(int(x)) + ";"))
		return r, k
	}
	return x, k
}

func isDigit(c byte) bool     { return c >= '0' && c <= '9' }
func isHexLetter(c byte) bool { return c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }

func skipRawText(s string, i int, name string) int {
	for {
		k := strings.Index(s[i:], "</")
		if k < 0 {
			return len(s)
		}
		at := i + k + 2
		end := at + len(name)
		if end <= len(s) && asciiEqualFold(s[at:end], name) && (end == len(s) || isAttrSpace(s[end]) || s[end] == '>' || s[end] == '/') {
			return i + k
		}
		i = at
	}
}

func isASCIILetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

func asciiEqualFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		x, y := a[i], b[i]
		if x >= 'A' && x <= 'Z' {
			x += 'a' - 'A'
		}
		if y >= 'A' && y <= 'Z' {
			y += 'a' - 'A'
		}
		if x != y {
			return false
		}
	}
	return true
}

func containsToken(s string) bool {
	digits := func(i int) int {
		j := i
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		if j == i {
			return -1
		}
		return j
	}
	for off := 0; ; {
		k := strings.Index(s[off:], tokenPrefix)
		if k < 0 {
			return false
		}
		i := off + k + len(tokenPrefix)
		off = i
		ok := true
		for n := 0; n < 3 && ok; n++ {
			if i = digits(i); i < 0 {
				ok = false
				break
			}
			sep := byte('x')
			if n == 2 {
				sep = 0
			}
			if sep != 0 {
				if i >= len(s) || s[i] != sep {
					ok = false
				} else {
					i++
				}
			}
		}
		if ok && i < len(s) && (s[i] == 'z' || s[i] == 'p' || s[i] == 's') {
			return true
		}
	}
}

func isAttrSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}

// SettleIDs replaces in s the placeholders of every region settled so far under ctx's scope, for a value that leaves the render other than as settled HTML.
func SettleIDs(ctx context.Context, s string) string {
	sc := scopeOf(ctx)
	if sc == nil || !strings.Contains(s, tokenPrefix) {
		return s
	}
	sc.settled.mu.Lock()
	reps := sc.settled.reps
	sc.settled.mu.Unlock()
	for _, r := range reps {
		s = r.Replace(s)
	}
	return s
}

func (s *idScope) record() []idOp {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rec.ops
}
