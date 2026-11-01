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
// The host installs a scope once per page render (core-ui/app's
// WithRequest does). A component rendered with no scope on its context,
// or with no context, keeps the id it derived. An explicit id is never
// rewritten; ReserveID records it so a derived id that would repeat it
// is suffixed instead.
//
// Parts of a page that render concurrently (layout outlet fills) take
// their contexts from a RegionBatch: each region records the ids it asks
// for, and Settle replays the records in region order once every region
// has finished, so the output is the same bytes a sequential render
// would give, whichever goroutine finished first.

type idScopeKey struct{}

type idScope struct {
	mu   sync.Mutex
	seen map[string]int // id -> the highest suffix handed out for it
	// rec is non-nil on a region of a RegionBatch: ids are recorded
	// and answered with placeholder tokens that Settle swaps for the
	// final ids.
	rec *regionLog
}

// regionLog is one concurrent region's record of id requests.
type regionLog struct {
	prefix string // token prefix, unique to the batch and region
	ops    []idOp
}

type idOp struct {
	id      string
	reserve bool // keep id as is (ReserveID, or an id no token stands in for)
}

// tokenSafe reports whether every escaper a component might pass the
// id through (HTML attribute, JS string, URL path or query, CSS ident)
// leaves both the id and its placeholder token unchanged, so swapping
// the token for the final id after the render gives the bytes a
// sequential render would have produced.
func tokenSafe(id string) bool {
	for i := 0; i < len(id); i++ {
		c := id[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// WithIDScope returns a context carrying a fresh id scope.
func WithIDScope(ctx context.Context) context.Context {
	return context.WithValue(ctx, idScopeKey{}, &idScope{seen: map[string]int{}})
}

// UniqueID returns id the first time ctx's scope sees it and id-2,
// id-3, … on each repeat, skipping a suffixed id already taken. With no
// scope (or a nil ctx) or an empty id it returns id unchanged. On a
// region of a RegionBatch it returns a placeholder that the batch's
// Settle replaces with the id the sequential render would have handed
// out.
func UniqueID(ctx context.Context, id string) string {
	s := scopeOf(ctx)
	if s == nil || id == "" {
		return id
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rec != nil {
		if !tokenSafe(id) {
			// No token can stand in for it: suffix against this
			// region's own ids now and reserve the result at Settle.
			final := s.next(id)
			s.rec.ops = append(s.rec.ops, idOp{id: final, reserve: true})
			return final
		}
		s.rec.ops = append(s.rec.ops, idOp{id: id})
		return s.rec.prefix + strconv.Itoa(len(s.rec.ops)-1) + "z"
	}
	return s.next(id)
}

// next hands out id or its next free suffix. Callers hold s.mu.
func (s *idScope) next(id string) string {
	n := s.seen[id]
	if n == 0 {
		s.seen[id] = 1
		return id
	}
	for {
		n++
		cand := id + "-" + strconv.Itoa(n)
		if s.seen[cand] == 0 {
			s.seen[id] = n
			s.seen[cand] = 1
			return cand
		}
	}
}

// ReserveID records an explicit id in ctx's scope without changing it,
// so a later derived id that would repeat it is suffixed.
func ReserveID(ctx context.Context, id string) {
	s := scopeOf(ctx)
	if s == nil || id == "" {
		return
	}
	s.mu.Lock()
	if s.seen[id] == 0 {
		s.seen[id] = 1
	}
	if s.rec != nil {
		s.rec.ops = append(s.rec.ops, idOp{id: id, reserve: true})
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

// RegionBatch hands out the contexts of n page regions that render
// concurrently and settles their ids afterwards. Build it with
// NewRegionBatch, render region i with Context(i) on any goroutine,
// then call Settle for each region in order.
type RegionBatch struct {
	parent  context.Context
	regions []*idScope
}

// NewRegionBatch prepares n concurrently rendered regions under ctx's
// id scope. With no scope on ctx every region renders with ctx and
// Settle returns the html unchanged.
func NewRegionBatch(ctx context.Context, n int) *RegionBatch {
	b := &RegionBatch{parent: ctx}
	if scopeOf(ctx) == nil {
		return b
	}
	seq := strconv.FormatUint(batchSeq.Add(1), 10)
	b.regions = make([]*idScope, n)
	for i := range b.regions {
		b.regions[i] = &idScope{
			seen: map[string]int{},
			rec:  &regionLog{prefix: "gofastr-idref-" + seq + "x" + strconv.Itoa(i) + "x"},
		}
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

// Settle hands region i's recorded ids out from the parent scope, in
// the order the region asked for them, and returns html with each
// placeholder replaced by its id. Call it once per region, in region
// order, after every region has rendered: region 1's repeat of an id
// region 0 used comes back suffixed.
func (b *RegionBatch) Settle(i int, html HTML) HTML {
	if b.regions == nil {
		return html
	}
	r := b.regions[i]
	r.mu.Lock()
	ops := r.rec.ops
	prefix := r.rec.prefix
	r.mu.Unlock()
	if len(ops) == 0 {
		return html
	}
	pairs := make([]string, 0, 2*len(ops))
	for k, op := range ops {
		if op.reserve {
			ReserveID(b.parent, op.id)
			continue
		}
		pairs = append(pairs, prefix+strconv.Itoa(k)+"z", UniqueID(b.parent, op.id))
	}
	if len(pairs) == 0 {
		return html
	}
	return HTML(strings.NewReplacer(pairs...).Replace(string(html)))
}
