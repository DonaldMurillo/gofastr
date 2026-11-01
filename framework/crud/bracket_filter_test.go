package crud

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// bracketTargetFields is a relation target with a colliding pair (stage
// beside stage_in), a lone opt_in, and a Hidden column.
func bracketTargetFields() []schema.Field {
	return []schema.Field{
		{Name: "id", Type: schema.Int},
		{Name: "stage", Type: schema.String},
		{Name: "stage_in", Type: schema.String},
		{Name: "opt_in", Type: schema.Bool},
		{Name: "secret", Type: schema.String, Hidden: true},
	}
}

// include=rel(...) reads its terms with the top-level rules: the bracket
// form names the whole field, every operator by word, a bare term that
// reads two ways is refused, and a lone opt_in is unchanged.
func TestScopedFilterBracketForm(t *testing.T) {
	fields := bracketTargetFields()
	for _, tc := range []struct {
		raw   string
		field string
		op    filter.FilterOp
	}{
		{"stage_in[eq]=x", "stage_in", filter.OpEq},
		{"stage[in]=a|b", "stage", filter.OpIn},
		{"stage[ne]=x", "stage", filter.OpNe},
		{"stage[like]=x", "stage", filter.OpLike},
		{"id[gt]=1", "id", filter.OpGt},
		{"id[gte]=1", "id", filter.OpGte},
		{"id[lt]=1", "id", filter.OpLt},
		{"id[lte]=1", "id", filter.OpLte},
		{"opt_in=true", "opt_in", filter.OpEq},
		{"stage_in_like=x", "stage_in", filter.OpLike},
	} {
		got, err := parseScopedFilters(tc.raw, fields, "members")
		if err != nil {
			t.Fatalf("%s: %v", tc.raw, err)
		}
		if len(got) == 0 || got[0].Field != tc.field || got[0].Op != tc.op {
			t.Fatalf("%s: %+v, want %s %s", tc.raw, got, tc.field, tc.op)
		}
	}
	_, err := parseScopedFilters("stage_in=x", fields, "members")
	if err == nil || !strings.Contains(err.Error(), `"stage_in[eq]"`) || !strings.Contains(err.Error(), `"stage[in]"`) {
		t.Fatalf("ambiguous scoped term: %v, want a refusal naming both bracket spellings", err)
	}
	if _, err := parseScopedFilters("stage[within]=x", fields, "members"); err == nil || !strings.Contains(err.Error(), "within") {
		t.Fatalf("unknown scoped bracket op: %v", err)
	}
	// One term repeated stays one IN list; status_in and status[in] on
	// one column are two terms, two ANDed IN lists.
	if fs, err := parseScopedFilters("stage[in]=a|b,stage[in]=c", fields, "members"); err != nil {
		t.Fatal(err)
	} else if c, _ := filterClause(fs, 1); strings.Count(c, " IN (") != 1 {
		t.Fatalf("one repeated term must stay one IN list: %q", c)
	}
	two := []schema.Field{{Name: "status", Type: schema.String}}
	fs, err := parseScopedFilters("status_in=a|b,status[in]=c", two, "members")
	if err != nil {
		t.Fatal(err)
	}
	if c, _ := filterClause(fs, 1); strings.Count(c, " IN (") != 2 {
		t.Fatalf("status_in and status[in] rendered %q, want two ANDed IN lists", c)
	}
}

// The same value run split at different places is a different predicate,
// so a second mention of the relation is not deduplicated away.
func TestScopedClauseDedupKeepsTermBoundaries(t *testing.T) {
	two := []schema.Field{{Name: "status", Type: schema.String}}
	a, _ := parseScopedFilters("status_in=x|y,status[in]=y", two, "r")
	b, _ := parseScopedFilters("status_in=x,status[in]=y|y", two, "r")
	var n IncludeNode
	if n.noteScopedClause(a) || n.noteScopedClause(b) {
		t.Fatal("two different scoped clauses were deduplicated as one")
	}
}

