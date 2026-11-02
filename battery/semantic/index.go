package semantic

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/internal/atomicfile"
)

// syncDir flushes the index directory after a snapshot is renamed into
// it; a variable so a test can watch the order against the WAL truncate.
var syncDir = atomicfile.SyncDir

// snapshotter is the optional Store capability needed for on-disk persistence
// (Options.Path). FlatStore implements it; a custom Store that doesn't gets
// rejected at Open() rather than silently never persisting.
type snapshotter interface {
	Snapshot(path string) error
	LoadSnapshot(path string) error
}

// chunkLister is the optional Store capability the hybrid/keyword path needs:
// listing a doc's chunk IDs (to purge keyword entries on delete) and looking a
// chunk up by ID (to hydrate keyword-only hits). FlatStore implements it; a
// custom Store that doesn't gets rejected at Open() when Options.Keyword is set.
// Otherwise hybrid search would silently drop keyword hits and deletes would
// leave stale keyword entries.
type chunkLister interface {
	ChunkIDsForDoc(docID string) []string
	ChunkByID(id string) (Chunk, bool)
	AllChunks() []Chunk
}

type index struct {
	embedder Embedder
	chunker  Chunker
	store    Store
	keyword  KeywordBackend
	reranker Reranker

	path          string
	snapshotEvery int
	// readOnly is Options.ReadOnly: no lock, WAL or snapshot writes.
	readOnly bool
	// lock is the writer's lock file on path (see lockIndexDir), closed
	// by Close.
	lock *os.File

	mu  sync.Mutex
	wal *wal
	// closed is set by Close under mu. A write that reached mu after Close
	// found the WAL gone and applied in memory only: acknowledged, then
	// lost on reopen. Writes after Close return ErrClosed.
	closed       bool
	opsSinceSnap int
	// halfApplied is the logged replace whose apply last failed part-way,
	// nil once every logged write has applied whole.
	halfApplied *pendingReplace

	// dim caches the store's vector dimension once it is known, so Add
	// never calls Store.Stats (a full-table COUNT on pgvector, under its
	// own timeout) per write.
	dim atomic.Int64

	// eraseName and unregisterErase are the store eraser registerEraser
	// added; empty and nil when it added none.
	eraseName       string
	unregisterErase func()
}

// registerEraser makes App.EraseUserData reach the chunks this process
// holds: a writer's snapshot and WAL hold every chunk's text, an index
// with no Path holds it in memory, and no SQL eraser reaches either. A
// read-only index leaves erasure to the writer. A store shared across
// processes (PgVectorStore) has its own SQL eraser, and a custom store
// that cannot list its chunks cannot be searched for the user's.
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

// eraseOwner removes every document whose chunks name owner and then
// rewrites the snapshot, which empties the WAL (an index with no Path has
// neither, and the snapshot step does nothing). A remove is logged as a
// record that names the documents, so the records that added them, text
// included, stay in the WAL and the snapshot until one is rewritten. The
// snapshot is rewritten even when owner has nothing left, since an earlier
// remove leaves the same records behind. It returns the chunks removed;
// with dryRun it removes nothing and returns how many it would.
//
// It holds i.mu from the selection through the snapshot, and first applies
// a logged write that failed part-way: that write's chunks are in no
// listing yet, and the snapshot would apply it, putting back the text the
// erasure was meant to remove. A write that still cannot apply fails the
// erasure, so it never reports success over text it could not reach.
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

// ownedChunksLocked counts the chunks naming owner as the index stands
// once a half-applied write finishes: the stored chunks of documents that
// write does not replace, plus its own. The caller MUST hold i.mu.
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

// storeDim returns the store's vector dimension, asking Stats only while
// it is still unknown (0: an empty FlatStore that infers it on first Add).
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
	// A fresh gob encoder must not append a second type table to the old
	// stream. Checkpoint replayed state before starting a new WAL stream.
	if info, err := w.f.Stat(); err != nil {
		return err
	} else if info.Size() > 0 {
		if err := i.snapshotLocked(); err != nil {
			return err
		}
	}
	return nil
}

