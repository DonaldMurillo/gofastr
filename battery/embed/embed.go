package embed

import (
	"context"
	"errors"
)

// Document is a unit of input to the index. A single Document is split
// by a [Chunker] into one or more [Chunk]s that are individually
// embedded and stored.
type Document struct {
	ID       string         `json:"id"`
	Source   string         `json:"source,omitempty"`
	Text     string         `json:"text"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Chunk is a stored, embedded slice of a Document. Vec is owned by the
// store and is L2-normalized so cosine similarity reduces to a dot
// product.
type Chunk struct {
	ID       string         `json:"id"`
	DocID    string         `json:"doc_id"`
	Source   string         `json:"source,omitempty"`
	Text     string         `json:"text"`
	Offset   [2]int         `json:"offset"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Vec      []float32      `json:"-"`
}

// Hit is a single retrieval result. Reason describes which stage of
// the pipeline produced the score ("vec", "kw", "hybrid", "mmr",
// "rerank") so callers can debug ranking behaviour.
type Hit struct {
	Chunk  Chunk   `json:"chunk"`
	Score  float64 `json:"score"`
	Reason string  `json:"reason,omitempty"`
}

// Filter restricts the candidate set before scoring. Empty fields are
// ignored. MetaMatch entries are checked for exact equality against
// the chunk's Metadata.
type Filter struct {
	Source    string         `json:"source,omitempty"`
	Kind      string         `json:"kind,omitempty"`
	MetaMatch map[string]any `json:"meta_match,omitempty"`
}

// Query is the input to [Index.Query].
//
// Zero-value semantics:
//   - K=0  → 10
//   - Hybrid=false and MMRLambda=0 → pure vector retrieval, no
//     diversity reranking.
type Query struct {
	Text      string  `json:"text"`
	K         int     `json:"k,omitempty"`
	Filter    Filter  `json:"filter,omitempty"`
	Hybrid    bool    `json:"hybrid,omitempty"`
	MMRLambda float64 `json:"mmr_lambda,omitempty"`
	Rerank    bool    `json:"rerank,omitempty"`
}

// Stats describes the current state of an [Index].
type Stats struct {
	Docs   int    `json:"docs"`
	Chunks int    `json:"chunks"`
	Dim    int    `json:"dim"`
	Model  string `json:"model"`
}

// Embedder turns text into fixed-dimension vectors. Implementations
// MUST produce L2-normalized output so the store can rely on dot
// product as cosine similarity.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Dim() int
	Name() string
}

// Reranker is an optional second-stage scorer applied to candidate
// hits before they are returned. It receives the original query text
// (not its vector) so cross-encoder rerankers can score query/chunk
// pairs jointly. The returned slice may be reordered or truncated;
// score values should reflect the reranker's confidence.
type Reranker interface {
	Rerank(ctx context.Context, query string, hits []Hit) ([]Hit, error)
}

// KeywordBackend is the subset of the battery/search backend the
// embed package needs for hybrid retrieval. It is defined here so the
// embed package does not import battery/search in its core path; the
// real wiring happens in hybrid_search.go via an adapter.
type KeywordBackend interface {
	Index(ctx context.Context, id, text string) error
	Delete(ctx context.Context, id string) error
	// Search returns up to top results as (chunkID, score) pairs.
	Search(ctx context.Context, text string, top int) ([]KeywordHit, error)
}

// KeywordHit is one result from a [KeywordBackend].
type KeywordHit struct {
	ChunkID string
	Score   float64
}

// Chunker splits a [Document] into [Chunk]s. Implementations MUST
// produce chunks with stable IDs derived from the document and chunk
// offset so re-indexing the same content does not duplicate entries.
type Chunker interface {
	Chunk(doc Document) ([]Chunk, error)
}

// Store is the per-app vector store. It owns the embedding lifecycle
// for added documents and exposes a candidate-generation API the
// retrieval pipeline composes on top of.
type Store interface {
	Add(ctx context.Context, chunks []Chunk) error
	RemoveDoc(ctx context.Context, docID string) error
	// Candidates returns up to top chunks by cosine similarity to qv,
	// after applying filter. The returned slice MUST NOT be retained;
	// callers should copy if needed.
	Candidates(ctx context.Context, qv []float32, filter Filter, top int) ([]Hit, error)
	Stats() Stats
}

// Index is the public handle returned by [Open]. It composes a
// [Chunker], [Embedder] and [Store] into the read/write API GoFastr
// apps consume.
type Index interface {
	Add(ctx context.Context, docs ...Document) error
	Remove(ctx context.Context, docIDs ...string) error
	Query(ctx context.Context, q Query) ([]Hit, error)
	// Snapshot persists the current state to the path configured via
	// [Options.Path]. It is a no-op if no path was configured.
	Snapshot() error
	Stats() Stats
	Close() error
}

// Options configures [Open].
type Options struct {
	// Embedder produces vectors. Required.
	Embedder Embedder
	// Chunker splits documents. Defaults to a [FixedWindow] with 512-rune
	// windows and 64-rune overlap if nil.
	Chunker Chunker
	// Store holds vectors. Defaults to an in-memory [FlatStore] if nil.
	Store Store
	// Keyword enables hybrid retrieval. When set, chunk text is
	// mirrored into this backend at Add time and consulted during
	// Query when [Query.Hybrid] is true. When nil, hybrid queries
	// silently fall back to pure vector retrieval. Use
	// [NewMemoryKeyword] for a zero-dep in-process backend or wrap any
	// [battery/search].Backend with [WrapSearchBackend].
	Keyword KeywordBackend
	// Reranker is the optional second-stage scorer. When nil,
	// [Query.Rerank] requests error out so silent quality loss is
	// impossible.
	Reranker Reranker
	// Path is the directory where the snapshot and WAL live. When
	// empty, the index runs purely in memory and [Index.Snapshot] is a
	// no-op. When set, [Open] loads any existing snapshot, replays the
	// WAL on top, and persists subsequent writes through the WAL.
	Path string
	// SnapshotEvery is the number of mutating ops after which the index
	// automatically flushes a full snapshot and truncates the WAL. 0
	// disables auto-snapshot; -1 snapshots after every op (mostly for
	// tests). The default is 1000.
	SnapshotEvery int
}

// Open constructs an Index from Options. It returns an error if no
// Embedder is supplied. If Options.Path is set, Open loads any
// existing snapshot and replays the WAL before returning.
func Open(opts Options) (Index, error) {
	if opts.Embedder == nil {
		return nil, errors.New("embed: Options.Embedder is required")
	}
	if opts.Chunker == nil {
		opts.Chunker = NewFixedWindow(512, 64)
	}
	if opts.Store == nil {
		opts.Store = NewFlatStore(opts.Embedder.Dim(), opts.Embedder.Name())
	}
	if opts.SnapshotEvery == 0 {
		opts.SnapshotEvery = 1000
	}
	idx := &index{
		embedder:      opts.Embedder,
		chunker:       opts.Chunker,
		store:         opts.Store,
		keyword:       opts.Keyword,
		reranker:      opts.Reranker,
		path:          opts.Path,
		snapshotEvery: opts.SnapshotEvery,
	}
	if err := idx.loadAndReplay(); err != nil {
		return nil, err
	}
	return idx, nil
}
