package semantic

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/internal/jsonexact"
	"github.com/lib/pq"
)

// PgVectorStore is a durable [Store] backed by a Postgres table with a
// pgvector embedding column. It is the production counterpart to
// [FlatStore]: instead of a brute-force scan of an in-memory slice, chunks
// live in the database so multiple app replicas share a single index and
// survive process restarts with no snapshot/WAL plumbing.
//
// Ranking uses pgvector's cosine-distance operator (<=>). Because the
// embedder contract produces L2-normalized vectors, cosine similarity equals
// the dot product [FlatStore] scores by, so the two stores produce the same
// top-K order given identical vectors (see TestPgVectorRankingParity). The
// distance is computed server-side; [Hit.Score] is set to 1-distance.
//
// The store implements the [chunkLister] capability (ChunkIDsForDoc /
// ChunkByID / AllChunks) so it composes with hybrid/keyword retrieval through
// [Open], and replaces a document's chunks in one locked transaction
// (replaceDocs), so a failed write keeps the old version and replicas
// replacing one document do not interleave. It deliberately does NOT
// implement [snapshotter]: a Postgres table
// IS the durable copy, so the gob snapshot path is meaningless, pairing this
// store with Options.Path fails closed at Open with an actionable error.
//
// No third-party pgvector Go driver is required. Vectors are encoded in
// pgvector's text format ([0.1,0.2,…]) and bound as ordinary string
// parameters, cast to the vector type in SQL.
type PgVectorStore struct {
	db           *sql.DB
	table        string // validated configured table name
	quotedTable  string // validated + quoted table identifier
	quotedDocIdx string // validated + quoted doc_id index name
	dim          int
	model        string

	// afterDelete is a test hook run right after a document's chunks are
	// deleted, before its new chunks are inserted.
	afterDelete func()
}

// PgVectorConfig configures a PgVectorStore.
type PgVectorConfig struct {
	// Table is the destination table. Defaults to "embed_chunks".
	Table string

	// Dim is the fixed embedding dimension. REQUIRED: a pgvector column
	// is dimensionally fixed at CREATE TABLE time, so the store must know
	// the dimension up front. Set it from the embedder:
	//
	//	PgVectorConfig{Dim: embedder.Dim()}
	//
	// A zero Dim is rejected at construction.
	Dim int

	// Model records the embedder model name in [Stats] for diagnostics.
	// Optional; [Index.Stats] fills it from the embedder when empty.
	Model string
}

// statsTimeout bounds the context-less [PgVectorStore.Stats] and the
// chunkLister lookups (which also lack a ctx in the interface). It is short
// on purpose: Stats is a best-effort snapshot, never a load-bearing query.
const statsTimeout = 3 * time.Second

// NewPgVector constructs a PgVectorStore. The table name is validated with
// the same core/query.SafeIdent used across the framework, and Dim must be
// positive. The returned store is usable after [PgVectorStore.EnsureSchema]
// creates the table; NewPgVector itself performs no I/O.
func NewPgVector(db *sql.DB, cfg PgVectorConfig) (*PgVectorStore, error) {
	if db == nil {
		return nil, errors.New("semantic: nil db")
	}
	if cfg.Dim <= 0 {
		return nil, fmt.Errorf("semantic: PgVectorConfig.Dim must be > 0 (got %d); set it from the embedder's Dim()", cfg.Dim)
	}
	table := cfg.Table
	if table == "" {
		table = "embed_chunks"
	}
	safe, err := query.SafeIdent(table)
	if err != nil {
		return nil, fmt.Errorf("semantic: invalid pgvector table name %q: %w", table, err)
	}
	// App.EraseUserData deletes the erased user's chunks (Document.Owner).
	// Registered under the table's own name, so two stores in one app do
	// not replace each other.
	datexport.RegisterEraser(datexport.DataEraser{
		Name: safe, Source: "semantic", Table: safe, Column: "owner",
		Mode: datexport.EraseDelete,
	})
	return &PgVectorStore{
		db:           db,
		table:        safe,
		quotedTable:  query.QuoteFolded(safe),
		quotedDocIdx: query.QuoteIndexName(docIndexName(safe)),
		dim:          cfg.Dim,
		model:        cfg.Model,
	}, nil
}

