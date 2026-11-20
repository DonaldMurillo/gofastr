package semantic

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/internal/atomicfile"
)

var syncDir = atomicfile.SyncDir

type snapshotter interface {
	Snapshot(path string) error
	LoadSnapshot(path string) error
}

type chunkLister interface {
	ChunkIDsForDoc(docID string) ([]string, error)
	ChunkByID(id string) (Chunk, bool)
	AllChunks() []Chunk
}

type index struct {
	restoring bool
	embedder  Embedder
	chunker   Chunker
	store     Store
	keyword   KeywordBackend
	reranker  Reranker

	path          string
	snapshotEvery int
	readOnly      bool
	lock          *os.File

	mu           sync.Mutex
	wal          *wal
	closed       bool
	opsSinceSnap int
	halfApplied  *pendingReplace

	dim atomic.Int64

	eraseName       string
	unregisterErase func()
}

func (i *index) registerEraser() {
	if i.readOnly {
		return
	}
	if _, shared := i.store.(sharedStore); shared {
		return
	}
	if _, ok := i.store.(chunkLister); !ok {
		return
	}
	name := "semantic:memory"
	if i.path != "" {
		name = "semantic:" + i.path
	}
	i.eraseName, i.unregisterErase = datexport.RegisterStoreEraser(datexport.StoreEraser{
		Name:   name,
		Source: "semantic",
		Erase:  i.eraseOwner,
	})
}

