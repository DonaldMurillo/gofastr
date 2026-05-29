package embed

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type index struct {
	embedder Embedder
	chunker  Chunker
	store    Store
	keyword  KeywordBackend
	reranker Reranker

	path          string
	snapshotEvery int

	mu           sync.Mutex
	wal          *wal
	opsSinceSnap int
}

func (i *index) loadAndReplay() error {
	if i.path == "" {
		return nil
	}
	fs, _ := i.store.(*FlatStore)
	snapPath := filepath.Join(i.path, "store.snap")
	walPath := filepath.Join(i.path, "store.wal")

	if fs != nil {
		if err := fs.LoadSnapshot(snapPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("embed: load snapshot: %w", err)
		}
	}

	w, err := openWAL(walPath)
	if err != nil {
		return err
	}
	i.wal = w
	ctx := context.Background()
	if err := w.replay(func(e walEntry) error {
		switch e.Op {
		case walOpAdd:
			if err := i.store.Add(ctx, e.Chunks); err != nil {
				return err
			}
			return i.keywordIndexChunks(ctx, e.Chunks)
		case walOpRemove:
			if err := i.store.RemoveDoc(ctx, e.DocID); err != nil {
				return err
			}
			return i.keywordRemoveDoc(ctx, e.DocID, nil)
		default:
			return fmt.Errorf("unknown wal op %q", e.Op)
		}
	}); err != nil {
		return err
	}
	// After replay, rebuild keyword index from any chunks loaded from
	// the snapshot itself (the snapshot does not include keyword state).
	if i.keyword != nil && fs != nil {
		fs.mu.RLock()
		chunks := make([]Chunk, len(fs.chunks))
		copy(chunks, fs.chunks)
		fs.mu.RUnlock()
		if err := i.keywordIndexChunks(ctx, chunks); err != nil {
			return err
		}
	}
	return nil
}

func (i *index) Add(ctx context.Context, docs ...Document) error {
	if len(docs) == 0 {
		return nil
	}
	var chunks []Chunk
	for _, doc := range docs {
		if doc.ID == "" {
			return fmt.Errorf("embed: Document.ID is required (source=%q)", doc.Source)
		}
		cs, err := i.chunker.Chunk(doc)
		if err != nil {
			return fmt.Errorf("embed: chunk doc %q: %w", doc.ID, err)
		}
		chunks = append(chunks, cs...)
	}
	if len(chunks) == 0 {
		return nil
	}
	texts := make([]string, len(chunks))
	for j, c := range chunks {
		texts[j] = c.Text
	}
	vecs, err := i.embedder.Embed(ctx, texts)
	if err != nil {
		return fmt.Errorf("embed: embedder.Embed: %w", err)
	}
	if len(vecs) != len(chunks) {
		return fmt.Errorf("embed: embedder returned %d vectors for %d chunks", len(vecs), len(chunks))
	}
	for j := range chunks {
		chunks[j].Vec = vecs[j]
	}

	// Replace-by-doc semantics: every prior chunk for these doc IDs is
	// removed before the new chunks land. We log the removes in the
	// WAL so a crash mid-replace is recoverable to a consistent state.
	for _, doc := range docs {
		if err := i.logAndApplyRemove(ctx, doc.ID); err != nil {
			return err
		}
	}
	return i.logAndApplyAdd(ctx, chunks)
}

