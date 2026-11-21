package semantic

import (
	"context"
	"errors"
	"fmt"
)

// Document is a unit of input to the index, split by a Chunker into Chunks.
type Document struct {
	ID       string         `json:"id"`
	Source   string         `json:"source,omitempty"`
	Text     string         `json:"text"`
	Metadata map[string]any `json:"metadata,omitempty"`
	// Owner is the user id the document belongs to; erasing that user deletes
	// its chunks.
	Owner string `json:"owner,omitempty"`
}

// Chunk is a stored, embedded slice of a Document; Vec is L2-normalized.
type Chunk struct {
	ID       string         `json:"id"`
	DocID    string         `json:"doc_id"`
	Source   string         `json:"source,omitempty"`
	Text     string         `json:"text"`
	Offset   [2]int         `json:"offset"`
	Metadata map[string]any `json:"metadata,omitempty"`
	// Owner is the owning Document's Owner; Add sets it.
	Owner string    `json:"owner,omitempty"`
	Vec   []float32 `json:"-"`
}

// Hit is one retrieval result; Reason names the stage that scored it.
type Hit struct {
	Chunk  Chunk   `json:"chunk"`
	Score  float64 `json:"score"`
	Reason string  `json:"reason,omitempty"`
}

// Filter restricts the candidate set; each MetaMatch entry is JSON equality on
// a metadata key.
type Filter struct {
	Source    string         `json:"source,omitempty"`
	Kind      string         `json:"kind,omitempty"`
	MetaMatch map[string]any `json:"meta_match,omitempty"`
}

// Query is the input to Index.Query. K defaults to 10.
type Query struct {
	Text      string  `json:"text"`
	K         int     `json:"k,omitempty"`
	Filter    Filter  `json:"filter"`
	Hybrid    bool    `json:"hybrid,omitempty"`
	MMRLambda float64 `json:"mmr_lambda,omitempty"`
	Rerank    bool    `json:"rerank,omitempty"`
}

func defaultTop(top int) int {
	if top <= 0 {
		return 10
	}
	return top
}

// Stats describes the current state of an [Index].
type Stats struct {
	Docs   int    `json:"docs"`
	Chunks int    `json:"chunks"`
	Dim    int    `json:"dim"`
	Model  string `json:"model"`
}

// Embedder turns text into L2-normalized fixed-dimension vectors.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Dim() int
	Name() string
}

// Reranker is an optional second-stage scorer that receives the query text and
// the candidate hits.
type Reranker interface {
	Rerank(ctx context.Context, query string, hits []Hit) ([]Hit, error)
}

// KeywordBackend is the keyword index hybrid retrieval reads.
type KeywordBackend interface {
	Index(ctx context.Context, id, text string) error
	Delete(ctx context.Context, id string) error
	// Search returns up to top results as (chunkID, score) pairs.
	Search(ctx context.Context, text string, top int) ([]KeywordHit, error)
}

// OwnedKeywordBackend is a KeywordBackend that tags each chunk with its owner,
// so erasure reaches it.
type OwnedKeywordBackend interface {
	KeywordBackend
	IndexOwned(ctx context.Context, id, owner, text string) error
}

// KeywordHit is one result from a [KeywordBackend].
type KeywordHit struct {
	ChunkID string
	Score   float64
}

// Chunker splits a Document into Chunks with IDs stable for the same content.
type Chunker interface {
	Chunk(doc Document) ([]Chunk, error)
}

// Store is the per-app vector store.
type Store interface {
	Add(ctx context.Context, chunks []Chunk) error
	RemoveDoc(ctx context.Context, docID string) error
	// Candidates returns up to top chunks by cosine similarity to qv inside
	// filter; callers must not retain the slice.
	Candidates(ctx context.Context, qv []float32, filter Filter, top int) ([]Hit, error)
	Stats() Stats
}

// Index is the handle Open returns.
type Index interface {
	Add(ctx context.Context, docs ...Document) error
	Remove(ctx context.Context, docIDs ...string) error
	Query(ctx context.Context, q Query) ([]Hit, error)
	// Snapshot persists the current state to Options.Path; a no-op without
	// one.
	Snapshot() error
	Stats() Stats
	Close() error
}

// ErrIndexLocked is returned by Open when another open index is writing
// Options.Path.
var ErrIndexLocked = errors.New("semantic: another process has this index open for writing")

