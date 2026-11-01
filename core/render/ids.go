package render

import (
	"context"
	"html"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// A component that derives an element id from its content (a section
// slugged from its heading, a tab strip named after its signal) gives
// two same-titled instances on one page the same id: aria-labelledby
// and a #fragment then resolve to the first. The id scope on the render
// context hands each derived id out once per render and suffixes a
// repeat: "features", then "features-2".
//
// An id comes with its family: the ids a component builds from it
// ("features-title" for a section's heading, "<id>-tab-0" for a tab).
// The scope takes the whole family, so a later section titled
// "Features title" does not land on the first section's heading id.
//
// The host installs a scope once per page render (core-ui/app's
// WithRequest does). A component rendered with no scope on its context,
// or with no context, keeps the id it derived. An explicit id is never
// rewritten; ReserveID records it so a derived id that would repeat it
// is suffixed instead, even one handed out earlier in the same region.
//
// Regions (NewRegionBatch) defer the hand-out: a region records the ids
// it asks for and answers with placeholder tokens, and Settle replays
// the record once the region has rendered, explicit ids first.
// ReserveRendered adds the ids the region's HTML writes in id
// attributes, so a component that forwards an explicit id without
// calling ReserveID is covered too. An id
// with a byte an escaper may rewrite ("cart.view") is emitted as is
// between two tokens that Settle fills with its namespace and its
// repeat suffix, so it is settled across regions like the rest. Layout
// outlet fills render concurrently as regions settled in outlet order,
// so the output is the same bytes whichever goroutine finished first;
// core-ui/app renders a whole page as one region, so an explicit id
// wins over a derived one wherever the two sit. A value that leaves the
// render other than as the HTML a Settle rewrites (a signal seed) goes
// through SettleIDs, which replays every settle the request ran.

type idScopeKey struct{}

type idScope struct {
	mu    sync.Mutex
	taken map[string]bool
	last  map[string]int // base id -> the highest suffix handed out for it
	// rec is non-nil on a region of a RegionBatch: ids are recorded
	// and answered with placeholder tokens that Settle swaps for the
	// final ids.
	rec *regionLog
	// ns is set on a namespace scope (WithIDNamespace): derived ids are
	// "<ns>--<id>", and every id handed out or reserved is also
	// reserved in outer, the scope the namespace was opened in.
	ns    string
	outer context.Context
	// settled is the request's record of every placeholder Settle has
	// replaced, shared by every scope opened under the one WithIDScope
	// started (SettleIDs reads it).
	settled *settledLog
}

// settledLog keeps each Settle's replacer, in the order they ran: an
// inner region's tokens become its parent region's tokens before
// those become ids, so replaying them in order resolves both.
type settledLog struct {
	mu   sync.Mutex
	reps []*strings.Replacer
}

// newIDScope returns a scope under parent's settled log, or a new log
// with no parent scope.
func newIDScope(parent *idScope) *idScope {
	s := &idScope{taken: map[string]bool{}, last: map[string]int{}}
	if parent != nil {
		s.settled = parent.settled
	} else {
		s.settled = &settledLog{}
	}
	return s
}

// regionLog is one concurrent region's record of id requests.
type regionLog struct {
	prefix string // token prefix, unique to the batch and region
	ops    []idOp
}

type idOp struct {
	id       string
	children []string
	reserve  bool // an explicit id (ReserveID): kept as is
	// split marks an id no token can stand in for (see tokenSafe): the
	// region emitted it between two tokens, prefix and suffix, which
	// Settle fills with the namespace ("<ns>--" or nothing) and the
	// repeat suffix ("-2" or nothing).
	split bool
}

// tokenPrefix starts every placeholder token.
const tokenPrefix = "gofastr-idref-"

// tokenSafe reports whether every escaper a component might pass the
// id through (HTML attribute, JS string, URL path or query, CSS ident)
// leaves both the id and its placeholder token unchanged, so swapping
// the token for the final id after the render gives the bytes a
// sequential render would have produced.
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

// WithIDScope returns a context carrying a fresh id scope.
func WithIDScope(ctx context.Context) context.Context {
	return context.WithValue(ctx, idScopeKey{}, newIDScope(nil))
}

// WithIDNamespace returns a context carrying a fresh id scope for a
// part of the page that also renders on its own (an island an RPC
// handler re-renders, a deferred outlet a part request fills). A
// derived id in it is "<ns>--<id>" ("cart--features"), suffixed within
// the namespace only, so the part's ids are the same whether it renders
// inside the page or alone: render it under the same ns both times.
// ns is spelled in id-safe bytes first: letters, digits and single
// inner hyphens stay, any other byte is "_" and its hex ("cart.v2" is
// "cart_2ev2"), so no namespace puts markup in an attribute and two
// namespaces never meet in one id. A slug never holds "--", so a
// namespaced id does not meet a derived id of the page around it, and
// each id the namespace hands out or reserves is reserved in ctx's
// scope as well. Explicit ids are kept as they are. An empty ns gives a
// plain fresh scope.
func WithIDNamespace(ctx context.Context, ns string) context.Context {
	s := newIDScope(scopeOf(ctx))
	s.ns, s.outer = encodeNamespace(ns), ctx
	return context.WithValue(ctx, idScopeKey{}, s)
}

// WithIDNamespaceKind is WithIDNamespace for a second family of
// namespaces that must never meet the first (core-ui/app's deferred
// outlets, beside islands, whose ids nothing validates). The namespace
// reads "_<kind>-<encoded ns>" ("_l-shell-aside"): an encoded plain
// namespace holds "_" only before two hex digits, so with kind a letter
// from g to z no island id gives the same namespace. ns must not be
// empty.
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

// encodeNamespace spells ns in the bytes tokenSafe accepts, so a
// namespace (an island id, which nothing validates) never brings a
// quote or a space into an attribute, and so that "<ns>--" ends where
// the first "--" of a namespaced id is: ASCII letters and digits are
// kept, a hyphen is kept between two of them, and every other byte is
// "_" and two lowercase hex digits ("a--b" is "a_2d_2db"). The result
// holds no "--" and does not end in "-", and decoding it is unique, so
// two namespaces never give one id: island "a" deriving "b--c" is
// "a--b--c", island "a--b" deriving "c" is "a_2d_2db--c".
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

// UniqueID returns id the first time ctx's scope sees it and id-2,
// id-3, … on each repeat, skipping a suffixed id already taken. children
// are the suffixes of the ids the caller builds from the result
// ("-title" for "<id>-title"): the scope picks the first candidate whose
// whole family is free and takes all of it. With no scope (or a nil
// ctx) or an empty id it returns id unchanged. On a region of a
// RegionBatch it returns a placeholder that Settle replaces with the
// final id.
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

// split hands id out and returns what goes before and after it: the
// namespace ("<ns>--") and the repeat suffix ("-2"). On a region the
// two are tokens Settle fills, so an id no single token can stand in
// for (one with a dot, which an escaper may rewrite) still keeps the
// bytes its escaper gives it and is settled across regions like any
// other.
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

// next hands out id or its next free suffix. Callers hold s.mu.
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

// ReserveID records an explicit id and its family (see UniqueID) in
// ctx's scope without changing it, so a derived id that would repeat
// any of them is suffixed.
func ReserveID(ctx context.Context, id string, children ...string) {
	s := scopeOf(ctx)
	if s == nil || id == "" || containsToken(id) {
		// A token is a derived id still on its way to the page (a
		// component handing it to the one that renders it). An id that
		// only starts like one ("gofastr-idref-guide") is reserved.
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

// HasIDScope reports whether ctx carries an id scope (WithIDScope,
// WithIDNamespace, or a region of a RegionBatch), so UniqueID hands
// out page-unique ids from it. A component that falls back to a
// process-wide counter when no scope is there asks this first: a
// counter gives an island one id on the page and another in its RPC
// answer.
func HasIDScope(ctx context.Context) bool { return scopeOf(ctx) != nil }

func scopeOf(ctx context.Context) *idScope {
	if ctx == nil {
		return nil
	}
	s, _ := ctx.Value(idScopeKey{}).(*idScope)
	return s
}

// batchSeq numbers RegionBatches so a nested batch's tokens never
// match its parent's.
var batchSeq atomic.Uint64

// RegionBatch hands out the contexts of n page regions and settles
// their ids once they have rendered. Build it with NewRegionBatch,
// render region i with Context(i) (on any goroutine), then call Settle
// for each region in order.
type RegionBatch struct {
	parent   context.Context
	regions  []*idScope
	reserved bool // the first Settle reserved every region's explicit ids
}

// NewRegionBatch prepares n regions under ctx's id scope. With no scope
// on ctx every region renders with ctx and Settle changes nothing.
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

// Settle hands region i's recorded ids out from the parent scope and
// returns the function that replaces each placeholder with its id.
// The first call reserves the explicit ids of every region; each call
// then hands out region i's derived ids in the order the region asked
// for them. Call it once per region, in region order, after every
// region has rendered, and apply the function to everything region i
// produced: region 1's repeat of an id region 0 used comes back
// suffixed, and no derived id lands on an explicit one.
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

// ReserveRendered reserves, for region i, every id attribute written in
// hs: an element's id="..." with no placeholder in it, read as the page
// spells it once its character references are decoded. Most components
// forward an explicit id without calling ReserveID; reading their
// output catches every one of them, so no derived id lands on an id the
// region wrote. Call it with each region's output after every region
// has rendered and before the first Settle; a call after that reserves
// in the parent scope at once.
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

// writtenIDs returns the values of the id attributes on the elements of
// s that hold no placeholder token (those are derived ids Settle
// fills), decoded. It reads tags only: a comment, another attribute's
// value, and the text of a raw-text element (script, style, textarea,
// title, iframe, noembed, noframes, xmp, plaintext) are not elements, so an id="..." inside them is not read. Tag and
// attribute names match in ASCII case, as an HTML parser does.
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
				return out // the rest of the document is text
			}
			for _, raw := range []string{"script", "style", "textarea", "title", "iframe", "noembed", "noframes", "xmp"} {
				if asciiEqualFold(name, raw) {
					i = skipRawText(s, i, raw)
					break
				}
			}
		default:
			// An end tag, a doctype, or a "<" in text.
			gt := strings.IndexByte(s[i+1:], '>')
			if gt < 0 {
				return out
			}
			i += 1 + gt + 1
		}
	}
	return out
}

