package a

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"filter"
)

var db *sql.DB

// --- 145b37ab: ?q= reached the search LIKE binds ---------------------

func searchPreFix(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	conds := filter.SearchConditionsPreFix([]string{"title"}, q.Get("q")) // want `request text reaches a SQL bind through filter.SearchConditionsPreFix \(param term\)`
	_ = conds
}

// The list handler parses the query once and hands url.Values to a
// helper: the helper's summary carries the parameter to the caller.
func listPreFix(w http.ResponseWriter, r *http.Request) {
	searchWheres(r.URL.Query()) // want `request text reaches a SQL bind through a.searchWheres \(param q\)`
}

func searchWheres(q url.Values) []filter.Condition { // want searchWheres:`sink=1 `
	return filter.SearchConditionsPreFix([]string{"title"}, q.Get("q"))
}

func searchFixed(w http.ResponseWriter, r *http.Request) {
	conds, err := filter.SearchConditions([]string{"title"}, r.URL.Query().Get("q"))
	if err != nil {
		http.Error(w, "bad q", http.StatusBadRequest)
		return
	}
	_ = conds
}

// --- 39de7827: Idempotency-Key 0xff reached the store through an
// interface and answered 503 -------------------------------------------

type Store interface {
	Begin(ctx context.Context, key, fingerprint string) error
}

type sqlStore struct{ db *sql.DB }

func (s *sqlStore) Begin(ctx context.Context, key, fingerprint string) error { // want Begin:`sink=110 `
	_, err := s.db.ExecContext(ctx, "INSERT INTO idem (key, fp) VALUES ($1, $2)", columnKey(key), fingerprint)
	return err
}

type memStore struct{ m map[string]string }

func (s *memStore) Begin(ctx context.Context, key, fingerprint string) error {
	s.m[key] = fingerprint
	return nil
}