func (i *index) eraseOwner(ctx context.Context, owner string, dryRun bool) (int, error) {
	cl, ok := i.store.(chunkLister)
	if !ok {
		return 0, fmt.Errorf("semantic: store %T cannot list its chunks, so the user's cannot be found", i.store)
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed {
		return 0, ErrClosed
	}
	if dryRun {
		return i.ownedChunksLocked(cl, owner), nil
	}
	if err := i.finishHalfAppliedLocked(); err != nil {
		return 0, fmt.Errorf("semantic: the user's documents were not erased: %w", err)
	}
	var docs []string
	seen := map[string]bool{}
	n := 0
	for _, c := range cl.AllChunks() {
		if c.Owner != owner {
			continue
		}
		n++
		if !seen[c.DocID] {
			seen[c.DocID] = true
			docs = append(docs, c.DocID)
		}
	}
	if len(docs) > 0 {
		if err := i.replaceLocked(context.WithoutCancel(ctx), docs, nil); err != nil {
			return 0, err
		}
	}
	if err := i.snapshotLocked(); err != nil {
		return 0, fmt.Errorf("semantic: documents removed, but the snapshot and WAL that still hold them were not rewritten: %w", err)
	}
	return n, nil
}

func (i *index) ownedChunksLocked(cl chunkLister, owner string) int {
	pending := map[string]bool{}
	n := 0
	if p := i.halfApplied; p != nil {
		for _, id := range p.docIDs {
			pending[id] = true
		}
		for _, c := range p.chunks {
			if c.Owner == owner {
				n++
			}
		}
	}
	for _, c := range cl.AllChunks() {
		if c.Owner == owner && !pending[c.DocID] {
			n++
		}
	}
	return n
}

func (i *index) storeDim() int {
	if d := i.dim.Load(); d > 0 {
		return int(d)
	}
	d := i.store.Stats().Dim
	if d > 0 {
		i.dim.Store(int64(d))
	}
	return d
}

func (i *index) loadAndReplay() error {
	if i.path == "" {
		return nil
	}
	snapPath := filepath.Join(i.path, "store.snap")
	walPath := filepath.Join(i.path, "store.wal")

	if !i.readOnly {
		lock, err := lockIndexDir(i.path)
		if err != nil {
			return err
		}
		i.lock = lock
	}
	if s, ok := i.store.(snapshotter); ok {
		if err := s.LoadSnapshot(snapPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("semantic: load snapshot: %w", err)
		}
	}
	if i.readOnly {
		return replayReadOnly(walPath, i.replayEntryLocked)
	}

	w, err := openWAL(walPath)
	if err != nil {
		return err
	}
	i.wal = w
	if err := w.replay(i.replayEntryLocked); err != nil {
		return err
	}
	if info, err := w.f.Stat(); err != nil {
		return err
	} else if info.Size() > 0 {
		if err := i.snapshotLocked(); err != nil {
			return err
		}
	}
	return nil
}

func (i *index) replayEntryLocked(e walEntry) error {
	var docIDs []string
	chunks := e.Chunks
	switch e.Op {
	case walOpAdd:
	case walOpRemove:
		docIDs, chunks = []string{e.DocID}, nil
	case walOpReplace:
		docIDs = e.DocIDs
	default:
		return fmt.Errorf("unknown wal op %q", e.Op)
	}
	if len(chunks) > 0 && e.Model != "" && e.Model != i.embedder.Name() {
		return &ModelMismatchError{SnapshotModel: e.Model, SnapshotDim: len(chunks[0].Vec), StoreModel: i.embedder.Name(), StoreDim: i.embedder.Dim()}
	}
	return i.applyLoggedLocked(context.Background(), docIDs, chunks)
}

func (i *index) rebuildKeyword() error {
	if i.keyword == nil {
		return nil
	}
	if _, ok := i.keyword.(pgKeyword); ok {
		return nil
	}
	ctx := context.Background()
	var chunks []Chunk
	if ce, ok := i.store.(chunkEnumerator); ok {
		var err error
		if chunks, err = ce.allChunks(ctx); err != nil {
			return fmt.Errorf("semantic: rebuild keyword index: %w", err)
		}
	} else if cl, ok := i.store.(chunkLister); ok {
		chunks = cl.AllChunks()
	}
	if len(chunks) > 0 {
		return i.keywordIndexChunks(ctx, chunks)
	}
	return nil
}

type chunkFilterer interface {
	chunksByIDMatching(ctx context.Context, ids []string, filter Filter) map[string]Chunk
}

type docSourceLister interface {
	docSources(ctx context.Context) (map[string]string, error)
}

func (i *index) docSources(ctx context.Context) (map[string]string, error) {
	var chunks []Chunk
	if e, ok := i.store.(chunkEnumerator); ok {
		var err error
		if chunks, err = e.allChunks(ctx); err != nil {
			return nil, err
		}
	} else if cl, ok := i.store.(chunkLister); ok {
		chunks = cl.AllChunks()
	} else {
		return nil, nil
	}
	out := make(map[string]string, len(chunks))
	for _, c := range chunks {
		out[c.DocID] = c.Source
	}
	return out, nil
}

type sharedStore interface {
	sharedAcrossProcesses()
}

type chunkEnumerator interface {
	allChunks(ctx context.Context) ([]Chunk, error)
}

func (i *index) Add(ctx context.Context, docs ...Document) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("semantic: add panicked: %v", v)
		}
	}()
	if len(docs) == 0 {
		return nil
	}
	docs = lastPerID(docs)
	for _, document := range docs {
		if err := validateDocumentText(document); err != nil {
			return err
		}
	}
	var chunks []Chunk
	for _, doc := range docs {
		if doc.ID == "" {
			return fmt.Errorf("semantic: Document.ID is required (source=%q)", doc.Source)
		}
		cs, err := i.chunker.Chunk(doc)
		if err != nil {
			return &chunkError{docID: doc.ID, err: err}
		}
		for k := range cs {
			cs[k].Owner = doc.Owner
		}
		chunks = append(chunks, cs...)
	}
	ids := make([]string, len(docs))
	for j, doc := range docs {
		ids[j] = doc.ID
	}
	if len(chunks) == 0 {
		return i.replace(ctx, ids, nil)
	}
	texts := make([]string, len(chunks))
	for j, c := range chunks {
		texts[j] = c.Text
	}
	vecs, err := i.embedder.Embed(ctx, texts)
	if err != nil {
		return fmt.Errorf("semantic: embedder.Embed: %w", err)
	}
	if len(vecs) != len(chunks) {
		return fmt.Errorf("semantic: embedder returned %d vectors for %d chunks", len(vecs), len(chunks))
	}
	expectedDim := i.storeDim()
	if expectedDim == 0 && len(vecs) > 0 {
		expectedDim = len(vecs[0])
	}
	if expectedDim == 0 {
		return fmt.Errorf("semantic: embedder.Embed: %w", errEmptyVector)
	}
	for j := range chunks {
		if len(vecs[j]) != expectedDim {
			return errVecDim(expectedDim, len(vecs[j]))
		}
		if err := finiteVector(vecs[j]); err != nil {
			return err
		}
		chunks[j].Vec = vecs[j]
	}

	return i.replace(ctx, ids, chunks)
}

func lastPerID(docs []Document) []Document {
	last := make(map[string]int, len(docs))
	for j, d := range docs {
		last[d.ID] = j
	}
	if len(last) == len(docs) {
		return docs
	}
	out := make([]Document, 0, len(last))
	for j, d := range docs {
		if last[d.ID] == j {
			out = append(out, d)
		}
	}
	return out
}

type chunkError struct {
	docID string
	err   error
}

