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

// PgVectorStore is a durable Store over a Postgres table with a pgvector
// column.
type PgVectorStore struct {
	db           *sql.DB
	table        string
	quotedTable  string
	quotedDocIdx string
	quotedKwIdx  string
	dim          int
	model        string

	afterDelete func()
}

// PgVectorConfig configures a PgVectorStore.
type PgVectorConfig struct {
	// Table is the destination table. Defaults to "embed_chunks".
	Table string

	// Dim is the fixed embedding dimension. Required.
	Dim int

	// Model records the embedder model name in Stats. Optional.
	Model string
}

const statsTimeout = 3 * time.Second

// NewPgVector constructs a PgVectorStore; it errors on an unsafe table name or
// a non-positive Dim.
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
	datexport.RegisterEraser(datexport.DataEraser{
		Name: safe, Source: "semantic", Table: safe, Column: "owner",
		Mode: datexport.EraseDelete,
	})
	return &PgVectorStore{
		db:           db,
		table:        safe,
		quotedTable:  query.QuoteFolded(safe),
		quotedDocIdx: query.QuoteIndexName(docIndexName(safe)),
		quotedKwIdx:  query.QuoteIndexName(keywordIndexName(safe)),
		dim:          cfg.Dim,
		model:        cfg.Model,
	}, nil
}

func docIndexName(table string) string     { return indexName(table, "_doc_id_idx") }
func keywordIndexName(table string) string { return indexName(table, "_kw_fts_idx") }

func indexName(table, suffix string) string {
	if i := strings.LastIndexByte(table, '.'); i >= 0 {
		table = table[i+1:]
	}
	if len(table)+len(suffix) <= 63 {
		return table + suffix
	}
	if len(table) <= 61 {
		return (table + suffix)[:63]
	}
	h := fnv.New32a()
	h.Write([]byte(table))
	tag := fmt.Sprintf("_%08x%s", h.Sum32(), suffix)
	return table[:63-len(tag)] + tag
}

// EnsureSchema creates the vector extension, the table and its indexes if
// missing. Idempotent.
func (s *PgVectorStore) EnsureSchema(ctx context.Context) error {
	if err := query.RefuseCaseKeptTables(ctx, s.db, s.table); err != nil {
		return fmt.Errorf("semantic: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, "CREATE EXTENSION IF NOT EXISTS vector"); err != nil {
		if pqCode(err) != "42501" {
			return fmt.Errorf("semantic: create vector extension: %w. An admin may need to run: CREATE EXTENSION vector;", err)
		}
	}
	//gofastr:allow(GOFASTR1415) a chunk table is an embedding index derived from documents the host stores and exports itself, and a restore re-embeds them; NewPgVector registers erasure by Document.Owner
	createTable := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		id           TEXT PRIMARY KEY NOT NULL,
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
		if pqCode(err) == "42704" {
			return errors.New("semantic: pgvector extension is not installed in this database: an admin must run: CREATE EXTENSION vector;")
		}
		return fmt.Errorf("semantic: create pgvector table: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, fmt.Sprintf(
		`ALTER TABLE %s ADD COLUMN IF NOT EXISTS owner TEXT NOT NULL DEFAULT ''`, s.quotedTable)); err != nil {
		return fmt.Errorf("semantic: add owner column: %w", err)
	}
	docIdx := "CREATE INDEX IF NOT EXISTS " + s.quotedDocIdx + " ON " + s.quotedTable + " (doc_id)"
	if _, err := s.db.ExecContext(ctx, docIdx); err != nil {
		return fmt.Errorf("semantic: create doc_id index: %w", err)
	}
	kwIdx := "CREATE INDEX IF NOT EXISTS " + s.quotedKwIdx + " ON " + s.quotedTable +
		" USING GIN (to_tsvector('simple', text))"
	if _, err := s.db.ExecContext(ctx, kwIdx); err != nil {
		return fmt.Errorf("semantic: create keyword index: %w. Postgres 11+ ships GIN expression indexes; to_tsvector('simple', text) must match the search's predicate exactly", err)
	}
	return s.ensureModelTable(ctx)
}