// replayEntryLocked applies one WAL record. Open is single-threaded, so
// replay applies through the *Locked helpers without taking i.mu. Add
// and remove records are what WALs written before the replace record
// hold.
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

// rebuildKeyword fills the keyword backend from every chunk the store
// already holds: one loaded from a snapshot (which carries no keyword
// state), or one a durable store such as PgVectorStore kept across a
// restart or wrote from another replica, with no Path involved.
// keyword != nil guarantees the store is a chunkLister (Open fails closed).
func (i *index) rebuildKeyword() error {
	if i.keyword == nil {
		return nil
	}
	// The store's own keyword index reads the store; there is nothing
	// to fill, and reading every row would load the table into memory.
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

// chunkFilterer is a store that looks chunks up under a Filter in its
// own query language (PgVectorStore), so the hybrid keyword leg applies
// the predicate the vector leg does instead of rechecking decoded
// metadata, where a JSON number has already lost precision. It returns
// the chunks among ids that pass filter, keyed by chunk ID, in one round
// trip.
type chunkFilterer interface {
	chunksByIDMatching(ctx context.Context, ids []string, filter Filter) map[string]Chunk
}

// docSourceLister lists every stored document's ID with its Source. A
// Watcher's first scan reads it to find files deleted while no watcher
// ran. A nil map means the store cannot enumerate its documents.
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

// sharedStore is a store other processes write to as well
// (PgVectorStore). A keyword index held in one process's memory cannot
// follow their writes, so Open refuses MemoryKeyword over one.
type sharedStore interface {
	sharedAcrossProcesses()
}

// chunkEnumerator is a store whose full read can fail (PgVectorStore).
// rebuildKeyword reads through it so a failed read fails Open instead of
// passing for an empty store.
type chunkEnumerator interface {
	allChunks(ctx context.Context) ([]Chunk, error)
}

func (i *index) Add(ctx context.Context, docs ...Document) (err error) {
	// The chunker and embedder are host-supplied (Options.Chunker /
	// Options.Embedder) and Add runs from handlers and the watcher loop
	// with no per-request recover net — a panicking backend becomes an
	// attributed error instead of a process kill.
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("semantic: add panicked: %v", v)
		}
	}()
	if len(docs) == 0 {
		return nil
	}
	docs = lastPerID(docs)
	var chunks []Chunk
	for _, doc := range docs {
		if doc.ID == "" {
			return fmt.Errorf("semantic: Document.ID is required (source=%q)", doc.Source)
		}
		cs, err := i.chunker.Chunk(doc)
		if err != nil {
			return &chunkError{docID: doc.ID, err: err}
		}
		// Set here, not in the chunker, so a host Chunker cannot drop it.
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

// lastPerID keeps the last Document for each ID, in batch order. Add
// removes each doc once and then appends every chunk, so a batch naming
// an ID twice would otherwise store both versions under the same chunk
// IDs; a later write to the same ID replaces the earlier one, as two
// separate Adds do.
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

// chunkError is a document the chunker refused (bad bytes, a failing
// fallback): a fault in that document's content, not in the index.
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

// replace removes every prior chunk of docIDs and then stores chunks
// (none for a plain Remove), logged as ONE WAL record before any step
// applies, so a crash replays the whole replace or none of it. Logged as
// a remove record and then an add record, a crash between the two
// reopened with neither version of the document. The whole sequence runs
// under i.mu: taking the lock per step let two replaces of one document
// interleave as remove-A, remove-B, add-A, add-B and keep both versions.
// A cancel is honored only before the first remove: once one has
// applied, stopping would lose the document, so the rest runs to
// completion. A store's own transactional replace honors it until it
// commits, since its rollback keeps the old version.
func (i *index) replace(ctx context.Context, docIDs []string, chunks []Chunk) (err error) {
	// walEncodable runs metadata values' own GobEncode methods, host
	// code like the store and keyword backend below.
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("semantic: replace panicked: %v", v)
		}
	}()
	if i.readOnly {
		return ErrReadOnly
	}
	// Check that the WAL can encode the chunks before logging them: a
	// failed gob Encode (metadata holding a type gob cannot carry) is
	// sticky on the encoder and would refuse every later record.
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