// docIndexName names the doc_id index: table+"_doc_id_idx" as Postgres
// cuts it at 63 bytes, the name every earlier build created. A 63-byte
// table name cuts to the table's own name, and CREATE INDEX IF NOT EXISTS
// then skipped the index, so that one name ends in a hash of the table
// instead.
func docIndexName(table string) string {
	name := table + "_doc_id_idx"
	if len(name) > 63 {
		name = name[:63]
	}
	if name != table {
		return name
	}
	h := fnv.New32a()
	h.Write([]byte(table))
	tag := fmt.Sprintf("_%08x_doc_id_idx", h.Sum32())
	return table[:63-len(tag)] + tag
}

// EnsureSchema creates the vector extension and chunks table if they do not
// already exist. Idempotent, safe to call on every boot.
//
// The vector extension requires a one-time CREATE EXTENSION, which on managed
// Postgres (RDS, Cloud SQL, …) is a superuser privilege the app role often
// lacks. We tolerate a permission failure here (the extension may already be
// installed by an admin) and let the subsequent CREATE TABLE surface a clear,
// actionable error if the vector type is genuinely missing.
func (s *PgVectorStore) EnsureSchema(ctx context.Context) error {
	if err := query.RefuseCaseKeptTables(ctx, s.db, s.table); err != nil {
		return fmt.Errorf("semantic: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, "CREATE EXTENSION IF NOT EXISTS vector"); err != nil {
		// 42501 = insufficient_privilege: the role can't create the
		// extension, but it may already be present. Fall through and let
		// CREATE TABLE decide. Anything else is a real failure.
		if pqCode(err) != "42501" {
			return fmt.Errorf("semantic: create vector extension: %w. An admin may need to run: CREATE EXTENSION vector;", err)
		}
	}
	// The embedding column dimension is the only interpolated value here;
	// it is a validated positive int. The table name is validated + quoted.
	//gofastr:allow(GOFASTR1415) a chunk table is an embedding index derived from documents the host stores and exports itself, and a restore re-embeds them; NewPgVector registers erasure by Document.Owner
	createTable := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		id           TEXT PRIMARY KEY,
		doc_id       TEXT NOT NULL,
		source       TEXT NOT NULL DEFAULT '',
		kind         TEXT NOT NULL DEFAULT '',
		text         TEXT NOT NULL DEFAULT '',
		offset_start INTEGER NOT NULL DEFAULT 0,
		offset_end   INTEGER NOT NULL DEFAULT 0,
		meta         JSONB NOT NULL DEFAULT '{}',
		embedding    vector(%d) NOT NULL,
		owner        TEXT NOT NULL DEFAULT ''
	)`, s.quotedTable, s.dim)
	if _, err := s.db.ExecContext(ctx, createTable); err != nil {
		// 42704 = undefined_object: the "vector" type does not exist,
		// meaning the extension is not installed in this database.
		if pqCode(err) == "42704" {
			return errors.New("semantic: pgvector extension is not installed in this database: an admin must run: CREATE EXTENSION vector;")
		}
		return fmt.Errorf("semantic: create pgvector table: %w", err)
	}
	// A table an earlier release created has no owner column.
	if _, err := s.db.ExecContext(ctx, fmt.Sprintf(
		`ALTER TABLE %s ADD COLUMN IF NOT EXISTS owner TEXT NOT NULL DEFAULT ''`, s.quotedTable)); err != nil {
		return fmt.Errorf("semantic: add owner column: %w", err)
	}
	// B-tree index on doc_id so RemoveDoc and ChunkIDsForDoc are index
	// scans, not full table scans. No ANN index by default: brute-force
	// cosine is correct and matches FlatStore's semantics, and ivfflat
	// needs data to train while hnsw may be absent on old pgvector. An
	// operator can add `CREATE INDEX ... USING hnsw (embedding
	// vector_cosine_ops)` for scale without changing this code.
	docIdx := "CREATE INDEX IF NOT EXISTS " + s.quotedDocIdx + " ON " + s.quotedTable + " (doc_id)"
	if _, err := s.db.ExecContext(ctx, docIdx); err != nil {
		return fmt.Errorf("semantic: create doc_id index: %w", err)
	}
	return nil
}

// Add inserts (or replaces by id) chunks. Vectors are L2-normalized in place
// so cosine similarity reduces to a dot product, mirroring [FlatStore.Add].
// A vector whose length differs from the configured dimension is rejected.
func (s *PgVectorStore) Add(ctx context.Context, chunks []Chunk) error {
	if len(chunks) == 0 {
		return nil
	}
	metas, err := s.prepareChunks(chunks)
	if err != nil {
		return err
	}
	// The batch is one transaction, so a chunk that fails leaves none of
	// the batch stored, as FlatStore.Add does.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("semantic: begin add: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := s.insertChunks(ctx, tx, chunks, metas); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("semantic: commit add: %w", err)
	}
	return nil
}

