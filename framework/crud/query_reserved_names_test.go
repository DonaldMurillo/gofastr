package crud

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// reservedNameRow is one key the list parser reserves. control is a
// query that uses the key for its reserved meaning; want reports
// whether the page answered that meaning, given the ids it listed. bug,
// when set, names the open finding that skips the control half.
type reservedNameRow struct {
	key     string
	control string
	want    func(t *testing.T, ids []string, body string)
	bug     string
}

func wantIDs(want ...string) func(*testing.T, []string, string) {
	return func(t *testing.T, ids []string, body string) {
		t.Helper()
		if !slices.Equal(ids, want) {
			t.Errorf("ids = %v, want %v; body=%s", ids, want, body)
		}
	}
}

// reservedNameRows covers every key filter.ListControlParams reserves;
// TestReservedNamesTableCoversTheParser holds it to that list, so a new
// reserved key joins by adding its row here.
var reservedNameRows = []reservedNameRow{
	{key: "cursor", control: "cursor=&limit=2", want: wantIDs("a", "b")},
	{key: "direction", control: "cursor=&direction=forward&limit=1", want: wantIDs("a")},
	{key: "fields", control: "fields=title&sort=id", want: func(t *testing.T, ids []string, body string) {
		t.Helper()
		wantIDs("a", "b", "c")(t, ids, body)
		if strings.Contains(body, `"page":"page-`) {
			t.Errorf("?fields=title projected a column it does not name: %s", body)
		}
	}},
	{key: "include", control: "include=&sort=id", want: wantIDs("a", "b", "c")},
	{key: "limit", control: "limit=1&sort=id", want: wantIDs("a")},
	{key: "offset", control: "offset=1&limit=1&sort=id", want: wantIDs("b")},
	{key: "page", control: "page=2&limit=1&sort=id", want: wantIDs("b")},
	{key: "per_page", control: "per_page=1&sort=id", want: wantIDs("a")},
	{key: "q", control: "q=orchard", want: wantIDs("b")},
	{key: "sort", control: "sort=-id", want: wantIDs("c", "b", "a")},
	{key: "stream", control: "stream=true&sort=id", want: wantIDs("a", "b", "c")},
	{key: "trashed", control: "trashed=false&sort=id", want: wantIDs("a", "b", "c")},
	{key: "where", control: `where={"field":"title","op":"eq","value":"meadow"}`, want: wantIDs("c")},
}

func TestReservedNamesTableCoversTheParser(t *testing.T) {
	var keys []string
	for _, row := range reservedNameRows {
		keys = append(keys, row.key)
	}
	slices.Sort(keys)
	if reserved := filter.ListControlParams(); !slices.Equal(keys, reserved) {
		t.Fatalf("reserved-name rows %v, want one per list control %v", keys, reserved)
	}
}