// replaceLocked is replace past its checks. The caller MUST hold i.mu.
func (i *index) replaceLocked(ctx context.Context, docIDs []string, chunks []Chunk) error {
	// A store that replaces documents in one transaction of its own
	// (PgVectorStore) does so: removing and adding as separate steps loses
	// the old version when the add fails, and i.mu does not reach a second
	// replica sharing the store. Such a store never has a WAL (Path needs
	// a snapshotter, which it is not); the WAL path keeps the steps. Its
	// transaction gets the caller's context: a cancel rolls the whole
	// replace back, where a context without one kept the caller waiting
	// on another replica's document lock while it held i.mu.
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
		// An earlier write that failed part-way applies first, so it can
		// never land over a newer version of its documents.
		if err := i.finishHalfAppliedLocked(); err != nil {
			return err
		}
		// A failed append stopped the WAL. The store holds exactly the
		// acknowledged writes (a write whose append failed was never
		// applied), so a snapshot of it is a clean cut and restarts the
		// WAL; until the disk takes one, writes keep failing.
		if i.wal.needsSnapshot() {
			if err := i.snapshotLocked(); err != nil {
				return err
			}
		}
		e := walEntry{Op: walOpReplace, DocIDs: docIDs, Chunks: chunks}
		if len(chunks) > 0 {
			e.Model = i.embedder.Name()
		}
		if err := i.wal.append(e); err != nil {
			return err
		}
		logged = &pendingReplace{docIDs: docIDs, chunks: chunks}
		for _, id := range docIDs {
			logged.priorKeyword = append(logged.priorKeyword, i.collectChunkIDs(id)...)
		}
	}
	// Past this point the replace is logged: store and keyword index
	// apply it (ctx no longer cancels). A panic or error mid-apply leaves
	// it half-applied until finishHalfAppliedLocked applies it again.
	i.halfApplied = logged
	if err := i.applyLoggedLocked(context.WithoutCancel(ctx), docIDs, chunks); err != nil {
		return err
	}
	i.halfApplied = nil
	// Snapshot only between whole replaces, never between a remove and
	// its add.
	if i.dueSnapshotLocked() {
		return i.snapshotLocked()
	}
	return nil
}

// applyLoggedLocked applies a logged replace: every prior chunk of docIDs
// out, then chunks in. Applying one again is harmless, since its removes
// also take out whatever part of chunks an earlier attempt stored. The
// caller MUST hold i.mu.
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

// pendingReplace is a logged replace not yet applied whole: its documents,
// its new chunks, and the keyword IDs of the chunks it replaces, which
// the store no longer lists once its remove applied.
type pendingReplace struct {
	docIDs       []string
	chunks       []Chunk
	priorKeyword []string
}

// finishHalfAppliedLocked applies again the logged replace whose apply
// failed part-way (a keyword delete refused after the store removed the
// old chunks). A snapshot taken over it would write the store without
// the document and truncate the record that restores it, so while it
// still fails no snapshot is taken and the WAL keeps it for replay. The
// caller MUST hold i.mu.
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

// docReplacer is a store that removes documents and stores their new
// chunks in one atomic step, serialized per document across every process
// sharing the store. model is the embedder that produced chunks; a store
// that records its model refuses chunks of another.
type docReplacer interface {
	replaceDocs(ctx context.Context, model string, docIDs []string, chunks []Chunk) error
}