// prepareChunks checks each chunk's dimension, normalizes its vector in
// place and marshals its metadata, before any statement runs.
func (s *PgVectorStore) prepareChunks(chunks []Chunk) ([][]byte, error) {
	for i := range chunks {
		if len(chunks[i].Vec) != s.dim {
			return nil, errVecDim(s.dim, len(chunks[i].Vec))
		}
	}
	metas := make([][]byte, len(chunks))
	for i := range chunks {
		meta, err := marshalMeta(chunks[i].Metadata)
		if err != nil {
			return nil, fmt.Errorf("semantic: marshal meta for chunk %q: %w", chunks[i].ID, err)
		}
		metas[i] = meta
	}
	for i := range chunks {
		normalize(chunks[i].Vec)
	}
	return metas, nil
}

// insertChunks upserts chunks inside tx.
func (s *PgVectorStore) insertChunks(ctx context.Context, tx *sql.Tx, chunks []Chunk, metas [][]byte) error {
	// UPSERT by id so a direct caller (or a re-EnsureSchema) cannot produce
	// duplicate-primary-key errors. The index layer replaces by document
	// through replaceDocs first, so this is defense in depth. The target is
	// aliased so a table named excluded does not collide with the EXCLUDED
	// pseudo-row.
	stmt := fmt.Sprintf(`INSERT INTO %s AS t
		(id, doc_id, source, kind, text, offset_start, offset_end, meta, embedding, owner)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::vector, $10)
	ON CONFLICT (id) DO UPDATE SET
		doc_id       = EXCLUDED.doc_id,
		source       = EXCLUDED.source,
		kind         = EXCLUDED.kind,
		text         = EXCLUDED.text,
		offset_start = EXCLUDED.offset_start,
		offset_end   = EXCLUDED.offset_end,
		meta         = EXCLUDED.meta,
		embedding    = EXCLUDED.embedding,
		owner        = EXCLUDED.owner`, s.quotedTable)
	for i := range chunks {
		c := &chunks[i]
		kind, _ := c.Metadata["kind"].(string)
		if _, err := tx.ExecContext(ctx, stmt,
			c.ID, c.DocID, c.Source, kind, c.Text,
			c.Offset[0], c.Offset[1], metas[i], encodeVector(c.Vec), c.Owner,
		); err != nil {
			return fmt.Errorf("semantic: insert chunk %q: %w", c.ID, err)
		}
	}
	return nil
}

// RemoveDoc deletes every chunk belonging to docID, under the same
// per-document lock replaceDocs takes.
func (s *PgVectorStore) RemoveDoc(ctx context.Context, docID string) error {
	return s.replaceDocs(ctx, []string{docID}, nil)
}

// replaceDocs deletes every chunk of docIDs and inserts chunks in one
// transaction, so a failed insert (metadata JSON cannot carry, a byte
// Postgres refuses) keeps the old version instead of losing both.
//
// The transaction first takes a transaction-scoped advisory lock per
// document. Replicas sharing the table each hold only their own process
// mutex, and under READ COMMITTED a DELETE cannot see another
// transaction's uncommitted inserts: without the lock two replaces of one
// document could run delete, delete, insert, insert and keep chunks of
// both versions. Keys are taken in ascending order so two batches naming
// the same documents cannot deadlock each other.
func (s *PgVectorStore) replaceDocs(ctx context.Context, docIDs []string, chunks []Chunk) error {
	if len(docIDs) == 0 && len(chunks) == 0 {
		return nil
	}
	metas, err := s.prepareChunks(chunks)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("semantic: begin replace: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, key := range s.docLockKeys(docIDs) {
		//gofastr:allow(GOFASTR1502) locks are taken one at a time in ascending key order so two batches cannot deadlock; one statement would not fix the order
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, key); err != nil {
			return fmt.Errorf("semantic: lock docs: %w", err)
		}
	}
	if len(docIDs) > 0 {
		stmt := fmt.Sprintf(`DELETE FROM %s WHERE doc_id = ANY($1)`, s.quotedTable)
		if _, err := tx.ExecContext(ctx, stmt, pq.Array(docIDs)); err != nil {
			return fmt.Errorf("semantic: remove docs: %w", err)
		}
	}
	if s.afterDelete != nil {
		//gofastr:allow(recovercallback) test-only hook, set only by package tests; never host code
		s.afterDelete()
	}
	if err := s.insertChunks(ctx, tx, chunks, metas); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("semantic: commit replace: %w", err)
	}
	return nil
}