// reservedNamesHandler is an entity with a column named after every
// reserved list key. Row x holds "<key>-x" in each, so a bare control
// read as a column filter matches no row.
func reservedNamesHandler(t *testing.T) (*CrudHandler, *router.Router) {
	t.Helper()
	fields := []schema.Field{{Name: "title", Type: schema.String}}
	cols := []string{"id TEXT PRIMARY KEY", "title TEXT"}
	for _, row := range reservedNameRows {
		fields = append(fields, schema.Field{Name: row.key, Type: schema.String})
		cols = append(cols, `"`+row.key+`" TEXT`)
	}
	db := setupDB(t, "CREATE TABLE reserved ("+strings.Join(cols, ", ")+")")
	var seed []map[string]any
	for id, title := range map[string]string{"a": "garden", "b": "orchard", "c": "meadow"} {
		rec := map[string]any{"id": id, "title": title}
		for _, row := range reservedNameRows {
			rec[`"`+row.key+`"`] = row.key + "-" + id
		}
		seed = append(seed, rec)
	}
	seedRows(t, db, "reserved", seed)
	ent := entity.Define("reserved", entity.EntityConfig{
		Table:        "reserved",
		Fields:       fields,
		SearchFields: []string{"title"},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	r := router.New()
	RegisterCrudRoutes(r, ch, "/reserved")
	return ch, r
}

func reservedList(t *testing.T, ch *CrudHandler, query string) ([]string, string) {
	t.Helper()
	req := withTestUser(httptest.NewRequest(http.MethodGet, "/reserved?"+query, nil), "alice")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("GET ?%s: status %d, body=%s", query, rec.Code, body)
	}
	var ids []string
	var resp struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err == nil {
		for _, row := range resp.Data {
			ids = append(ids, row["id"].(string))
		}
		return ids, body
	}
	for line := range strings.SplitSeq(strings.TrimSpace(body), "\n") {
		var row map[string]any
		if json.Unmarshal([]byte(line), &row) == nil {
			if id, ok := row["id"].(string); ok {
				ids = append(ids, id)
			}
		}
	}
	return ids, body
}

// TestReservedNamesKeepBothMeanings: for a column named after a
// reserved list key, the bracket spelling (<key>[eq]=, query-dsl.md)
// filters the column and the bare key keeps its reserved meaning.
func TestReservedNamesKeepBothMeanings(t *testing.T) {
	installOwnerExtractor(t)
	ch, _ := reservedNamesHandler(t)
	for _, row := range reservedNameRows {
		t.Run(row.key+"/filter", func(t *testing.T) {
			filterKey := filter.BracketKey(row.key, filter.OpEq)
			ids, body := reservedList(t, ch, url.Values{filterKey: {row.key + "-b"}}.Encode())
			wantIDs("b")(t, ids, body)
		})
		t.Run(row.key+"/control", func(t *testing.T) {
			if row.bug != "" {
				t.Skip(row.bug)
			}
			ids, body := reservedList(t, ch, row.control)
			row.want(t, ids, body)
		})
	}
}

// mcpListIDs calls the list tool and returns the ids it listed.
func mcpListIDs(t *testing.T, srv *mcp.Server, ctx context.Context, args map[string]any) []string {
	t.Helper()
	out, err := srv.CallTool(ctx, "reserved_list", args)
	if err != nil {
		t.Fatalf("reserved_list %v: %v", args, err)
	}
	rows, _ := out.(map[string]any)["data"].([]any)
	var ids []string
	for _, r := range rows {
		ids = append(ids, r.(map[string]any)["id"].(string))
	}
	return ids
}

// TestReservedNamesMCPListTool: the filter on a column named after any
// reserved key takes its bracket form in the list tool's schema and
// works, and each of the tool's own control arguments keeps its name
// and type.
func TestReservedNamesMCPListTool(t *testing.T) {
	installOwnerExtractor(t)
	ch, r := reservedNamesHandler(t)
	srv := mcp.NewServer()
	if err := RegisterEntityMCPTools(srv, ch, r); err != nil {
		t.Fatalf("register mcp: %v", err)
	}
	ctx := handler.SetUser(context.Background(), &testUser{id: "alice"})
	props := listToolSchema(ch.Entity, nil)["properties"].(map[string]any)
	for _, row := range reservedNameRows {
		t.Run(row.key+"/filter", func(t *testing.T) {
			if ps, ok := props[row.key].(map[string]any); ok && ps["type"] == "string" && listToolControlArgs[row.key] == "" {
				t.Errorf("schema %q is a bare column filter, which the route reads as its control", row.key)
			}
			filterKey := filter.BracketKey(row.key, filter.OpEq)
			if ps, ok := props[filterKey].(map[string]any); !ok || ps["type"] != "string" {
				t.Errorf("schema %q = %v, want the string column filter", filterKey, props[filterKey])
			}
			if got := mcpListIDs(t, srv, ctx, map[string]any{filterKey: row.key + "-c"}); !slices.Equal(got, []string{"c"}) {
				t.Errorf("filter %s=%s listed %v, want [c]", filterKey, row.key+"-c", got)
			}
		})
		if _, takes := listToolControlArgs[row.key]; !takes {
			continue
		}
		t.Run(row.key+"/control", func(t *testing.T) {
			ps, _ := props[row.key].(map[string]any)
			if want := listToolControlArgs[row.key]; ps["type"] != want {
				t.Errorf("schema %q type = %v, want %s", row.key, ps["type"], want)
			}
			if row.bug != "" {
				t.Skip(row.bug)
			}
			args := map[string]any{}
			for k, v := range queryToolArgs(t, row.control) {
				args[k] = v
			}
			ids, body := mcpListIDs(t, srv, ctx, args), ""
			row.want(t, ids, body)
		})
	}
}

// listToolControlArgs is every control the list tool takes, with its
// schema type.
var listToolControlArgs = map[string]string{
	"limit": "integer", "offset": "integer", "page": "integer", "per_page": "integer",
	"q": "string", "sort": "string",
}

// queryToolArgs turns a row's control query into list-tool arguments.
func queryToolArgs(t *testing.T, query string) map[string]any {
	t.Helper()
	q, err := url.ParseQuery(query)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]any{}
	for k, vs := range q {
		if listToolControlArgs[k] == "integer" {
			n, err := strconv.Atoi(vs[0])
			if err != nil {
				t.Fatal(err)
			}
			out[k] = n
			continue
		}
		out[k] = vs[0]
	}
	return out
}
