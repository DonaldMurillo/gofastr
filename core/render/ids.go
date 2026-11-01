package render

import (
	"context"
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
// the record once the region has rendered, explicit ids first. An id
// with a byte an escaper may rewrite ("cart.view") is emitted as is
// between two tokens that Settle fills with its namespace and its
// repeat suffix, so it is settled across regions like the rest. Layout
// outlet fills render concurrently as regions settled in outlet order,
// so the output is the same bytes whichever goroutine finished first;
// core-ui/app renders a whole page as one region, so an explicit id
// wins over a derived one wherever the two sit.

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
}

func newIDScope() *idScope {
	return &idScope{taken: map[string]bool{}, last: map[string]int{}}
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
	return context.WithValue(ctx, idScopeKey{}, newIDScope())
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
	s := newIDScope()
	s.ns, s.outer = encodeNamespace(ns), ctx
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
	if s == nil || id == "" || strings.HasPrefix(id, tokenPrefix) {
		// A token is a derived id still on its way to the page (a
		// component handing it to the one that renders it).
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
		s := newIDScope()
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
	return func(h HTML) HTML { return HTML(rep.Replace(string(h))) }
}

func (s *idScope) record() []idOp {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rec.ops
}