// docLockKeys returns the advisory-lock key of each distinct document,
// sorted ascending. A key hashes the table and the document ID, each
// length-prefixed so no pair of names can spell another pair. Two
// documents that collide share a lock, which only serializes them.
func (s *PgVectorStore) docLockKeys(docIDs []string) []int64 {
	keys := make([]int64, 0, len(docIDs))
	for _, id := range docIDs {
		h := fnv.New64a()
		var n [8]byte
		for _, part := range []string{s.quotedTable, id} {
			binary.BigEndian.PutUint64(n[:], uint64(len(part)))
			h.Write(n[:])
			h.Write([]byte(part))
		}
		keys = append(keys, int64(h.Sum64())) //gofastr:allow(intwrap) a lock key is any 64-bit pattern; wrapping is the intent
	}
	slices.Sort(keys)
	return slices.Compact(keys)
}

// Candidates returns up to top chunks by cosine similarity to qv, applying
// filter to the candidate set first. The query vector is normalized so cosine
// == dot product for already-normalized stored vectors. Ranking is computed
// server-side with <=> (cosine distance); ties break by chunk id ascending,
// matching [FlatStore.Candidates]. Returned hits carry Chunk.Vec populated
// (MMR needs it; [Index.Query] strips it on the way out).
func (s *PgVectorStore) Candidates(ctx context.Context, qv []float32, filter Filter, top int) ([]Hit, error) {
	if len(qv) != s.dim {
		return nil, errVecDim(s.dim, len(qv))
	}
	if top <= 0 {
		top = 10
	}
	q := make([]float32, len(qv))
	copy(q, qv)
	normalize(q)

	args := []any{encodeVector(q)} // $1 = query vector
	var where strings.Builder
	where.WriteString("TRUE")
	args, err := appendPgFilter(&where, args, filter)
	if err != nil {
		return nil, err
	}
	args = append(args, top)
	limitIdx := len(args)

	// $1::vector is referenced twice (distance projection + ORDER BY);
	// PostgreSQL permits reusing a positional parameter within a statement.
	stmt := fmt.Sprintf(`SELECT id, doc_id, source, text, offset_start, offset_end, meta, embedding, owner,
		embedding <=> $1::vector AS distance
	FROM %s
	WHERE %s
	ORDER BY embedding <=> $1::vector ASC, id ASC
	LIMIT $%d`, s.quotedTable, where.String(), limitIdx)

	rows, err := s.db.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, fmt.Errorf("semantic: pgvector candidates: %w", err)
	}
	defer rows.Close()
	var hits []Hit
	for rows.Next() {
		var (
			c         Chunk
			metaBytes []byte
			embText   string
			distance  float64
		)
		if err := rows.Scan(
			&c.ID, &c.DocID, &c.Source, &c.Text,
			&c.Offset[0], &c.Offset[1], &metaBytes, &embText, &c.Owner, &distance,
		); err != nil {
			return nil, fmt.Errorf("semantic: pgvector scan: %w", err)
		}
		if err := unmarshalMeta(metaBytes, &c.Metadata); err != nil {
			return nil, fmt.Errorf("semantic: pgvector unmarshal meta: %w", err)
		}
		vec, err := parseVector(embText)
		if err != nil {
			return nil, fmt.Errorf("semantic: pgvector parse embedding: %w", err)
		}
		c.Vec = vec
		// A zero vector (text with no tokens) has no direction: pgvector's
		// cosine distance to it is NaN. Score it 0, as FlatStore's dot over
		// a normalized query does; a NaN score fails JSON encoding.
		if math.IsNaN(distance) {
			distance = 1
		}
		hits = append(hits, Hit{Chunk: c, Score: 1 - distance, Reason: "vec"})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("semantic: pgvector rows: %w", err)
	}
	return hits, nil
}