// ErrReadOnly is returned by Add, Remove and Snapshot on a read-only index.
var ErrReadOnly = errors.New("semantic: index is open read-only")

// ErrClosed is returned by Add, Remove and Snapshot after Close.
var ErrClosed = errors.New("semantic: index is closed")

// Options configures [Open].
type Options struct {
	// Embedder produces vectors. Required.
	Embedder Embedder
	// Chunker splits documents. Defaults to FixedWindow(512, 64).
	Chunker Chunker
	// Store holds vectors. Defaults to an in-memory FlatStore; Path and
	// Keyword need a store with snapshot and chunk-listing support.
	Store Store
	// Keyword enables hybrid retrieval; nil makes hybrid queries fall back to
	// vectors.
	Keyword KeywordBackend
	// Reranker is the optional second-stage scorer; a Rerank query without one
	// errors.
	Reranker Reranker
	// Path is the directory of the snapshot and WAL; empty runs in memory.
	Path string
	// ReadOnly opens Path beside a writer: no lock, and Add, Remove and
	// Snapshot return ErrReadOnly.
	ReadOnly bool
	// SnapshotEvery is the mutating ops between automatic snapshots: 0 means
	// 1000, -1 every op.
	SnapshotEvery int
}

// Open constructs an Index; it errors without an Embedder and loads Path's
// snapshot and WAL.
func Open(opts Options) (Index, error) {
	if opts.Embedder == nil {
		return nil, errors.New("semantic: Options.Embedder is required")
	}
	if opts.ReadOnly && opts.Path == "" {
		return nil, errors.New("semantic: Options.ReadOnly needs Options.Path: an in-memory index has nothing to read")
	}
	if opts.Chunker == nil {
		opts.Chunker = NewFixedWindow(512, 64)
	}
	if opts.Store == nil {
		opts.Store = NewFlatStore(opts.Embedder.Dim(), opts.Embedder.Name())
	}
	if opts.Path != "" {
		if _, ok := opts.Store.(snapshotter); !ok {
			return nil, fmt.Errorf("semantic: Options.Path is set but Store %T cannot persist: it must implement Snapshot(path)/LoadSnapshot(path)", opts.Store)
		}
	}
	if opts.Keyword != nil {
		if _, ok := opts.Store.(chunkLister); !ok {
			return nil, fmt.Errorf("semantic: Options.Keyword is set but Store %T cannot list chunks: it must implement ChunkIDsForDoc(docID)/ChunkByID(id) for hybrid search", opts.Store)
		}
	}
	if _, local := opts.Keyword.(*MemoryKeyword); local {
		if _, shared := opts.Store.(sharedStore); shared {
			return nil, fmt.Errorf("semantic: Options.Keyword is a MemoryKeyword, which holds one process's writes, but Store %T is shared by every replica: use the store's own keyword index (PgVectorStore.Keyword()) or a shared search backend (WrapSearchBackend)", opts.Store)
		}
	}
	if pk, ok := opts.Keyword.(pgKeyword); ok && opts.Store != Store(pk.s) {
		return nil, errors.New("semantic: Options.Keyword is the keyword index of another store than Options.Store; its hits would not hydrate")
	}
	if mb, ok := opts.Store.(modelBinder); ok {
		ctx, cancel := context.WithTimeout(context.Background(), bindTimeout)
		err := mb.bindModel(ctx, opts.Embedder.Name())
		cancel()
		if err != nil {
			return nil, err
		}
	}
	if opts.SnapshotEvery == 0 {
		opts.SnapshotEvery = 1000
	}
	idx := &index{
		restoring:     true,
		embedder:      opts.Embedder,
		chunker:       opts.Chunker,
		store:         opts.Store,
		keyword:       opts.Keyword,
		reranker:      opts.Reranker,
		path:          opts.Path,
		readOnly:      opts.ReadOnly,
		snapshotEvery: opts.SnapshotEvery,
	}
	err := idx.loadAndReplay()
	if err == nil {
		err = idx.rebuildKeyword()
	}
	if err != nil {
		if idx.wal != nil {
			_ = idx.wal.close()
		}
		if idx.lock != nil {
			_ = idx.lock.Close()
		}
		return nil, err
	}
	idx.restoring = false
	idx.storeDim()
	idx.registerEraser()
	return idx, nil
}