func (e *chunkError) Error() string {
	return fmt.Sprintf("semantic: chunk doc %q: %v", e.docID, e.err)
}

func (e *chunkError) Unwrap() error { return e.err }

func (i *index) Remove(ctx context.Context, docIDs ...string) error {
	return i.replace(ctx, docIDs, nil)
}

func (i *index) replace(ctx context.Context, docIDs []string, chunks []Chunk) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("semantic: replace panicked: %v", v)
		}
	}()
	if i.readOnly {
		return ErrReadOnly
	}
	if i.path != "" && len(chunks) > 0 {
		if err := walEncodable(chunks); err != nil {
			return err
		}
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed {
		return ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return i.replaceLocked(ctx, docIDs, chunks)
}

func (i *index) replaceLocked(ctx context.Context, docIDs []string, chunks []Chunk) error {
	if r, ok := i.store.(docReplacer); ok && i.wal == nil {
		if err := i.applyReplaceLocked(ctx, r, docIDs, chunks); err != nil {
			return err
		}
		if i.dueSnapshotLocked() {
			return i.snapshotLocked()
		}
		return nil
	}
	var logged *pendingReplace
	if i.wal != nil {
		if err := i.finishHalfAppliedLocked(); err != nil {
			return err
		}
		if i.wal.needsSnapshot() {
			if err := i.snapshotLocked(); err != nil {
				return err
			}
		}
		var priorKeyword []string
		for _, id := range docIDs {
			prior, err := i.collectChunkIDs(id)
			if err != nil {
				return err
			}
			priorKeyword = append(priorKeyword, prior...)
		}
		e := walEntry{Op: walOpReplace, DocIDs: docIDs, Chunks: chunks}
		if len(chunks) > 0 {
			e.Model = i.embedder.Name()
		}
		if err := i.wal.append(e); err != nil {
			return err
		}
		logged = &pendingReplace{docIDs: docIDs, chunks: chunks, priorKeyword: priorKeyword}
	}
	i.halfApplied = logged
	if err := i.applyLoggedLocked(context.WithoutCancel(ctx), docIDs, chunks); err != nil {
		return err
	}
	i.halfApplied = nil
	if i.dueSnapshotLocked() {
		return i.snapshotLocked()
	}
	return nil
}

func (i *index) applyLoggedLocked(ctx context.Context, docIDs []string, chunks []Chunk) error {
	for _, id := range docIDs {
		if err := i.applyRemoveLocked(ctx, id); err != nil {
			return err
		}
	}
	if len(chunks) > 0 {
		return i.applyAddLocked(ctx, chunks)
	}
	return nil
}

type pendingReplace struct {
	docIDs       []string
	chunks       []Chunk
	priorKeyword []string
}

func (i *index) finishHalfAppliedLocked() error {
	p := i.halfApplied
	if p == nil {
		return nil
	}
	ctx := context.Background()
	err := i.keywordRemoveDoc(ctx, "", p.priorKeyword)
	if err == nil {
		err = i.applyLoggedLocked(ctx, p.docIDs, p.chunks)
	}
	if err != nil {
		return fmt.Errorf("semantic: an earlier write is half-applied; the WAL keeps it for replay: %w", err)
	}
	i.halfApplied = nil
	return nil
}

type docReplacer interface {
	replaceDocs(ctx context.Context, model string, docIDs []string, chunks []Chunk) error
}

func (i *index) applyReplaceLocked(ctx context.Context, r docReplacer, docIDs []string, chunks []Chunk) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("semantic: apply replace panicked: %v", v)
		}
	}()
	var prior []string
	for _, id := range docIDs {
		ids, err := i.collectChunkIDs(id)
		if err != nil {
			return err
		}
		prior = append(prior, ids...)
	}
	if err := r.replaceDocs(ctx, i.embedder.Name(), docIDs, chunks); err != nil {
		return err
	}
	ctx = context.WithoutCancel(ctx)
	if err := i.keywordRemoveDoc(ctx, "", prior); err != nil {
		return err
	}
	if err := i.keywordIndexChunks(ctx, chunks); err != nil {
		return err
	}
	i.opsSinceSnap += len(docIDs)
	if len(chunks) > 0 {
		i.opsSinceSnap++
	}
	return nil
}

func (i *index) applyAddLocked(ctx context.Context, chunks []Chunk) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("semantic: apply add panicked: %v", v)
		}
	}()
	if err := i.store.Add(ctx, chunks); err != nil {
		return err
	}
	if err := i.keywordIndexChunks(ctx, chunks); err != nil {
		return err
	}
	i.opsSinceSnap++
	return nil
}

