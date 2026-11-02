package crud

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

type wireRig struct {
	t   *testing.T
	r   *router.Router
	srv *mcp.Server
	ch  *CrudHandler
	ctx context.Context
}

func newWireRig(t *testing.T) *wireRig {
	t.Helper()
	installSecurityOwnerExtractor(t)
	db := setupDB(t, `CREATE TABLE wire (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, amount INTEGER, price TEXT, enabled BOOLEAN, meta TEXT, name TEXT)`)
	ent := entity.Define("wire", makeEntityConfig("wire", "wire", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String},
		{Name: "amount", Type: schema.Int},
		{Name: "price", Type: schema.Decimal},
		{Name: "enabled", Type: schema.Bool},
		{Name: "meta", Type: schema.JSON},
		{Name: "name", Type: schema.String},
	}))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	r := router.New()
	RegisterCrudRoutes(r, ch, "/wire")
	srv := mcp.NewServer()
	if err := RegisterEntityMCPTools(srv, ch, r); err != nil {
		t.Fatal(err)
	}
	orig, _ := http.NewRequest(http.MethodPost, "/mcp", nil)
	ctx := handler.SetUser(mcp.WithRequest(context.Background(), orig), &testUser{id: "alice"})
	return &wireRig{t: t, r: r, srv: srv, ch: ch, ctx: ctx}
}

func (w *wireRig) post(body string) (int, string) {
	return w.postTo("/wire", body)
}

func (w *wireRig) postTo(path, body string) (int, string) {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)).
		WithContext(handler.SetUser(context.Background(), &testUser{id: "alice"}))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	w.r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func (w *wireRig) create(args string) (string, *mcp.RPCError) {
	p, err := json.Marshal(map[string]any{"name": "wire_create", "arguments": json.RawMessage(args)})
	if err != nil {
		w.t.Fatal(err)
	}
	resp := w.srv.HandleRequest(w.ctx, mcp.Request{JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: p})
	out, err := json.Marshal(resp.Result)
	if err != nil {
		w.t.Fatal(err)
	}
	return string(out), resp.Error
}

// An integer spelled with an exponent past 2^53 is refused over MCP as
// over HTTP. The argument decode rounded it to a float64, the redispatch
// wrote that back as an integer literal, and 9007199254740992 was stored.
func TestMCPRefusesRoundedExponentInt(t *testing.T) {
	w := newWireRig(t)
	if code, _ := w.post(`{"amount":9007199254740993e0}`); code != http.StatusBadRequest {
		t.Fatalf("HTTP control: status %d, want 400", code)
	}
	out, rpcErr := w.create(`{"amount":9007199254740993e0}`)
	if rpcErr == nil {
		t.Fatalf("MCP stored a rounded amount: %s", out)
	}
}

// A Decimal travels as text over MCP, the spelling the route accepts.
func TestMCPCreateTakesDecimalText(t *testing.T) {
	w := newWireRig(t)
	out, rpcErr := w.create(`{"price":"12.34"}`)
	if rpcErr != nil {
		t.Fatalf("create with price \"12.34\": %+v", rpcErr)
	}
	if !strings.Contains(out, `\"price\":\"12.34\"`) {
		t.Errorf("result lost the price: %s", out)
	}
}

// An integer inside a JSON field reads back exactly.
func TestJSONFieldKeepsBigInt(t *testing.T) {
	w := newWireRig(t)
	code, body := w.post(`{"meta":{"n":9007199254740993}}`)
	if code != http.StatusCreated {
		t.Fatalf("status %d: %s", code, body)
	}
	if !strings.Contains(body, `"n":9007199254740993`) {
		t.Errorf("create response rounded the stored integer: %s", body)
	}
}

// A body holding a second JSON value after the object is refused.
func TestBodyRefusesTrailingJSON(t *testing.T) {
	w := newWireRig(t)
	if code, body := w.post(`{"name":"first"}{"name":"second"}`); code != http.StatusBadRequest {
		t.Errorf("status %d, want 400: %s", code, body)
	}
	if code, body := w.post("{\"name\":\"first\"}\n"); code != http.StatusCreated {
		t.Errorf("trailing whitespace refused: status %d: %s", code, body)
	}
}

// A Bool field takes the integer 1 as it takes 1.0.
func TestBoolTakesIntegerOne(t *testing.T) {
	w := newWireRig(t)
	code, body := w.post(`{"enabled":1}`)
	if code != http.StatusCreated || !strings.Contains(body, `"enabled":true`) {
		t.Errorf("status %d: %s", code, body)
	}
	if code, _ := w.post(`{"enabled":2}`); code != http.StatusBadRequest {
		t.Errorf("enabled 2: status %d, want 400", code)
	}
}

// Every spelling a Bool field accepts is stored as the boolean, so a
// boolean filter finds it. SQLite kept "true" and "1" as text, which
// ?enabled=true (bound as 1) never matched.
func TestBoolSpellingsFilterAlike(t *testing.T) {
	w := newWireRig(t)
	for i, v := range []string{`true`, `"true"`, `"TRUE"`, `"1"`, `1`, `1.0`} {
		if code, body := w.post(fmt.Sprintf(`{"name":"t%d","enabled":%s}`, i, v)); code != http.StatusCreated {
			t.Fatalf("enabled %s: status %d: %s", v, code, body)
		}
	}
	for i, v := range []string{`false`, `"false"`, `"0"`, `0`} {
		if code, body := w.post(fmt.Sprintf(`{"name":"f%d","enabled":%s}`, i, v)); code != http.StatusCreated {
			t.Fatalf("enabled %s: status %d: %s", v, code, body)
		}
	}
	for want, n := range map[string]int{"true": 6, "false": 4} {
		req := httptest.NewRequest(http.MethodGet, "/wire?limit=50&enabled="+want, nil).
			WithContext(handler.SetUser(context.Background(), &testUser{id: "alice"}))
		rec := httptest.NewRecorder()
		w.r.ServeHTTP(rec, req)
		if got := strings.Count(rec.Body.String(), `"name":"`+want[:1]); got != n || rec.Code != http.StatusOK {
			t.Errorf("?enabled=%s: %d rows (status %d), want %d:\n%s", want, got, rec.Code, n, rec.Body.String())
		}
	}
}