// Stats returns a snapshot of store state. Docs is the distinct doc_id count;
// Chunks is the row count. Dim/Model come from config. The counts are read
// with a background context + short timeout because the [Store] interface
// gives Stats no ctx, a failed/missing table yields zero counts rather than
// an error, matching FlatStore's "never error from Stats" behaviour.
func (s *PgVectorStore) Stats() Stats {
	ctx, cancel := context.WithTimeout(context.Background(), statsTimeout)
	defer cancel()
	var docs, chunks int
	_ = s.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT COUNT(DISTINCT doc_id), COUNT(*) FROM %s`, s.quotedTable),
	).Scan(&docs, &chunks)
	return Stats{Docs: docs, Chunks: chunks, Dim: s.dim, Model: s.model}
}

// ChunkIDsForDoc returns the IDs of every chunk belonging to docID. Part of
// the [chunkLister] capability; used to purge a doc's keyword entries on
// remove.
func (s *PgVectorStore) ChunkIDsForDoc(docID string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), statsTimeout)
	defer cancel()
	rows, err := s.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT id FROM %s WHERE doc_id = $1 ORDER BY id`, s.quotedTable),
		docID,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil
		}
		ids = append(ids, id)
	}
	// A failure part-way through Next stops the loop with no error of its
	// own. Returning the short list would hand the caller a partial doc
	// that reads as complete, and this list drives RemoveDoc's keyword
	// purge: the orphaned entries would point at a doc that is gone. Fail
	// the same way a scan error already does.
	if err := rows.Err(); err != nil {
		return nil
	}
	return ids
}

// ChunkByID returns the chunk with the given ID. Part of chunkLister; used to
// hydrate keyword-only hits into full Hits.
func (s *PgVectorStore) ChunkByID(id string) (Chunk, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), statsTimeout)
	defer cancel()
	c, err := s.scanOne(ctx, `WHERE id = $1`, id)
	if err != nil {
		return Chunk{}, false
	}
	return c, true
}

