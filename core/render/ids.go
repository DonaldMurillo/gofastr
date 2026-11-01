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
// the record once the region has rendered, explicit ids first. Layout
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
	reserve  bool // keep id as is (ReserveID, or an id no token stands in for)
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
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rec != nil {
		if !tokenSafe(id, children) {
			// No token can stand in for it: suffix against this
			// region's own ids now and reserve the result at Settle.
			final := s.next(id, children)
			s.rec.ops = append(s.rec.ops, idOp{id: final, children: children, reserve: true})
			return final
		}
		s.rec.ops = append(s.rec.ops, idOp{id: id, children: children})
		return s.rec.prefix + strconv.Itoa(len(s.rec.ops)-1) + "z"
	}
	return s.next(id, children)
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
	s.mu.Unlock()
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
		if !op.reserve {
			pairs = append(pairs, r.rec.prefix+strconv.Itoa(k)+"z", UniqueID(b.parent, op.id, op.children...))
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