func columnKey(key string) string { return strings.ReplaceAll(key, `\`, `\\`) } // want columnKey:`return=1 `

func storeKey(principal, key string) string { // want storeKey:`return=11 `
	return strconv.Itoa(len(principal)) + ":" + principal + ":" + key
}

func idempotencyPreFix(store Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("Idempotency-Key")
		if key == "" || len(key) > 255 {
			return
		}
		_ = store.Begin(r.Context(), storeKey("u1", key), "fp") // want `request text reaches a SQL bind through sqlStore.Begin \(param key\)`
	})
}

func idempotencyFixed(store Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("Idempotency-Key")
		if key == "" || len(key) > 255 {
			return
		}
		if !printableASCII(key) {
			http.Error(w, "bad key", http.StatusBadRequest)
			return
		}
		_ = store.Begin(r.Context(), storeKey("u1", key), "fp")
	})
}

// printableASCII is the 39de7827 fix: an ASCII walk refuses every byte
// past 0x7e (so the text is valid UTF-8) and every control byte (NUL).
func printableASCII(s string) bool { // want printableASCII:`check=1 `
	for i := 0; i < len(s); i++ {
		if c := s[i]; c < 0x20 || c > 0x7e {
			return false
		}
	}
	return true
}

// --- direct binds -------------------------------------------------------

func direct(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("name")
	db.QueryRowContext(r.Context(), "SELECT id FROM users WHERE name = $1", name) // want `request text reaches a SQL bind`
	id := r.PathValue("id")
	db.ExecContext(r.Context(), "DELETE FROM t WHERE id = $1", "x-"+id) // want `request text reaches a SQL bind`
}

type builder struct{}

func (b *builder) Where(cond string, args ...any) *builder { return b }

func whereBuilder(r *http.Request) {
	var b builder
	b.Where("slug = ?", strings.ToLower(r.URL.Query().Get("slug"))) // want `request text reaches a SQL bind`
}

func conditionLiteral(r *http.Request) filter.Condition {
	return filter.Condition{SQL: "tag = ?", Args: []any{r.Header.Get("X-Tag")}} // want `request text reaches a SQL bind`
}

// --- postures that stay quiet ---------------------------------------------

func inlineCheck(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("name")
	if !utf8.ValidString(name) || strings.IndexByte(name, 0) >= 0 {
		http.Error(w, "bad name", http.StatusBadRequest)
		return
	}
	db.QueryRowContext(r.Context(), "SELECT id FROM users WHERE name = $1", name)
}

func enclosingCheck(r *http.Request) {
	name := r.FormValue("name")
	if utf8.ValidString(name) && !strings.ContainsRune(name, 0) {
		db.QueryRowContext(r.Context(), "SELECT id FROM users WHERE name = $1", name)
	}
}

func coercedKept(w http.ResponseWriter, r *http.Request) {
	v := r.URL.Query().Get("v")
	if _, err := filter.CoerceValue("v", v); err != nil {
		http.Error(w, "bad v", http.StatusBadRequest)
		return
	}
	db.QueryRowContext(r.Context(), "SELECT 1 FROM t WHERE v = $1", v)
}

var sorts = map[string]string{"new": "created_at", "old": "created_at DESC"}

func allowListed(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("dir")
	switch dir {
	case "asc", "desc":
		db.QueryRowContext(r.Context(), "SELECT 1 WHERE $1 IS NOT NULL", dir)
	}
	sort := r.URL.Query().Get("sort")
	if _, ok := sorts[sort]; !ok {
		return
	}
	db.QueryRowContext(r.Context(), "SELECT 1 WHERE $1 IS NOT NULL", sort)
	provider := r.PathValue("provider")
	col, ok := sorts[provider]
	if !ok {
		return
	}
	db.QueryRowContext(r.Context(), "SELECT 1 WHERE $1 = $2", provider, col)
	if mode := r.FormValue("mode"); mode == "fast" {
		db.QueryRowContext(r.Context(), "SELECT 1 WHERE $1 IS NOT NULL", mode)
	}
}

func parsed(r *http.Request) {
	n, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil {
		return
	}
	db.QueryRowContext(r.Context(), "SELECT 1 FROM t OFFSET $1", n)
	sum := sha256.Sum256([]byte(r.Header.Get("Authorization")))
	db.QueryRowContext(r.Context(), "SELECT 1 FROM tokens WHERE hash = $1", hex.EncodeToString(sum[:]))
}

// The SQL string is not a bind: a column name built from the request is
// another rule's business (and the allow-list there is the fix).
func sqlStringNotBind(r *http.Request) {
	col := r.URL.Query().Get("col")
	db.QueryRowContext(r.Context(), fmt.Sprintf("SELECT %s FROM t", col))
}

// --- near misses --------------------------------------------------------

// Half a check: valid UTF-8 can still hold a NUL.
func halfCheck(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("name")
	if !utf8.ValidString(name) {
		return
	}
	db.QueryRowContext(r.Context(), "SELECT id FROM users WHERE name = $1", name) // want `request text reaches a SQL bind`
}

// A check-shaped name is not a check: the body decides.
func nameOnly(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("name")
	if !filter.LooksValid(name) {
		return
	}
	db.QueryRowContext(r.Context(), "SELECT id FROM users WHERE name = $1", name) // want `request text reaches a SQL bind`
}

// A check whose verdict is thrown away vets nothing.
func discardedCheck(r *http.Request) {
	v := r.URL.Query().Get("v")
	filter.CoerceValue("v", v)
	db.QueryRowContext(r.Context(), "SELECT 1 FROM t WHERE v = $1", v) // want `request text reaches a SQL bind`
}

// A check made before the value was reassigned covers the old value only.
func checkThenReassign(r *http.Request) {
	v := r.URL.Query().Get("a")
	if !utf8.ValidString(v) || strings.IndexByte(v, 0) >= 0 {
		return
	}
	v = r.URL.Query().Get("b")
	db.QueryRowContext(r.Context(), "SELECT 1 FROM t WHERE v = $1", v) // want `request text reaches a SQL bind`
}

// A response's header and a value the program builds are not request text.
func notRequest(ctx context.Context, resp *http.Response, w http.ResponseWriter) {
	db.QueryRowContext(ctx, "SELECT 1 FROM t WHERE etag = $1", resp.Header.Get("ETag"))
	db.QueryRowContext(ctx, "SELECT 1 FROM t WHERE v = $1", w.Header().Get("X-Request-Id"))
	vals := url.Values{}
	vals.Set("k", "fixed")
	db.QueryRowContext(ctx, "SELECT 1 FROM t WHERE v = $1", vals.Get("k"))
}

// A marked site is the shape on purpose.
func marked(r *http.Request) {
	//gofastr:allow(textbind) the column is BYTEA; Postgres stores any bytes there
	db.QueryRowContext(r.Context(), "INSERT INTO blobs (b) VALUES ($1)", []byte(r.FormValue("b")))
}

// --- a reader that returns request text --------------------------------

// rawRecordID is crud's PathRecordID before the fix: its result is
// request text, so every caller binding it is a source site.
func rawRecordID(r *http.Request) string { // want rawRecordID:`source=true`
	return r.PathValue("id")
}

// recordID is the fix, its check declared below it: the summary must
// not keep a taint the check clears just because it was computed first.
func recordID(r *http.Request) string {
	id := r.PathValue("id")
	if !isStorable(id) {
		return ""
	}
	return id
}

func readRecord(r *http.Request) {
	db.QueryRowContext(r.Context(), "SELECT 1 FROM t WHERE id = $1", rawRecordID(r)) // want `request text reaches a SQL bind`
	db.QueryRowContext(r.Context(), "SELECT 1 FROM t WHERE id = $1", recordID(r))
}

func isStorable(s string) bool { // want isStorable:`check=1 `
	for i := 0; i < len(s); i++ {
		if s[i] == 0 {
			return false
		}
	}
	return utf8.ValidString(s)
}