// applyReplaceLocked replaces docIDs with chunks through the store's own
// atomic replace, then brings the keyword backend in line. The keyword
// backend is touched only after the store has committed, so a failed
// replace leaves both on the old version. The caller MUST hold i.mu.
func (i *index) applyReplaceLocked(ctx context.Context, r docReplacer, docIDs []string, chunks []Chunk) (err error) {
	// Same net as applyAddLocked.
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("semantic: apply replace panicked: %v", v)
		}
	}()
	var prior []string
	for _, id := range docIDs {
		prior = append(prior, i.collectChunkIDs(id)...)
	}
	if err := r.replaceDocs(ctx, i.embedder.Name(), docIDs, chunks); err != nil {
		return err
	}
	// The store has committed; the keyword backend follows it whatever
	// the caller does now.
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

// applyAddLocked adds chunks to the store and the keyword backend. The
// WAL record covering it is the caller's (replace logs one record for
// the whole replace; replay applies a logged one). The caller MUST hold
// i.mu.
func (i *index) applyAddLocked(ctx context.Context, chunks []Chunk) (err error) {
	// Store and keyword backend are host-supplied code on the same
	// dispatch paths; a panic mid-apply leaves the WAL entry unapplied
	// and replay heals it on restart, so an attributed error is the
	// safe degradation.
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

// applyRemoveLocked removes a document from the store and the keyword
// backend; the WAL record is the caller's, as for applyAddLocked. The
// caller MUST hold i.mu.
func (i *index) applyRemoveLocked(ctx context.Context, docID string) (err error) {
	// Same net as applyAddLocked.
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("semantic: apply remove panicked: %v", v)
		}
	}()
	// Collect prior chunk IDs under the same lock as the mutation so the
	// keyword cleanup matches the exact state being removed.
	priorChunkIDs := i.collectChunkIDs(docID)
	if err := i.store.RemoveDoc(ctx, docID); err != nil {
		return err
	}
	if err := i.keywordRemoveDoc(ctx, docID, priorChunkIDs); err != nil {
		return err
	}
	i.opsSinceSnap++
	return nil
}

func (i *index) collectChunkIDs(docID string) []string {
	if i.keyword == nil {
		return nil
	}
	cl, ok := i.store.(chunkLister)
	if !ok {
		return nil
	}
	return cl.ChunkIDsForDoc(docID)
}

func (i *index) keywordIndexChunks(ctx context.Context, chunks []Chunk) (err error) {
	// keyword is a host-supplied KeywordBackend on dispatch paths.
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

// dueSnapshotLocked reports whether an auto-snapshot is due. The caller MUST
// hold i.mu. snapshotEvery<0 means "snapshot after every op" (a test helper);
// >0 is the periodic threshold; 0 (only reachable by building an index
// without Open, which defaults it to 1000) disables auto-snapshot.
func (i *index) dueSnapshotLocked() bool {
	if i.snapshotEvery == 0 {
		return false
	}
	if i.snapshotEvery < 0 {
		return true
	}
	return i.opsSinceSnap >= i.snapshotEvery
}

// snapshotLocked writes a consistent cut of the store and truncates the WAL to
// match. The caller MUST hold i.mu: that is what makes the store copy and the
// WAL truncation bracket exactly the same set of applied operations. A write
// that lands concurrently is then either fully included in the snapshot (its
// WAL entry safely truncated) or fully after it (its WAL entry preserved),
// never straddling the copy→truncate window where it would be lost.
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
	// The snapshot is renamed into place, and a rename is durable only
	// once its directory is synced. Truncating the WAL first, a power loss
	// could keep the empty WAL beside the old snapshot: the writes since
	// lost, and an erasure's removed text back.
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

// Snapshot writes a full snapshot and truncates the WAL, serializing against
// concurrent mutations (Add/Remove) so the on-disk snapshot and the surviving
// WAL together represent one consistent cut of the index.
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
	w := max(k*4, 50)
	return w
}