// Add upserts chunks in one transaction, normalizing their vectors.
func (s *PgVectorStore) Add(ctx context.Context, chunks []Chunk) error {
	if len(chunks) == 0 {
		return nil
	}
	metas, err := s.prepareChunks(chunks)
	if err != nil {
		return err
	}
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

func (s *PgVectorStore) insertChunks(ctx context.Context, tx *sql.Tx, chunks []Chunk, metas [][]byte) error {
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

// RemoveDoc deletes every chunk of docID, under the per-document lock
// replaceDocs takes.
func (s *PgVectorStore) RemoveDoc(ctx context.Context, docID string) error {
	return s.replaceDocs(ctx, "", []string{docID}, nil)
}

func (s *PgVectorStore) replaceDocs(ctx context.Context, model string, docIDs []string, chunks []Chunk) error {
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
	if model != "" && len(chunks) > 0 {
		if err := s.checkModel(ctx, tx, model); err != nil {
			return err
		}
	}
	keys, err := s.docLockKeys(ctx, tx, docIDs)
	if err != nil {
		return err
	}
	for _, key := range keys {
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

func (s *PgVectorStore) docLockKeys(ctx context.Context, tx *sql.Tx, docIDs []string) ([]int64, error) {
	if len(docIDs) == 0 {
		return nil, nil
	}
	var oid string
	if err := tx.QueryRowContext(ctx, `SELECT $1::regclass::oid::text`, s.quotedTable).Scan(&oid); err != nil {
		return nil, fmt.Errorf("semantic: resolve table: %w", err)
	}
	keys := make([]int64, 0, len(docIDs))
	for _, id := range docIDs {
		h := fnv.New64a()
		var n [8]byte
		for _, part := range []string{oid, id} {
			binary.BigEndian.PutUint64(n[:], uint64(len(part)))
			h.Write(n[:])
			h.Write([]byte(part))
		}
		keys = append(keys, int64(h.Sum64())) //gofastr:allow(intwrap) a lock key is any 64-bit pattern; wrapping is the intent
	}
	slices.Sort(keys)
	return slices.Compact(keys), nil
}

// Candidates returns up to top chunks by cosine similarity to qv inside
// filter.
func (s *PgVectorStore) Candidates(ctx context.Context, qv []float32, filter Filter, top int) ([]Hit, error) {
	if len(qv) != s.dim {
		return nil, errVecDim(s.dim, len(qv))
	}
	top = defaultTop(top)
	q := make([]float32, len(qv))
	copy(q, qv)
	normalize(q)

	args := []any{encodeVector(q)}
	var where strings.Builder
	where.WriteString("TRUE")
	args, err := appendPgFilter(&where, args, filter)
	if err != nil {
		return nil, err
	}
	args = append(args, top)
	limitIdx := len(args)

	stmt := fmt.Sprintf(`SELECT %s,
		embedding <=> $1::vector AS distance
	FROM %s
	WHERE %s
	ORDER BY COALESCE(NULLIF(embedding <=> $1::vector, 'NaN'::float8), 1) ASC, id ASC
	LIMIT $%d`, pgChunkColumns, s.quotedTable, where.String(), limitIdx)

	rows, err := s.db.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, fmt.Errorf("semantic: pgvector candidates: %w", err)
	}
	defer rows.Close()
	var hits []Hit
	for rows.Next() {
		var distance float64
		c, err := scanChunkRow(rows, &distance)
		if err != nil {
			return nil, err
		}
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

// Stats returns the distinct document and row counts and the configured
// dimension and model.
func (s *PgVectorStore) Stats() Stats {
	ctx, cancel := context.WithTimeout(context.Background(), statsTimeout)
	defer cancel()
	var docs, chunks int
	_ = s.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT COUNT(DISTINCT doc_id), COUNT(*) FROM %s`, s.quotedTable),
	).Scan(&docs, &chunks)
	return Stats{Docs: docs, Chunks: chunks, Dim: s.dim, Model: s.model}
}

// ChunkIDsForDoc returns the IDs of every chunk of docID, or an error.
func (s *PgVectorStore) ChunkIDsForDoc(docID string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), statsTimeout)
	defer cancel()
	rows, err := s.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT id FROM %s WHERE doc_id = $1 ORDER BY id`, s.quotedTable),
		docID,
	)
	if err != nil {
		return nil, fmt.Errorf("semantic: list chunks for doc %q: %w", docID, err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("semantic: list chunks for doc %q: %w", docID, err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("semantic: list chunks for doc %q: %w", docID, err)
	}
	return ids, nil
}

// ChunkByID returns the chunk with the given ID.
func (s *PgVectorStore) ChunkByID(id string) (Chunk, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), statsTimeout)
	defer cancel()
	c, err := s.scanOne(ctx, `WHERE id = $1`, id)
	if err != nil {
		return Chunk{}, false
	}
	return c, true
}

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
		c, err := scanChunkRow(rows, nil)
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

func appendPgFilter(where *strings.Builder, args []any, filter Filter) ([]any, error) {
	if filter.Source != "" {
		args = append(args, filter.Source)
		fmt.Fprintf(where, " AND source = $%d", len(args))
	}
	if filter.Kind != "" {
		args = append(args, filter.Kind)
		fmt.Fprintf(where, " AND kind = $%d", len(args))
	}
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

// AllChunks returns every stored chunk ordered by id.
func (s *PgVectorStore) AllChunks() []Chunk {
	ctx, cancel := context.WithTimeout(context.Background(), statsTimeout)
	defer cancel()
	out, err := s.allChunks(ctx)
	if err != nil {
		return nil
	}
	return out
}

func (s *PgVectorStore) allChunks(ctx context.Context) ([]Chunk, error) {
	rows, err := s.db.QueryContext(ctx, s.selectAll("ORDER BY id"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Chunk
	for rows.Next() {
		c, err := scanChunkRow(rows, nil)
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
	c, err := scanChunkRow(rows, nil)
	if err != nil {
		return Chunk{}, err
	}
	return c, rows.Err()
}

const pgChunkColumns = "id, doc_id, source, text, offset_start, offset_end, meta, embedding, owner"

func (s *PgVectorStore) selectAll(tail string) string {
	return fmt.Sprintf("SELECT %s FROM %s %s", pgChunkColumns, s.quotedTable, tail)
}

// scanChunkRow reads the chunk projection. Candidate rows include a distance
// column and report the stage that failed when decoding a row.
func scanChunkRow(rows *sql.Rows, distance *float64) (Chunk, error) {
	var (
		c         Chunk
		metaBytes []byte
		embText   string
	)
	var dests [10]any
	columns := append(dests[:0],
		&c.ID, &c.DocID, &c.Source, &c.Text,
		&c.Offset[0], &c.Offset[1], &metaBytes, &embText, &c.Owner,
	)
	if distance != nil {
		columns = append(columns, distance)
	}
	rowError := func(stage string, err error) error {
		if distance != nil {
			return fmt.Errorf("semantic: pgvector %s: %w", stage, err)
		}
		return err
	}
	if err := rows.Scan(columns...); err != nil {
		return Chunk{}, rowError("scan", err)
	}
	if err := unmarshalMeta(metaBytes, &c.Metadata); err != nil {
		return Chunk{}, rowError("unmarshal meta", err)
	}
	vec, err := parseVector(embText)
	if err != nil {
		return Chunk{}, rowError("parse embedding", err)
	}
	c.Vec = vec
	return c, nil
}

func marshalMeta(m map[string]any) ([]byte, error) {
	if len(m) == 0 {
		return []byte("{}"), nil
	}
	return json.Marshal(m)
}

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

func pqCode(err error) string {
	if pe, ok := errors.AsType[*pq.Error](err); ok {
		return string(pe.Code)
	}
	return ""
}

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

var (
	_ Store       = (*PgVectorStore)(nil)
	_ chunkLister = (*PgVectorStore)(nil)
	_ docReplacer = (*PgVectorStore)(nil)
)