// readAttrs reads a start tag's attributes from s[k:] up to its ">",
// and returns the index past it and the tag's id values.
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
			ids = append(ids, html.UnescapeString(val))
		}
	}
	return len(s), ids
}

// skipRawText returns the index of the end tag closing a raw-text
// element named name, searched from i, or len(s).
func skipRawText(s string, i int, name string) int {
	for {
		k := strings.Index(s[i:], "</")
		if k < 0 {
			return len(s)
		}
		at := i + k + 2
		end := at + len(name)
		// The name must end there: </textareax> does not close a textarea.
		if end <= len(s) && asciiEqualFold(s[at:end], name) && (end == len(s) || isAttrSpace(s[end]) || s[end] == '>' || s[end] == '/') {
			return i + k
		}
		i = at
	}
}

func isASCIILetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

// asciiEqualFold compares a and b ignoring ASCII case only.
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

// containsToken reports whether s holds a placeholder a region issued:
// tokenPrefix, then <batch>x<region>x<op> in digits, then z, p or s.
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

// SettleIDs replaces the placeholders in s that the regions settled so
// far under ctx's scope stand for. A value a component built during a
// region's render (HTML seeded into a signal slice, say) holds the
// region's placeholders, and the region's Settle only rewrote the HTML
// it was handed: the host runs a value that leaves the render some
// other way through SettleIDs once the render is done. With no scope s
// is returned as is.
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
