package crud

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// The property under test is one sentence: no query surface accepts a NoQuery
// column. The surfaces are the axis, so this file is one setup and a subtest
// per surface rather than a file per case.

// setupNoQueryRelated builds a parent entity with a relation to a child whose
// `secret` column is NoQuery, so the relation-crossing surfaces (nested
// filters, scoped include filters) can be probed as well as the flat ones.
func setupNoQueryRelated(t *testing.T) (*CrudHandler, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec(`
		CREATE TABLE nq_authors (id TEXT PRIMARY KEY, name TEXT, secret TEXT);
		CREATE TABLE nq_posts (id TEXT PRIMARY KEY, title TEXT, author_id TEXT);
		INSERT INTO nq_authors (id, name, secret) VALUES ('a1','alice','SECRET-042');
		INSERT INTO nq_posts (id, title, author_id) VALUES ('p1','hello','a1');
	`); err != nil {
		t.Fatal(err)
	}

	authors := entity.Define("nq_authors", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "name", Type: schema.String},
			{Name: "secret", Type: schema.String, NoQuery: true},
		},
	}.WithTimestamps(false))
	authors.SetDB(db)

	posts := entity.Define("nq_posts", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
			{Name: "author_id", Type: schema.String},
		},
		Relations: []entity.Relation{entity.BelongsTo("author", "nq_authors", "author_id")},
	}.WithTimestamps(false))
	posts.SetDB(db)

	reg := stubRegistry{byName: map[string]*entity.Entity{
		"nq_authors": authors, "nq_posts": posts,
	}}

	ch := NewCrudHandler(posts, db).WithJSONCase(CaseSnake)
	ch.Registry = reg
	return ch, db
}

func statusFor(t *testing.T, ch *CrudHandler, path string) (int, string) {
	t.Helper()
	req := withTestUser(httptest.NewRequest(http.MethodGet, path, nil), "u1")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	return rec.Code, rec.Body.String()
}

// TestNoQueryAcrossRelationSurfaces covers the relation-crossing paths: a
// NoQuery column one hop away is the same oracle, and both the nested-filter
// and scoped-include parsers have their own allow-lists to get wrong.
func TestNoQueryAcrossRelationSurfaces(t *testing.T) {
	ch, _ := setupNoQueryRelated(t)

	cases := []struct{ name, path string }{
		{"nested_filter_eq", "/nq_posts?author.secret=SECRET-042"},
		{"nested_filter_like", "/nq_posts?author.secret_like=SECRET-0"},
		{"scoped_include_eq", "/nq_posts?include=author(secret=SECRET-042)"},
		{"scoped_include_like", "/nq_posts?include=author(secret_like=SECRET-0)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, body := statusFor(t, ch, c.path)
			if code != http.StatusBadRequest {
				t.Errorf("SECURITY: %s = %d, want 400. A NoQuery column reached the query "+
					"one relation hop away, so row presence still leaks its value.\nbody=%s",
					c.path, code, body)
			}
			if !strings.Contains(body, "secret") {
				t.Errorf("rejection should name the field (it is visible in responses): %s", body)
			}
		})
	}
}

// TestNoQueryStillReadableThroughInclude pins the other half — the guard
// blocks filtering on the column, not reading it. If this ever starts
// failing, NoQuery has silently become Hidden.
func TestNoQueryStillReadableThroughInclude(t *testing.T) {
	ch, _ := setupNoQueryRelated(t)

	code, body := statusFor(t, ch, "/nq_posts?include=author")
	if code != http.StatusOK {
		t.Fatalf("include = %d, want 200; body=%s", code, body)
	}
	if !strings.Contains(body, "secret") {
		t.Errorf("NoQuery field vanished from the include payload — it must stay readable, "+
			"only unqueryable. Use Hidden to remove it from responses.\nbody=%s", body)
	}
}

// TestNoQueryAbsentFromAgentSurfaces covers the three generated descriptions
// an agent or SDK reads to decide what it may filter on. Advertising a filter
// the parser rejects costs a wasted call and a confusing 400.
func TestNoQueryAbsentFromAgentSurfaces(t *testing.T) {
	ch, _ := setupNoQueryRelated(t)
	authors, err := ch.Registry.Get("nq_authors")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("mcp_list_tool_schema", func(t *testing.T) {
		props, _ := listToolSchema(authors)["properties"].(map[string]any)
		if _, ok := props["secret"]; ok {
			t.Error("MCP list tool advertises a filter arg for a NoQuery field")
		}
		if _, ok := props["name"]; !ok {
			t.Error("MCP list tool dropped a normal filterable field")
		}
	})

	t.Run("llm_md_filter_section", func(t *testing.T) {
		md := EntityLLMMD(authors)
		if !strings.Contains(md, "not filterable/sortable") {
			t.Error("llm.md field table does not flag the NoQuery column")
		}
		for line := range strings.SplitSeq(md, "\n") {
			if strings.Contains(line, "secret_like") {
				t.Errorf("llm.md advertises a filter operator example on a NoQuery field: %q", line)
			}
		}
	})
}

// TestNoQueryCursorFieldRejectedAtDefine pins the keyset half. Cursor columns
// land in ORDER BY and in the emitted token, which is reversible base64 JSON
// — so a NoQuery cursor field hands back exactly what the flag withholds.
func TestNoQueryCursorFieldRejectedAtDefine(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("Define accepted a NoQuery cursor field")
		}
		if msg, ok := r.(string); !ok || !strings.Contains(msg, "cursor field") {
			t.Fatalf("panic = %v, want it to name the cursor field", r)
		}
	}()
	entity.Define("nq_cursor_bad", entity.EntityConfig{Fields: []schema.Field{
		{Name: "body", Type: schema.String, NoQuery: true},
	}, Pagination: &entity.PaginationConfig{

		// TestNoQuerySearchFieldRejectedAtDefine covers the Go-API half of the ?q=
		// guard; the blueprint half is pinned in cmd/gofastr.
		CursorField: "body"},
	})
}