// A JSON field keeps an integer a float64 would round: past int64 on
// the way in, and past 2^53 spelled with an exponent on either side,
// sent as an object or as JSON text.
func TestJSONFieldKeepsHugeInts(t *testing.T) {
	w := newWireRig(t)
	for body, want := range map[string]string{
		`{"meta":{"n":9223372036854775809}}`:        `"n":9223372036854775809`,
		`{"meta":{"n":9007199254740993e0}}`:         `"n":9007199254740993e0`,
		`{"meta":"{\"n\":9007199254740993e0}"}`:     `"n":9007199254740993e0`,
		`{"meta":{"n":1.5,"m":9007199254740992.0}}`: `"n":1.5`,
		`{"meta":9223372036854775809}`:              `"meta":9223372036854775809`,
		// Below 2^53 an exponent spelling is the integer it names.
		`{"amount":5e0}`:     `"amount":5`,
		`{"meta":{"n":5e0}}`: `"n":5}`,
	} {
		code, got := w.post(body)
		if code != http.StatusCreated || !strings.Contains(got, want) {
			t.Errorf("%s: status %d, want %s in %s", body, code, want, got)
		}
	}
	// Outside a JSON field such a number is still not text.
	if code, got := w.post(`{"name":9007199254740993e0}`); code != http.StatusBadRequest {
		t.Errorf("name 9007199254740993e0: status %d, want 400: %s", code, got)
	}
}

// A typed read keeps a JSON field's integers: json.Unmarshal into an
// `any` read 9007199254740993 as 9007199254740992, and writing the row
// back stored the rounded value.
func TestTypedReadKeepsHugeInts(t *testing.T) {
	w := newWireRig(t)
	const doc = `{"m":9007199254740993,"n":9223372036854775809,"s":1}`
	if code, got := w.post(`{"meta":` + doc + `}`); code != http.StatusCreated {
		t.Fatalf("create: %d %s", code, got)
	}
	type wireRow struct {
		Meta any `json:"meta,omitempty"`
	}
	row, err := NewTypedQuery[wireRow](w.ch).First(handler.SetUser(context.Background(), &testUser{id: "alice"}))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := json.Marshal(row.Meta); string(got) != doc {
		t.Errorf("typed read meta = %s, want %s", got, doc)
	}
	meta, _ := row.Meta.(map[string]any)
	if _, ok := meta["s"].(float64); !ok {
		t.Errorf("small integer = %T, want float64 as before", meta["s"])
	}
}

// A json.Number past float64 range, which only code can put in a body,
// is refused rather than stored as infinity.
func TestNumberPastFloatRefused(t *testing.T) {
	w := newWireRig(t)
	if err := w.ch.coerceNumberColumnValues(map[string]any{"price": json.Number("1e999")}); err == nil {
		t.Fatal("price 1e999 accepted")
	}
}

// A batch item reads its numbers the way a single create does: 1 is a
// boolean, and a number is not a string.
func TestBatchNumbersMatchCreate(t *testing.T) {
	w := newWireRig(t)
	if code, body := w.postTo("/wire/_batch", `{"items":[{"enabled":1}]}`); code != http.StatusOK || !strings.Contains(body, `"enabled":true`) {
		t.Errorf("batch enabled 1: status %d: %s", code, body)
	}
	if code, _ := w.post(`{"name":5}`); code != http.StatusBadRequest {
		t.Fatalf("create control: name 5 status %d, want 400", code)
	}
	if code, body := w.postTo("/wire/_batch", `{"items":[{"name":5}]}`); code != http.StatusBadRequest {
		t.Errorf("batch name 5: status %d, want 400: %s", code, body)
	}
	// A Before-hook sees the same Go types either way.
	var seen []string
	w.ch.Hooks = hook.NewHookRegistry()
	w.ch.Hooks.RegisterHook(hook.BeforeCreate, func(_ context.Context, data any) error {
		m, _ := data.(map[string]any)
		// Only the two probes below carry meta; a hook that panicked on
		// the others would fail them with 500 before validation runs.
		if meta, ok := m["meta"].(map[string]any); ok {
			seen = append(seen, fmt.Sprintf("%T %T", m["amount"], meta["n"]))
		}
		return nil
	})
	w.post(`{"amount":7,"meta":{"n":1.5}}`)
	w.postTo("/wire/_batch", `{"items":[{"amount":7,"meta":{"n":1.5}}]}`)
	if len(seen) != 2 || seen[0] != seen[1] {
		t.Errorf("hook types create vs batch = %q, want the same", seen)
	}
	if code, _ := w.post(`{"price":1e999}`); code != http.StatusBadRequest {
		t.Fatalf("create control: price 1e999 status %d, want 400", code)
	}
	if code, body := w.postTo("/wire/_batch", `{"items":[{"price":1e999}]}`); code != http.StatusBadRequest {
		t.Errorf("batch price 1e999: status %d, want 400: %s", code, body)
	}
}
