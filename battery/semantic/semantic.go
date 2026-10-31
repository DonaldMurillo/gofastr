package semantic

import (
	"context"
	"errors"
	"fmt"
)

// Document is a unit of input to the index. A single Document is split
// by a [Chunker] into one or more [Chunk]s that are individually
// embedded and stored.
type Document struct {
	ID       string         `json:"id"`
	Source   string         `json:"source,omitempty"`
	Text     string         `json:"text"`
	Metadata map[string]any `json:"metadata,omitempty"`
	// Owner is the user id the document belongs to, or empty for one
	// that names nobody. Add copies it onto every chunk, and
	// App.EraseUserData deletes the erased user's chunks from a
	// PgVectorStore.
	Owner string `json:"owner,omitempty"`
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
	// Owner is the owning Document's Owner; Add sets it.
	Owner string    `json:"owner,omitempty"`
	Vec   []float32 `json:"-"`
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
// ignored. Each MetaMatch entry requires the key in the chunk's
// Metadata with a value equal to the entry's, compared as JSON on
// every Store: 5 equals 5.0, [] matches only an empty array, and a nil
// value matches a stored null, never a missing key.
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
	Filter    Filter  `json:"filter"`
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
// semantic package needs for hybrid retrieval. It is defined here so the
// semantic package does not import battery/search in its core path; the
// real wiring happens in hybrid_search.go via an adapter.
type KeywordBackend interface {
	Index(ctx context.Context, id, text string) error
	Delete(ctx context.Context, id string) error
	// Search returns up to top results as (chunkID, score) pairs.
	Search(ctx context.Context, text string, top int) ([]KeywordHit, error)
}

// OwnedKeywordBackend is a [KeywordBackend] that can tag each chunk it
// holds with the user it belongs to. The index uses IndexOwned in place
// of Index when the backend has it, so the keyword copy of a user's text
// is reachable by erasure the way the chunk is. [WrapSearchBackend]
// implements it by setting search.Document.Owner.
type OwnedKeywordBackend interface {
	KeywordBackend
	IndexOwned(ctx context.Context, id, owner, text string) error
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

// ErrIndexLocked is returned by [Open] when another open index, in this
// process or another, is writing Options.Path. Close that one, or open
// with Options.ReadOnly to read beside it.
var ErrIndexLocked = errors.New("semantic: another process has this index open for writing")

// ErrReadOnly is returned by Add, Remove and Snapshot on an index opened
// with Options.ReadOnly.
var ErrReadOnly = errors.New("semantic: index is open read-only")

// ErrClosed is returned by Add, Remove and Snapshot on an index after
// Close: the write would have nowhere durable to go.
var ErrClosed = errors.New("semantic: index is closed")

// Options configures [Open].
type Options struct {
	// Embedder produces vectors. Required.
	Embedder Embedder
	// Chunker splits documents. Defaults to a [FixedWindow] with 512-rune
	// windows and 64-rune overlap if nil.
	Chunker Chunker
	// Store holds vectors. Defaults to an in-memory [FlatStore] if nil.
	//
	// A custom Store must additionally implement Snapshot(path)/LoadSnapshot(path)
	// to be used with Path (persistence), and ChunkIDsForDoc(docID)/ChunkByID(id)/
	// AllChunks() to be used with Keyword (hybrid search). Open returns an error
	// rather than silently degrading if a capability is missing. FlatStore
	// implements all of them.
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
	// ReadOnly opens Path for reading beside a process that writes it,
	// such as `gofastr semantic query` while `semantic watch` runs. It
	// loads the snapshot and every whole WAL record, takes no lock,
	// and never snapshots, truncates or creates anything; Add, Remove
	// and Snapshot return [ErrReadOnly]. It requires Path.
	//
	// Without it an Open with Path takes an exclusive lock on the
	// directory (a "lock" file in it), held until Close, and a second
	// writer gets [ErrIndexLocked]: each writer snapshots and truncates
	// the WAL the other appends to.
	ReadOnly bool
	// SnapshotEvery is the number of mutating ops after which the index
	// automatically flushes a full snapshot and truncates the WAL. 0
	// means the default, 1000; -1 snapshots after every op (mostly for
	// tests).
	SnapshotEvery int
}

// Open constructs an Index from Options. It returns an error if no
// Embedder is supplied. If Options.Path is set, Open loads any
// existing snapshot and replays the WAL before returning.
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
	// Fail closed on a custom Store that can't support the requested features,
	// rather than silently degrading (persistence that never persists; hybrid
	// search that drops every keyword hit; deletes that leak stale keyword
	// entries). The default FlatStore satisfies both capabilities.
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
	// A MemoryKeyword holds the writes of this process only. Over a store
	// every replica writes, another replica's documents would never reach
	// this one's keyword leg, and hybrid results would differ by replica.
	if _, local := opts.Keyword.(*MemoryKeyword); local {
		if _, shared := opts.Store.(sharedStore); shared {
			return nil, fmt.Errorf("semantic: Options.Keyword is a MemoryKeyword, which holds one process's writes, but Store %T is shared by every replica: use the store's own keyword index (PgVectorStore.Keyword()) or a shared search backend (WrapSearchBackend)", opts.Store)
		}
	}
	if pk, ok := opts.Keyword.(pgKeyword); ok && opts.Store != Store(pk.s) {
		return nil, errors.New("semantic: Options.Keyword is the keyword index of another store than Options.Store; its hits would not hydrate")
	}
	// A durable store records the model its vectors came from; vectors of
	// another model of the same dimension would rank as one space.
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
	idx.storeDim() // seed the cache at startup, not on a write
	return idx, nil
}