func (i *index) applyRemoveLocked(ctx context.Context, docID string) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("semantic: apply remove panicked: %v", v)
		}
	}()
	priorChunkIDs, err := i.collectChunkIDs(docID)
	if err != nil {
		return err
	}
	if err := i.store.RemoveDoc(ctx, docID); err != nil {
		return err
	}
	if err := i.keywordRemoveDoc(ctx, docID, priorChunkIDs); err != nil {
		return err
	}
	i.opsSinceSnap++
	return nil
}

func (i *index) collectChunkIDs(docID string) ([]string, error) {
	if i.keyword == nil {
		return nil, nil
	}
	cl, ok := i.store.(chunkLister)
	if !ok {
		return nil, nil
	}
	return cl.ChunkIDsForDoc(docID)
}

func (i *index) keywordIndexChunks(ctx context.Context, chunks []Chunk) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("semantic: keyword index panicked: %v", v)
		}
	}()
	if i.keyword == nil {
		return nil
	}
	owned, _ := i.keyword.(OwnedKeywordBackend)
	for _, c := range chunks {
		index := i.keyword.Index
		if owned != nil {
			index = func(ctx context.Context, id, text string) error { return owned.IndexOwned(ctx, id, c.Owner, text) }
		}
		if err := index(ctx, c.ID, c.Text); err != nil {
			if invalid := (*InvalidDocumentTextError)(nil); i.restoring && errors.As(err, &invalid) {
				slog.Warn("semantic: keyword index skipped stored chunk with invalid document text", "chunk", textsafe.ScrubControlBytes(c.ID), "err", textsafe.ScrubControlBytes(err.Error()))
				continue
			}
			return fmt.Errorf("semantic: keyword index %q: %w", c.ID, err)
		}
	}
	return nil
}

func (i *index) keywordRemoveDoc(ctx context.Context, _ string, priorChunkIDs []string) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("semantic: keyword delete panicked: %v", v)
		}
	}()
	if i.keyword == nil {
		return nil
	}
	for _, id := range priorChunkIDs {
		if err := i.keyword.Delete(ctx, id); err != nil {
			return fmt.Errorf("semantic: keyword delete %q: %w", id, err)
		}
	}
	return nil
}

func (i *index) dueSnapshotLocked() bool {
	if i.snapshotEvery == 0 {
		return false
	}
	if i.snapshotEvery < 0 {
		return true
	}
	return i.opsSinceSnap >= i.snapshotEvery
}

func (i *index) snapshotLocked() error {
	if i.path == "" {
		return nil
	}
	s, ok := i.store.(snapshotter)
	if !ok {
		return nil
	}
	if err := i.finishHalfAppliedLocked(); err != nil {
		return err
	}
	snapPath := filepath.Join(i.path, "store.snap")
	if err := s.Snapshot(snapPath); err != nil {
		return err
	}
	if err := syncDir(i.path); err != nil {
		return fmt.Errorf("semantic: fsync index dir after snapshot: %w", err)
	}
	if i.wal != nil {
		if err := i.wal.truncate(); err != nil {
			return err
		}
	}
	i.opsSinceSnap = 0
	return nil
}