func (i *index) Remove(ctx context.Context, docIDs ...string) error {
	for _, id := range docIDs {
		if err := i.logAndApplyRemove(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

func (i *index) logAndApplyAdd(ctx context.Context, chunks []Chunk) error {
	if i.wal != nil {
		if err := i.wal.append(walEntry{Op: walOpAdd, Chunks: chunks}); err != nil {
			return err
		}
	}
	if err := i.store.Add(ctx, chunks); err != nil {
		return err
	}
	if err := i.keywordIndexChunks(ctx, chunks); err != nil {
		return err
	}
	return i.maybeAutoSnapshot()
}

func (i *index) logAndApplyRemove(ctx context.Context, docID string) error {
	priorChunkIDs := i.collectChunkIDs(docID)
	if i.wal != nil {
		if err := i.wal.append(walEntry{Op: walOpRemove, DocID: docID}); err != nil {
			return err
		}
	}
	if err := i.store.RemoveDoc(ctx, docID); err != nil {
		return err
	}
	if err := i.keywordRemoveDoc(ctx, docID, priorChunkIDs); err != nil {
		return err
	}
	return i.maybeAutoSnapshot()
}

func (i *index) collectChunkIDs(docID string) []string {
	fs, ok := i.store.(*FlatStore)
	if !ok || i.keyword == nil {
		return nil
	}
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	positions := fs.byDoc[docID]
	out := make([]string, 0, len(positions))
	for _, p := range positions {
		out = append(out, fs.chunks[p].ID)
	}
	return out
}

func (i *index) keywordIndexChunks(ctx context.Context, chunks []Chunk) error {
	if i.keyword == nil {
		return nil
	}
	for _, c := range chunks {
		if err := i.keyword.Index(ctx, c.ID, c.Text); err != nil {
			return fmt.Errorf("embed: keyword index %q: %w", c.ID, err)
		}
	}
	return nil
}

func (i *index) keywordRemoveDoc(ctx context.Context, _ string, priorChunkIDs []string) error {
	if i.keyword == nil {
		return nil
	}
	for _, id := range priorChunkIDs {
		if err := i.keyword.Delete(ctx, id); err != nil {
			return fmt.Errorf("embed: keyword delete %q: %w", id, err)
		}
	}
	return nil
}

func (i *index) maybeAutoSnapshot() error {
	if i.wal == nil || i.snapshotEvery == 0 {
		return nil
	}
	i.mu.Lock()
	i.opsSinceSnap++
	due := i.snapshotEvery > 0 && i.opsSinceSnap >= i.snapshotEvery
	due = due || i.snapshotEvery < 0
	i.mu.Unlock()
	if !due {
		return nil
	}
	return i.Snapshot()
}

func (i *index) Snapshot() error {
	if i.path == "" {
		return nil
	}
	fs, ok := i.store.(*FlatStore)
	if !ok {
		return nil
	}
	snapPath := filepath.Join(i.path, "store.snap")
	if err := fs.Snapshot(snapPath); err != nil {
		return err
	}
	if i.wal != nil {
		if err := i.wal.truncate(); err != nil {
			return err
		}
	}
	i.mu.Lock()
	i.opsSinceSnap = 0
	i.mu.Unlock()
	return nil
}

// maxQueryK caps the attacker-controllable result count K. K only
// names how many results the caller wants back; values larger than this
// are nonsensical and would otherwise drive an unbounded candidate
// allocation (candidateWidth multiplies K by 4). Clamping fails closed
// without surfacing an error, since a too-large K is harmless intent.
const maxQueryK = 1000

// candidateWidth decides how many candidates to pull from each stage
// before MMR/rerank narrows down to k. Pulling 4x widens recall
// enough to give MMR room without paying the cost of scanning more
// vectors than we have to.
func candidateWidth(k int) int {
	// Defense in depth: clamp k here too so no caller path can produce an
	// unbounded width even if a future caller forgets to clamp K. Query
	// already clamps K to maxQueryK before this runs.
	if k > maxQueryK {
		k = maxQueryK
	}
	w := k * 4
	if w < 50 {
		w = 50
	}
	return w
}

func (i *index) Query(ctx context.Context, q Query) ([]Hit, error) {
	if q.Text == "" {
		return nil, nil
	}
	k := q.K
	if k <= 0 {
		k = 10
	}
	if k > maxQueryK {
		// Clamp attacker-controllable K so candidateWidth cannot drive an
		// unbounded candidate allocation. Fail closed silently: a too-large
		// K is harmless intent, not an error to surface.
		k = maxQueryK
	}
	if q.Rerank && i.reranker == nil {
		return nil, errors.New("embed: Query.Rerank=true but no Reranker configured on Options")
	}
	vecs, err := i.embedder.Embed(ctx, []string{q.Text})
	if err != nil {
		return nil, fmt.Errorf("embed: embed query: %w", err)
	}
	if len(vecs) != 1 {
		return nil, fmt.Errorf("embed: embedder returned %d vectors for query", len(vecs))
	}
	qv := vecs[0]

	width := candidateWidth(k)
	vecHits, err := i.store.Candidates(ctx, qv, q.Filter, width)
	if err != nil {
		return nil, err
	}

	candidates := vecHits
	if q.Hybrid && i.keyword != nil {
		kwRaw, err := i.keyword.Search(ctx, q.Text, width)
		if err != nil {
			return nil, fmt.Errorf("embed: keyword search: %w", err)
		}
		kwHits := i.hydrateKeywordHits(kwRaw, q.Filter)
		candidates = fuseRRF(vecHits, kwHits)
		if len(candidates) > width {
			candidates = candidates[:width]
		}
	}

	if q.MMRLambda > 0 && len(candidates) > 0 {
		candidates = mmr(qv, candidates, q.MMRLambda, k)
	} else if len(candidates) > k {
		candidates = candidates[:k]
	}

	if q.Rerank {
		candidates, err = i.reranker.Rerank(ctx, q.Text, candidates)
		if err != nil {
			return nil, fmt.Errorf("embed: reranker: %w", err)
		}
		if len(candidates) > k {
			candidates = candidates[:k]
		}
	}

	// Strip embeddings on the way out — internal stages need them; the
	// caller does not, and shipping ~1.5KB of floats per hit over the
	// wire is wasteful.
	for j := range candidates {
		candidates[j].Chunk.Vec = nil
	}
	return candidates, nil
}

// hydrateKeywordHits turns chunk-ID-only keyword results into full
// Hits by looking each chunk up in the store. Filtered-out chunks are
// dropped silently. We need the lookup because the keyword backend
// only knows the chunk IDs; the vectors and metadata live in the
// store.
func (i *index) hydrateKeywordHits(raw []KeywordHit, filter Filter) []Hit {
	fs, ok := i.store.(*FlatStore)
	if !ok {
		return nil
	}
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	byID := make(map[string]*Chunk, len(fs.chunks))
	for idx := range fs.chunks {
		byID[fs.chunks[idx].ID] = &fs.chunks[idx]
	}
	out := make([]Hit, 0, len(raw))
	for _, kh := range raw {
		c, ok := byID[kh.ChunkID]
		if !ok {
			continue
		}
		if !chunkMatches(c, filter) {
			continue
		}
		out = append(out, Hit{Chunk: *c, Score: kh.Score, Reason: "kw"})
	}
	return out
}

func (i *index) Stats() Stats {
	s := i.store.Stats()
	if s.Model == "" {
		s.Model = i.embedder.Name()
	}
	if s.Dim == 0 {
		s.Dim = i.embedder.Dim()
	}
	return s
}

func (i *index) Close() error {
	var errs []error
	if i.path != "" {
		if err := i.Snapshot(); err != nil {
			errs = append(errs, err)
		}
	}
	if i.wal != nil {
		if err := i.wal.close(); err != nil {
			errs = append(errs, err)
		}
		i.wal = nil
	}
	if c, ok := i.store.(interface{ Close() error }); ok {
		if err := c.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
