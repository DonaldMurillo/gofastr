package embed

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
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
// [Open]. It deliberately does NOT implement [snapshotter]: a Postgres table
// IS the durable copy, so the gob snapshot path is meaningless — pairing this
// store with Options.Path fails closed at Open with an actionable error.
//
// No third-party pgvector Go driver is required. Vectors are encoded in
// pgvector's text format ([0.1,0.2,…]) and bound as ordinary string
// parameters, cast to the vector type in SQL.
type PgVectorStore struct {
	db           *sql.DB
	quotedTable  string // validated + quoted table identifier
	quotedDocIdx string // validated + quoted doc_id index name
	dim          int
	model        string
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
		return nil, errors.New("embed: nil db")
	}
	if cfg.Dim <= 0 {
		return nil, fmt.Errorf("embed: PgVectorConfig.Dim must be > 0 (got %d); set it from the embedder's Dim()", cfg.Dim)
	}
	table := cfg.Table
	if table == "" {
		table = "embed_chunks"
	}
	safe, err := query.SafeIdent(table)
	if err != nil {
		return nil, fmt.Errorf("embed: invalid pgvector table name %q: %w", table, err)
	}
	return &PgVectorStore{
		db:           db,
		quotedTable:  query.QuoteIdent(safe),
		quotedDocIdx: query.QuoteIdent(safe + "_doc_id_idx"),
		dim:          cfg.Dim,
		model:        cfg.Model,
	}, nil
}

// EnsureSchema creates the vector extension and chunks table if they do not
// already exist. Idempotent — safe to call on every boot.
//
// The vector extension requires a one-time CREATE EXTENSION, which on managed
// Postgres (RDS, Cloud SQL, …) is a superuser privilege the app role often
// lacks. We tolerate a permission failure here (the extension may already be
// installed by an admin) and let the subsequent CREATE TABLE surface a clear,
// actionable error if the vector type is genuinely missing.
func (s *PgVectorStore) EnsureSchema(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, "CREATE EXTENSION IF NOT EXISTS vector"); err != nil {
		// 42501 = insufficient_privilege: the role can't create the
		// extension, but it may already be present. Fall through and let
		// CREATE TABLE decide. Anything else is a real failure.
		if pqCode(err) != "42501" {
			return fmt.Errorf("embed: create vector extension: %w — an admin may need to run: CREATE EXTENSION vector;", err)
		}
	}
	// The embedding column dimension is the only interpolated value here;
	// it is a validated positive int. The table name is validated + quoted.
	createTable := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		id           TEXT PRIMARY KEY,
		doc_id       TEXT NOT NULL,
		source       TEXT NOT NULL DEFAULT '',
		kind         TEXT NOT NULL DEFAULT '',
		text         TEXT NOT NULL DEFAULT '',
		offset_start INTEGER NOT NULL DEFAULT 0,
		offset_end   INTEGER NOT NULL DEFAULT 0,
		meta         JSONB NOT NULL DEFAULT '{}',
		embedding    vector(%d) NOT NULL
	)`, s.quotedTable, s.dim)
	if _, err := s.db.ExecContext(ctx, createTable); err != nil {
		// 42704 = undefined_object: the "vector" type does not exist,
		// meaning the extension is not installed in this database.
		if pqCode(err) == "42704" {
			return errors.New("embed: pgvector extension is not installed in this database — an admin must run: CREATE EXTENSION vector;")
		}
		return fmt.Errorf("embed: create pgvector table: %w", err)
	}
	// B-tree index on doc_id so RemoveDoc and ChunkIDsForDoc are index
	// scans, not full table scans. No ANN index by default: brute-force
	// cosine is correct and matches FlatStore's semantics, and ivfflat
	// needs data to train while hnsw may be absent on old pgvector. An
	// operator can add `CREATE INDEX ... USING hnsw (embedding
	// vector_cosine_ops)` for scale without changing this code.
	docIdx := "CREATE INDEX IF NOT EXISTS " + s.quotedDocIdx + " ON " + s.quotedTable + " (doc_id)"
	if _, err := s.db.ExecContext(ctx, docIdx); err != nil {
		return fmt.Errorf("embed: create doc_id index: %w", err)
	}
	return nil
}