// Snapshot writes a full snapshot and truncates the WAL as one consistent cut.
func (i *index) Snapshot() error {
	if i.readOnly {
		return ErrReadOnly
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed {
		return ErrClosed
	}
	return i.snapshotLocked()
}

const maxQueryK = 1000

func candidateWidth(k int) int {
	if k > maxQueryK {
		k = maxQueryK
	}
	w := max(k*4, 50)
	return w
}

func (i *index) Query(ctx context.Context, q Query) (hits []Hit, err error) {
	defer func() {
		if v := recover(); v != nil {
			hits, err = nil, fmt.Errorf("semantic: query panicked: %v", v)
		}
	}()
	if math.IsNaN(q.MMRLambda) || math.IsInf(q.MMRLambda, 0) || q.MMRLambda < 0 || q.MMRLambda > 1 {
		return nil, &InvalidQueryError{Reason: "MMRLambda must be finite and between 0 and 1"}
	}
	if err := validateQueryText(q); err != nil {
		return nil, err
	}
	if q.Text == "" {
		return nil, nil
	}
	k := q.K
	if k <= 0 {
		k = 10
	}
	if k > maxQueryK {
		k = maxQueryK
	}
	if q.Rerank && i.reranker == nil {
		return nil, &InvalidQueryError{Reason: "Query.Rerank=true but no Reranker configured on Options"}
	}
	vecs, err := i.embedder.Embed(ctx, []string{q.Text})
	if err != nil {
		return nil, fmt.Errorf("semantic: query embedding: %w", err)
	}
	if len(vecs) != 1 {
		return nil, fmt.Errorf("semantic: embedder returned %d vectors for query", len(vecs))
	}
	qv := vecs[0]

	width := candidateWidth(k)
	vecHits, err := i.store.Candidates(ctx, qv, q.Filter, width)
	if err != nil {
		return nil, err
	}

	candidates := vecHits
	if q.Hybrid && i.keyword != nil {
		kwHits, err := i.scopedKeywordHits(ctx, boundedKeywordText(q.Text), q.Filter, width)
		if err != nil {
			return nil, fmt.Errorf("semantic: keyword search: %w", err)
		}
		candidates = fuseRRF(vecHits, kwHits)
		if len(candidates) > width {
			candidates = candidates[:width]
		}
	}

	if q.MMRLambda > 0 && len(candidates) > 0 {
		unit := slices.Clone(qv)
		normalize(unit)
		candidates = mmr(unit, candidates, q.MMRLambda, k)
	} else if q.Rerank && len(candidates) > width {
		candidates = candidates[:width]
	} else if !q.Rerank && len(candidates) > k {
		candidates = candidates[:k]
	}

	if q.Rerank {
		candidates, err = i.reranker.Rerank(ctx, q.Text, candidates)
		if err != nil {
			return nil, fmt.Errorf("semantic: reranker: %w", err)
		}
		if len(candidates) > k {
			candidates = candidates[:k]
		}
	}

	for j := range candidates {
		candidates[j].Chunk.Vec = nil
	}
	return candidates, nil
}

const maxKeywordQueryTerms = 64

func boundedKeywordText(q string) string {
	n := 0
	inField := false
	for i, r := range q {
		if !keywordRune(r) {
			inField = false
		} else {
			if !inField {
				inField = true
				n++
				if n > maxKeywordQueryTerms {
					return q[:i]
				}
			}
		}
	}
	return q
}

func (i *index) hydrateKeywordHits(ctx context.Context, raw []KeywordHit, filter Filter) []Hit {
	cl, ok := i.store.(chunkLister)
	if !ok {
		return nil
	}
	fl, filtersItself := i.store.(chunkFilterer)
	var matched map[string]Chunk
	if filtersItself && len(raw) > 0 {
		ids := make([]string, len(raw))
		for j, kh := range raw {
			ids[j] = kh.ChunkID
		}
		matched = fl.chunksByIDMatching(ctx, ids, filter)
	}
	out := make([]Hit, 0, len(raw))
	match := newFilterMatcher(filter)
	for _, kh := range raw {
		var c Chunk
		if filtersItself {
			if c, ok = matched[kh.ChunkID]; !ok {
				continue
			}
		} else if c, ok = cl.ChunkByID(kh.ChunkID); !ok || !match.matches(&c) {
			continue
		}
		out = append(out, Hit{Chunk: c, Score: kh.Score, Reason: "kw"})
	}
	return out
}

func (i *index) scopedKeywordHits(ctx context.Context, text string, filter Filter, width int) ([]Hit, error) {
	unfiltered := filter.Source == "" && filter.Kind == "" && len(filter.MetaMatch) == 0
	top, ceiling := width, -1
	for {
		raw, err := i.keywordSearch(ctx, text, top)
		if err != nil {
			return nil, err
		}
		hits := i.hydrateKeywordHits(ctx, raw, filter)
		if len(hits) >= width {
			return hits[:width], nil
		}
		if unfiltered || len(raw) < top {
			return hits, nil
		}
		if ceiling < 0 {
			ceiling = i.store.Stats().Chunks
		}
		if top >= ceiling {
			return hits, nil
		}
		top = min(top*4, ceiling)
	}
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
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed {
		return nil
	}
	i.closed = true
	if i.unregisterErase != nil {
		i.unregisterErase()
		i.unregisterErase = nil
	}
	var errs []error
	if i.path != "" && !i.readOnly {
		if err := i.snapshotLocked(); err != nil {
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
	if i.lock != nil {
		if err := i.lock.Close(); err != nil {
			errs = append(errs, err)
		}
		i.lock = nil
	}
	return errors.Join(errs...)
}

func (i *index) keywordSearch(ctx context.Context, text string, top int) (out []KeywordHit, err error) {
	defer func() {
		if v := recover(); v != nil {
			out = nil
			err = fmt.Errorf("keyword backend panicked: %v", v)
		}
	}()
	return i.keyword.Search(ctx, text, top)
}