func (i *index) Query(ctx context.Context, q Query) ([]Hit, error) {
	if math.IsNaN(q.MMRLambda) || math.IsInf(q.MMRLambda, 0) || q.MMRLambda < 0 || q.MMRLambda > 1 {
		return nil, errors.New("semantic: MMRLambda must be finite and between 0 and 1")
	}
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
		return nil, errors.New("semantic: Query.Rerank=true but no Reranker configured on Options")
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

	// Without MMR the reranker sees the whole candidate set, at most
	// width hits, and K cuts its output: cutting to K first left it
	// nothing to promote (at K=1 it could only confirm the top hit).
	if q.MMRLambda > 0 && len(candidates) > 0 {
		candidates = mmr(qv, candidates, q.MMRLambda, k)
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

	// Strip embeddings on the way out, internal stages need them; the
	// caller does not, and shipping ~1.5KB of floats per hit over the
	// wire is wasteful.
	for j := range candidates {
		candidates[j].Chunk.Vec = nil
	}
	return candidates, nil
}

// maxKeywordQueryTerms bounds how many whitespace-separated terms of the
// raw query text the keyword leg of a hybrid query receives. battery/
// search applies the same 64-term cap (maxQueryTerms) in every
// query-text consumer so "an attacker-controlled query cannot amplify
// cost"; this pipeline was the one consumer forwarding raw text to its
// keyword backend, and the shipped MemoryKeyword backend scores
// len(docs) × len(terms) per request — the /query route's 1 MiB body
// cap alone admits ~100k tokens. Distinct terms beyond the cap add
// negligible selectivity.
const maxKeywordQueryTerms = 64

// boundedKeywordText returns q truncated to its first
// maxKeywordQueryTerms terms, counted as keywordTokens splits them (runs
// of letters and digits): counting whitespace fields let "a1,a2,…" pass
// as one. Scanned in place, no intermediate slice: the whole point is to
// not allocate per term on hostile input.
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

// hydrateKeywordHits turns chunk-ID-only keyword results into full
// Hits by looking each chunk up in the store. Filtered-out chunks are
// dropped silently. We need the lookup because the keyword backend
// only knows the chunk IDs; the vectors and metadata live in the
// store. A store that filters in its own query language (PgVectorStore)
// judges the filter itself, with the predicate its vector leg uses.
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
	for _, kh := range raw {
		var c Chunk
		if filtersItself {
			if c, ok = matched[kh.ChunkID]; !ok {
				continue
			}
		} else if c, ok = cl.ChunkByID(kh.ChunkID); !ok || !chunkMatches(&c, filter) {
			continue
		}
		out = append(out, Hit{Chunk: c, Score: kh.Score, Reason: "kw"})
	}
	return out
}

// scopedKeywordHits returns up to width keyword hits inside filter. The
// keyword backend knows nothing of the filter, so its top width hits can
// all lie outside it: cutting there let out-of-scope matches crowd an
// in-scope document out of the keyword leg. The window widens fourfold
// until width hits pass the filter, the backend returns fewer than asked
// (it has no more), or the window covers every chunk the store holds.
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
		// Nilled under i.mu so it is synchronized with every other i.wal
		// reader (replace, snapshotLocked), Close no longer races an
		// in-flight mutation on the wal pointer.
		i.wal = nil
	}
	if c, ok := i.store.(interface{ Close() error }); ok {
		if err := c.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	// Released last, once the snapshot and WAL are closed, so the next
	// writer opens what this one left.
	if i.lock != nil {
		if err := i.lock.Close(); err != nil {
			errs = append(errs, err)
		}
		i.lock = nil
	}
	return errors.Join(errs...)
}

// keywordSearch calls the host keyword backend on the query path; a panic
// becomes an attributed error, as it does on the Add and Remove paths.
func (i *index) keywordSearch(ctx context.Context, text string, top int) (out []KeywordHit, err error) {
	defer func() {
		if v := recover(); v != nil {
			out = nil
			err = fmt.Errorf("keyword backend panicked: %v", v)
		}
	}()
	return i.keyword.Search(ctx, text, top)
}