// A bracket on a Hidden field answers as one on an absent field, scoped
// and nested alike, whatever the operator word.
func TestBracketHiddenParityOneHopDown(t *testing.T) {
	fields := bracketTargetFields()
	users := mkEntity("users", "", fields)
	reg := versionedReg{m: map[string]*entity.Entity{"users|": users}}
	posts := mkRelEntity("posts", "", "users")
	for _, op := range []string{"eq", "like", "in", "foo"} {
		_, hErr := parseScopedFilters("secret["+op+"]=x", fields, "members")
		_, aErr := parseScopedFilters("nosuch["+op+"]=x", fields, "members")
		if hErr == nil || aErr == nil {
			t.Fatalf("scoped [%s]: hidden %v absent %v, want both refused", op, hErr, aErr)
		}
		if strings.ReplaceAll(hErr.Error(), "secret", "F") != strings.ReplaceAll(aErr.Error(), "nosuch", "F") {
			t.Fatalf("scoped hidden vs absent differ:\n %v\n %v", hErr, aErr)
		}
		_, hErr = parseNestedFiltersValues(url.Values{"author.secret[" + op + "]": {"x"}}, posts, reg)
		_, aErr = parseNestedFiltersValues(url.Values{"author.nosuch[" + op + "]": {"x"}}, posts, reg)
		if hErr == nil || aErr == nil {
			t.Fatalf("nested [%s]: hidden %v absent %v, want both refused", op, hErr, aErr)
		}
		if strings.ReplaceAll(hErr.Error(), "secret", "F") != strings.ReplaceAll(aErr.Error(), "nosuch", "F") {
			t.Fatalf("nested hidden vs absent differ:\n %v\n %v", hErr, aErr)
		}
	}
}

// ?rel.field= reads its last segment with the top-level rules.
func TestNestedFilterBracketForm(t *testing.T) {
	users := mkEntity("users", "", bracketTargetFields())
	reg := versionedReg{m: map[string]*entity.Entity{"users|": users}}
	posts := mkRelEntity("posts", "", "users")
	for _, tc := range []struct {
		key   string
		field string
		op    filter.FilterOp
	}{
		{"author.stage_in[eq]", "stage_in", filter.OpEq},
		{"author.stage[in]", "stage", filter.OpIn},
		{"author.id[gte]", "id", filter.OpGte},
		{"author.stage[like]", "stage", filter.OpLike},
		{"author.opt_in", "opt_in", filter.OpEq},
	} {
		v := "1"
		if tc.field == "opt_in" {
			v = "true"
		}
		got, err := parseNestedFiltersValues(url.Values{tc.key: {v}}, posts, reg)
		if err != nil {
			t.Fatalf("?%s: %v", tc.key, err)
		}
		if len(got) != 1 || got[0].Field != tc.field || got[0].Op != tc.op {
			t.Fatalf("?%s: %+v, want %s %s", tc.key, got, tc.field, tc.op)
		}
	}
	_, err := parseNestedFiltersValues(url.Values{"author.stage_in": {"x"}}, posts, reg)
	if err == nil || !strings.Contains(err.Error(), `"stage_in[eq]"`) || !strings.Contains(err.Error(), `"stage[in]"`) {
		t.Fatalf("ambiguous nested key: %v, want a refusal naming both bracket spellings", err)
	}
	if _, err := parseNestedFiltersValues(url.Values{"author.stage[within]": {"x"}}, posts, reg); err == nil || !strings.Contains(err.Error(), "within") {
		t.Fatalf("unknown nested bracket op: %v", err)
	}
}

// llm.md documents the bracket form and names each ambiguous bare key
// with its two bracket spellings; a non-colliding entity keeps its
// suffix-spelled examples.
func TestLLMMDDocumentsBracketKeys(t *testing.T) {
	ent := mkEntity("stages", "", []schema.Field{
		{Name: "stage", Type: schema.String},
		{Name: "stage_in", Type: schema.String},
	})
	md := EntityLLMMD(ent)
	for _, want := range []string{
		"`?<field>[op]=value`",
		"`?stage_in=` reads two ways and answers 400: send `?stage_in[eq]=`",
		"`?stage[in]=`",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("llm.md missing %q", want)
		}
	}
	plain := EntityLLMMD(mkEntity("plain", "", []schema.Field{{Name: "status", Type: schema.String}}))
	if strings.Contains(plain, "reads two ways") || !strings.Contains(plain, "`id_in=a,b,c`") {
		t.Errorf("a non-colliding entity's llm.md changed its examples:\n%s", plain)
	}
}