func TestNoQuerySearchFieldRejectedAtDefine(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("Define accepted a NoQuery SearchFields entry")
		}
		if msg, ok := r.(string); !ok || !strings.Contains(msg, "NoQuery") {
			t.Fatalf("panic = %v, want it to explain the column is NoQuery", r)
		}
	}()
	entity.Define("nq_search_bad", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "body", Type: schema.String, NoQuery: true},
		},
		SearchFields: []string{"body"},
	})
}

// TestNoQuerySurvivesDeclarationRoundTrip guards the silent-drop class: the
// flag is carried by hand through several serializers, and one that forgets
// it produces a declaration promising a protection the app does not enforce.
func TestNoQuerySurvivesDeclarationRoundTrip(t *testing.T) {
	orig := entity.EntityDeclaration{
		Name: "cards",
		Fields: []entity.FieldDeclaration{
			{Name: "number", Type: "string", NoQuery: true},
			{Name: "label", Type: "string"},
		},
	}
	blob, err := json.Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(blob), "no_query") {
		t.Fatalf("NoQuery has no JSON representation: %s", blob)
	}
	var back entity.EntityDeclaration
	if err := json.Unmarshal(blob, &back); err != nil {
		t.Fatal(err)
	}
	cfg, err := back.Config()
	if err != nil {
		t.Fatal(err)
	}
	// Assert the field SURVIVES, not merely that it kept its flag if present.
	// A loop that only fires "if f.Name == number" passes when the conversion
	// drops the column entirely — losing the flag and the column together,
	// which is strictly worse than losing the flag.
	var found *schema.Field
	for i := range cfg.Fields {
		if cfg.Fields[i].Name == "number" {
			found = &cfg.Fields[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("the NoQuery field did not survive the round trip at all: %#v", cfg.Fields)
	}
	if !found.NoQuery {
		t.Error("NoQuery lost converting a declaration to an EntityConfig")
	}
}

// TestNoQueryPrimaryKeyRejectedAsDefaultCursor covers the case the first
// version of the Define check walked straight past: it validated only
// explicitly declared cursor columns, but keyset paging falls back to the
// primary key when none is configured. A NoQuery primary key was therefore
// used for ORDER BY and keyset comparison exactly as if it had been named,
// and its stored value was base64'd into the emitted cursor token — in the
// same response whose row showed the masked form. The DSL's after() guard
// resolved the same default and did check it, so the two guards disagreed.
func TestNoQueryPrimaryKeyRejectedAsDefaultCursor(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("Define accepted a NoQuery primary key with no explicit cursor field; " +
				"?cursor= would page on it and leak the stored value in the token")
		}
		if msg, ok := r.(string); !ok || !strings.Contains(msg, "cursor field") {
			t.Fatalf("panic = %v, want it to name the cursor field", r)
		}
	}()
	entity.Define("nq_pk_cursor", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "id", Type: schema.String, NoQuery: true},
			{Name: "body", Type: schema.String},
		},
	})
}

// TestPlainEntityDefinesWithoutCursorPanic is the false-positive guard for the
// above: the default-PK check must not reject ordinary entities, which never
// declare a cursor field and whose id is a normal column.
func TestPlainEntityDefinesWithoutCursorPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Define panicked on an ordinary entity: %v", r)
		}
	}()
	entity.Define("nq_plain_ok", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
			{Name: "masked", Type: schema.String, NoQuery: true},
		},
	})
}

// TestNoQueryPKRejectedAsCompositeTiebreak covers the second hole in the same
// check. cursorFields() appends the primary key to any composite that omits
// it, so CursorFields:["created_at"] silently pages on ("created_at","id")
// too. Validating only the declared members let a NoQuery id through, and
// buildCursorPage encodes every keyset value into the token — so the masked
// column came back in reversible base64 alongside the row that hid it.
func TestNoQueryPKRejectedAsCompositeTiebreak(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("Define accepted a NoQuery primary key as a composite cursor tiebreak")
		}
		if msg, ok := r.(string); !ok || !strings.Contains(msg, "cursor field") {
			t.Fatalf("panic = %v, want it to name the cursor field", r)
		}
	}()
	entity.Define("nq_composite_pk", entity.EntityConfig{Fields: []schema.Field{
		{Name: "id", Type: schema.String, NoQuery: true},
		{Name: "created_at", Type: schema.Timestamp},
	}, Pagination: &entity.PaginationConfig{CursorFields: []string{"created_at"}},
	})
}

// TestSingleCursorFieldDoesNotPullInPK is the false-positive guard: the
// single-field CursorField branch does NOT append the primary key, so a
// NoQuery id must not make an otherwise-valid entity panic.
func TestSingleCursorFieldDoesNotPullInPK(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Define panicked on a valid single-field cursor: %v", r)
		}
	}()
	entity.Define("nq_single_cursor_ok", entity.EntityConfig{Fields: []schema.Field{
		{Name: "id", Type: schema.String, NoQuery: true},
		{Name: "seq", Type: schema.Int},
	}, Pagination: &entity.PaginationConfig{CursorField: "seq"},
	})
}