// chunksByIDMatching returns the chunks among ids that pass filter,
// judged by the SQL predicate Candidates applies, in one query. The
// hybrid keyword leg hydrates through it: rechecking decoded metadata in
// Go would compare JSON numbers as float64, where 2^53+1 equals 2^53. A
// failed query matches nothing, as a missing chunk does.
func (s *PgVectorStore) chunksByIDMatching(ctx context.Context, ids []string, filter Filter) map[string]Chunk {
	ctx, cancel := context.WithTimeout(ctx, statsTimeout)
	defer cancel()
	var where strings.Builder
	where.WriteString("WHERE id = ANY($1)")
	args, err := appendPgFilter(&where, []any{pq.Array(ids)}, filter)
	if err != nil {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, s.selectAll(where.String()), args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := make(map[string]Chunk, len(ids))
	for rows.Next() {
		c, err := scanChunkRow(rows)
		if err != nil {
			return nil
		}
		out[c.ID] = c
	}
	if rows.Err() != nil {
		return nil
	}
	return out
}

// appendPgFilter adds filter's predicates to where and their parameters
// to args, numbered after the ones already there.
func appendPgFilter(where *strings.Builder, args []any, filter Filter) ([]any, error) {
	if filter.Source != "" {
		args = append(args, filter.Source)
		fmt.Fprintf(where, " AND source = $%d", len(args))
	}
	if filter.Kind != "" {
		args = append(args, filter.Kind)
		fmt.Fprintf(where, " AND kind = $%d", len(args))
	}
	// MetaMatch is per-key equality, as FlatStore applies it. JSONB
	// containment (meta @> filter) is not: {"tenants": []} is contained in
	// every array. The key and value are both bound; jsonb = compares
	// numbers by value, so 5 and 5.0 are equal here too.
	for _, k := range slices.Sorted(maps.Keys(filter.MetaMatch)) {
		b, err := json.Marshal(filter.MetaMatch[k])
		if err != nil {
			return nil, fmt.Errorf("semantic: marshal meta filter: %w", err)
		}
		args = append(args, k, b)
		fmt.Fprintf(where, " AND meta -> $%d = $%d::jsonb", len(args)-1, len(args))
	}
	return args, nil
}

// AllChunks returns every stored chunk ordered by id. Part of chunkLister;
// used to rebuild the keyword index after a snapshot load.
func (s *PgVectorStore) AllChunks() []Chunk {
	ctx, cancel := context.WithTimeout(context.Background(), statsTimeout)
	defer cancel()
	out, err := s.allChunks(ctx)
	if err != nil {
		return nil
	}
	return out
}

// allChunks is AllChunks with its failure kept. Open's keyword rebuild
// reads through it with no deadline: a large table outlasts the stats
// timeout, and an empty answer would pass for an empty store.
func (s *PgVectorStore) allChunks(ctx context.Context) ([]Chunk, error) {
	rows, err := s.db.QueryContext(ctx, s.selectAll("ORDER BY id"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Chunk
	for rows.Next() {
		c, err := scanChunkRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *PgVectorStore) scanOne(ctx context.Context, whereClause string, args ...any) (Chunk, error) {
	rows, err := s.db.QueryContext(ctx, s.selectAll(whereClause), args...)
	if err != nil {
		return Chunk{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return Chunk{}, sql.ErrNoRows
	}
	c, err := scanChunkRow(rows)
	if err != nil {
		return Chunk{}, err
	}
	return c, rows.Err()
}

func (s *PgVectorStore) selectAll(tail string) string {
	return fmt.Sprintf(`SELECT id, doc_id, source, text, offset_start, offset_end, meta, embedding, owner FROM %s %s`, s.quotedTable, tail)
}

// scanChunkRow scans the canonical 9-column chunk projection. It closes
// nothing; the caller owns rows.
func scanChunkRow(rows *sql.Rows) (Chunk, error) {
	var (
		c         Chunk
		metaBytes []byte
		embText   string
	)
	if err := rows.Scan(
		&c.ID, &c.DocID, &c.Source, &c.Text,
		&c.Offset[0], &c.Offset[1], &metaBytes, &embText, &c.Owner,
	); err != nil {
		return Chunk{}, err
	}
	if err := unmarshalMeta(metaBytes, &c.Metadata); err != nil {
		return Chunk{}, err
	}
	vec, err := parseVector(embText)
	if err != nil {
		return Chunk{}, err
	}
	c.Vec = vec
	return c, nil
}

// marshalMeta serializes a chunk's metadata to JSONB bytes, emitting "{}" for
// empty so a MetaMatch key lookup (meta -> key) behaves predictably.
func marshalMeta(m map[string]any) ([]byte, error) {
	if len(m) == 0 {
		return []byte("{}"), nil
	}
	return json.Marshal(m)
}

// unmarshalMeta decodes JSONB bytes into a metadata map; NULL/empty yields nil.
func unmarshalMeta(b []byte, m *map[string]any) error {
	*m = nil
	if len(b) == 0 {
		return nil
	}
	var out map[string]any
	if err := jsonexact.Unmarshal(b, &out); err != nil {
		return err
	}
	*m = out
	return nil
}

// pqCode extracts the SQLSTATE code from a lib/pq error, or "" otherwise.
func pqCode(err error) string {
	if pe, ok := errors.AsType[*pq.Error](err); ok {
		return string(pe.Code)
	}
	return ""
}

// encodeVector renders a vector in pgvector's text input format: [x,y,z].
// 'g' with 32-bit width emits the shortest text that round-trips to the same
// float32, keeping the wire payload compact without precision loss.
func encodeVector(v []float32) string {
	var b strings.Builder
	b.Grow(len(v)*8 + 2)
	b.WriteByte('[')
	for i, x := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(x), 'g', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}

// parseVector decodes pgvector's text output format ([x,y,z]) into []float32.
func parseVector(s string) ([]float32, error) {
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != '[' || s[len(s)-1] != ']' {
		return nil, fmt.Errorf("malformed vector %q", s)
	}
	inner := s[1 : len(s)-1]
	if strings.TrimSpace(inner) == "" {
		return nil, nil
	}
	parts := strings.Split(inner, ",")
	out := make([]float32, 0, len(parts))
	for _, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 32)
		if err != nil {
			return nil, fmt.Errorf("parse vector component %q: %w", p, err)
		}
		out = append(out, float32(f))
	}
	return out, nil
}

// Compile-time assertions that PgVectorStore satisfies the [Store] interface
// and the [chunkLister] capability (so it composes with hybrid retrieval).
// It intentionally does NOT satisfy [snapshotter]: pairing it with
// Options.Path must fail closed at [Open].
var (
	_ Store       = (*PgVectorStore)(nil)
	_ chunkLister = (*PgVectorStore)(nil)
	_ docReplacer = (*PgVectorStore)(nil)
)