// The MCP list tool names each filter param by the key it forwards: the
// bracket spelling exactly where the bare key reads two ways, the suffix
// spelling everywhere else, so the schema never offers a key the route
// refuses and both colliding columns stay filterable.
func TestMCPListToolSpellsCollidingKeys(t *testing.T) {
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE mcp_stages (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, stage TEXT, stage_in TEXT)`
	cfg := makeEntityConfig("mcp_stages", "mcp_stages", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "stage", Type: schema.String},
		{Name: "stage_in", Type: schema.String},
	})
	ch, _ := setupSecurityTestHandler(t, cfg, ddl)
	props, _ := listToolSchema(ch.Entity)["properties"].(map[string]any)
	for _, k := range []string{"stage", "stage[in]", "stage_in[eq]", "stage_ne", "stage_in_ne", "stage_in_in", "user_id", "user_id_in"} {
		if _, ok := props[k]; !ok {
			t.Errorf("list tool schema has no %q param", k)
		}
	}
	if _, ok := props["stage_in"]; ok {
		t.Error("list tool schema offers the ambiguous bare key stage_in")
	}

	var gotQuery url.Values
	rec := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[],"total":0}`))
	})
	_, _ = ch.listTool(rec)(context.Background(), map[string]any{
		"stage_in[eq]": "noon",
		"stage[in]":    []any{"a", "b"},
		"user_id_in":   []any{"u"},
	})
	if gotQuery.Get("stage_in[eq]") != "noon" || len(gotQuery["stage[in]"]) != 2 || gotQuery.Get("user_id_in") != "u" {
		t.Fatalf("forwarded %v", gotQuery)
	}
	if gotQuery.Has("stage_in") {
		t.Fatalf("forwarded the ambiguous bare key: %v", gotQuery)
	}
}

// ?rel.field= repeated with a scalar operator is the top-level parser's
// filter.repeated_filter, the bracket form included: keeping values[0]
// ran ?author.id_lt=100&author.id_lt=5 as id < 100 while a proxy
// reading the last occurrence saw < 5. A repeated IN key still reads
// every occurrence.
func TestNestedFilterRepeatedScalarKeyRefused(t *testing.T) {
	users := mkEntity("users", "", bracketTargetFields())
	reg := versionedReg{m: map[string]*entity.Entity{"users|": users}}
	posts := mkRelEntity("posts", "", "users")
	for _, key := range []string{"author.id_lt", "author.id[lt]", "author.stage", "author.stage[eq]", "author.stage_in[eq]", "author.opt_in"} {
		v1, v2 := "100", "5"
		if key == "author.opt_in" {
			v1, v2 = "true", "false"
		}
		got, err := parseNestedFiltersValues(url.Values{key: {v1, v2}}, posts, reg)
		if err == nil {
			t.Fatalf("?%s twice: accepted as %+v; a repeated scalar key must be refused", key, got)
		}
		var ie *i18n.Error
		if !errors.As(err, &ie) || ie.Reason.Key() != "filter.repeated_filter" {
			t.Fatalf("?%s twice: %v, want filter.repeated_filter", key, err)
		}
		if !strings.Contains(err.Error(), strconv.Quote(key)) || !strings.Contains(err.Error(), "2 times") {
			t.Fatalf("?%s twice: %q does not name the key and its count", key, err)
		}
	}
	got, err := parseNestedFiltersValues(url.Values{"author.stage[in]": {"a", "b,c"}, "author.id_gt": {"1"}}, posts, reg)
	if err != nil {
		t.Fatalf("repeated IN key: %v", err)
	}
	for _, nf := range got {
		if nf.Op == filter.OpIn && len(nf.Values) != 3 {
			t.Fatalf("repeated IN key kept %v, want every occurrence", nf.Values)
		}
	}
}