// Add inserts (or replaces by id) chunks. Vectors are L2-normalized in place
// so cosine similarity reduces to a dot product, mirroring [FlatStore.Add].
// A vector whose length differs from the configured dimension is rejected.
func (s *PgVectorStore) Add(_ context.Context, chunks []Chunk) error {
	if len(chunks) == 0 {
		return nil
	}
	for i := range chunks {
		if len(chunks[i].Vec) != s.dim {
			return errVecDim(s.dim, len(chunks[i].Vec))
		}
		normalize(chunks[i].Vec)
	}
	// UPSERT by id so a direct caller (or a re-EnsureSchema) cannot produce
	// duplicate-primary-key errors. The index layer already replace-by-docs
	// via RemoveDoc first, so this is defense in depth.
	stmt := fmt.Sprintf(`INSERT INTO %s
		(id, doc_id, source, kind, text, offset_start, offset_end, meta, embedding)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::vector)
	ON CONFLICT (id) DO UPDATE SET
		doc_id       = EXCLUDED.doc_id,
		source       = EXCLUDED.source,
		kind         = EXCLUDED.kind,
		text         = EXCLUDED.text,
		offset_start = EXCLUDED.offset_start,
		offset_end   = EXCLUDED.offset_end,
		meta         = EXCLUDED.meta,
		embedding    = EXCLUDED.embedding`, s.quotedTable)
	for i := range chunks {
		c := &chunks[i]
		meta, err := marshalMeta(c.Metadata)
		if err != nil {
			return fmt.Errorf("embed: marshal meta for chunk %q: %w", c.ID, err)
		}
		kind, _ := c.Metadata["kind"].(string)
		if _, err := s.db.Exec(stmt,
			c.ID, c.DocID, c.Source, kind, c.Text,
			c.Offset[0], c.Offset[1], meta, encodeVector(c.Vec),
		); err != nil {
			return fmt.Errorf("embed: insert chunk %q: %w", c.ID, err)
		}
	}
	return nil
}

// RemoveDoc deletes every chunk belonging to docID.
func (s *PgVectorStore) RemoveDoc(ctx context.Context, docID string) error {
	stmt := fmt.Sprintf(`DELETE FROM %s WHERE doc_id = $1`, s.quotedTable)
	if _, err := s.db.ExecContext(ctx, stmt, docID); err != nil {
		return fmt.Errorf("embed: remove doc %q: %w", docID, err)
	}
	return nil
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
	if filter.Source != "" {
		args = append(args, filter.Source)
		fmt.Fprintf(&where, " AND source = $%d", len(args))
	}
	if filter.Kind != "" {
		args = append(args, filter.Kind)
		fmt.Fprintf(&where, " AND kind = $%d", len(args))
	}
	if len(filter.MetaMatch) > 0 {
		b, err := json.Marshal(filter.MetaMatch)
		if err != nil {
			return nil, fmt.Errorf("embed: marshal meta filter: %w", err)
		}
		args = append(args, b)
		fmt.Fprintf(&where, " AND meta @> $%d::jsonb", len(args))
	}
	args = append(args, top)
	limitIdx := len(args)

	// $1::vector is referenced twice (distance projection + ORDER BY);
	// PostgreSQL permits reusing a positional parameter within a statement.
	stmt := fmt.Sprintf(`SELECT id, doc_id, source, text, offset_start, offset_end, meta, embedding,
		embedding <=> $1::vector AS distance
	FROM %s
	WHERE %s
	ORDER BY embedding <=> $1::vector ASC, id ASC
	LIMIT $%d`, s.quotedTable, where.String(), limitIdx)

	rows, err := s.db.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, fmt.Errorf("embed: pgvector candidates: %w", err)
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
			&c.Offset[0], &c.Offset[1], &metaBytes, &embText, &distance,
		); err != nil {
			return nil, fmt.Errorf("embed: pgvector scan: %w", err)
		}
		if err := unmarshalMeta(metaBytes, &c.Metadata); err != nil {
			return nil, fmt.Errorf("embed: pgvector unmarshal meta: %w", err)
		}
		vec, err := parseVector(embText)
		if err != nil {
			return nil, fmt.Errorf("embed: pgvector parse embedding: %w", err)
		}
		c.Vec = vec
		hits = append(hits, Hit{Chunk: c, Score: 1 - distance, Reason: "vec"})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("embed: pgvector rows: %w", err)
	}
	return hits, nil
}

// Stats returns a snapshot of store state. Docs is the distinct doc_id count;
// Chunks is the row count. Dim/Model come from config. The counts are read
// with a background context + short timeout because the [Store] interface
// gives Stats no ctx — a failed/missing table yields zero counts rather than
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

// AllChunks returns every stored chunk ordered by id. Part of chunkLister;
// used to rebuild the keyword index after a snapshot load.
func (s *PgVectorStore) AllChunks() []Chunk {
	ctx, cancel := context.WithTimeout(context.Background(), statsTimeout)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, s.selectAll("ORDER BY id"))
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Chunk
	for rows.Next() {
		c, err := scanChunkRow(rows)
		if err != nil {
			return nil
		}
		out = append(out, c)
	}
	return out
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
	return fmt.Sprintf(`SELECT id, doc_id, source, text, offset_start, offset_end, meta, embedding FROM %s %s`, s.quotedTable, tail)
}

// scanChunkRow scans the canonical 8-column chunk projection. It closes
// nothing; the caller owns rows.
func scanChunkRow(rows *sql.Rows) (Chunk, error) {
	var (
		c         Chunk
		metaBytes []byte
		embText   string
	)
	if err := rows.Scan(
		&c.ID, &c.DocID, &c.Source, &c.Text,
		&c.Offset[0], &c.Offset[1], &metaBytes, &embText,
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
// empty so the @> containment operator behaves predictably.
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
	if err := json.Unmarshal(b, &out); err != nil {
		return err
	}
	*m = out
	return nil
}

// pqCode extracts the SQLSTATE code from a lib/pq error, or "" otherwise.
func pqCode(err error) string {
	var pe *pq.Error
	if errors.As(err, &pe) {
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
)
