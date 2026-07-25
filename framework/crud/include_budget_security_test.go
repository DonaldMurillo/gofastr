package crud

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// budgetHandler builds a self-referencing entity (`up` BelongsTo parent,
// `down` HasMany children) over `rows` sibling rows that all point at a
// single root. That shape is the amplifier: every `up` hop collapses N
// parents onto one row, every `down` hop fans that row back out to N.
func budgetHandler(t *testing.T, rows int) *CrudHandler {
	t.Helper()
	ddl := `
CREATE TABLE bg_nodes (
	id     TEXT PRIMARY KEY,
	parent TEXT,
	label  TEXT
);
`
	cfg := makeEntityConfig("bg_nodes", "bg_nodes", "",
		[]schema.Field{
			{Name: "parent", Type: schema.String},
			{Name: "label", Type: schema.String},
		},
		func(c *entity.EntityConfig) {
			c.Relations = []entity.Relation{
				entity.BelongsTo("up", "bg_nodes", "parent"),
				entity.HasMany("down", "bg_nodes", "parent"),
			}
		},
	)
	ch, db := setupSecurityTestHandler(t, cfg, ddl)
	reg := newTestRegistry(t)
	reg.addByName(ch.Entity)
	ch.Registry = reg

	seed := []map[string]any{{"id": "root", "parent": "root", "label": "root"}}
	for i := 0; i < rows; i++ {
		seed = append(seed, map[string]any{
			"id": fmt.Sprintf("n%03d", i), "parent": "root", "label": "leaf",
		})
	}
	seedRows(t, db, "bg_nodes", seed)
	return ch
}

// TestIncludeDepthBudget pins that ?include= is bounded. Attack: a 23-byte
// `include=up.down.up.down.up.down` on a self-referencing entity expanded
// into a 13.7 MB response (~600,000×) and OOMed two levels deeper — no
// segment cap, no depth cap, no LIMIT on the eager SELECTs, and every
// parent got its own deep copy of the shared child subtree.
//
// The property is "the response an include can produce is bounded by the
// request, not exponential in it". Case shapes: over-deep, cyclic,
// wide fan-out — plus a legitimate two-level include that must still work.
func TestIncludeDepthBudget(t *testing.T) {
	ch := budgetHandler(t, 20)

	refused := []struct {
		name    string
		handler *CrudHandler
		include string
	}{
		{"over-deep", ch, "up.down.up.down.up"},
		{"cyclic", ch, strings.TrimSuffix(strings.Repeat("up.down.", 6), ".")},
		// Within the depth cap, but the fan-out at every level puts the
		// assembled response over the node budget. This is the case a depth
		// cap alone does not catch.
		{"over budget", budgetHandler(t, 60), "up.down.up.down"},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			req := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/bg_nodes?include=" + tc.include, UserID: "u1"})
			rr := httptest.NewRecorder()
			tc.handler.List()(rr, req)
			if rr.Code != http.StatusBadRequest {
				t.Errorf("SECURITY: [dos] include=%s returned %d with a %d-byte body — an unbounded include must be refused with 400",
					tc.include, rr.Code, rr.Body.Len())
			}
		})
	}

	t.Run("legitimate two-level", func(t *testing.T) {
		req := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/bg_nodes?include=up.down", UserID: "u1"})
		rr := httptest.NewRecorder()
		ch.List()(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("a legitimate two-level include was refused: %d (body=%s)", rr.Code, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), `"up"`) {
			t.Errorf("two-level include did not attach the relation. Body: %s", rr.Body.String())
		}
	})
}

// TestIncludeSharedSubtreeNotRecopied pins the multiplier itself: a
// permitted, at-the-cap include must stay linear in the data. Every one of
// the 20 rows resolves `up` to the SAME root row, so the shared subtree
// was deep-copied once per parent at each level. Bounded depth alone does
// not fix that — the copy has to be shared.
func TestIncludeSharedSubtreeNotRecopied(t *testing.T) {
	ch := budgetHandler(t, 20)

	req := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/bg_nodes?include=up.down.up.down", UserID: "u1"})
	rr := httptest.NewRecorder()
	ch.List()(rr, req)

	if rr.Code != http.StatusOK && rr.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status %d (body len %d)", rr.Code, rr.Body.Len())
	}
	// 21 rows of three short columns cannot legitimately exceed a few
	// hundred KB even fully expanded at the depth cap.
	const budget = 512 << 10
	if rr.Body.Len() > budget {
		t.Errorf("SECURITY: [dos] include=up.down.up.down over 21 rows produced %d bytes (budget %d) — shared subtrees are still copied per parent",
			rr.Body.Len(), budget)
	}
}

// The budget is threaded as a pointer so in-process callers that build
// IncludeNodes by hand can pass nil; a nil budget must be unmetered
// rather than a panic. Also covers the exhaustion boundary directly —
// the HTTP tests above reach it through a real query, which does not
// exercise the "spend more than remains in one call" path that a wide
// shared subtree takes.
func TestIncludeBudgetAccounting(t *testing.T) {
	var nilBudget *includeBudget
	if err := nilBudget.spend(1_000_000); err != nil {
		t.Errorf("a nil budget must be unmetered, got %v", err)
	}

	b := &includeBudget{remaining: 3}
	if err := b.spend(2); err != nil {
		t.Fatalf("spend within budget: %v", err)
	}
	if err := b.spend(1); err != nil {
		t.Fatalf("spend to exactly zero must succeed: %v", err)
	}
	if err := b.spend(1); !errors.Is(err, errIncludeBudget) {
		t.Errorf("spend past zero = %v, want errIncludeBudget", err)
	}

	// A single oversized charge — one shared subtree referenced again —
	// trips it just the same.
	big := &includeBudget{remaining: 3}
	if err := big.spend(9); !errors.Is(err, errIncludeBudget) {
		t.Errorf("oversized single charge = %v, want errIncludeBudget", err)
	}
}

// writeIncludeError renders a blown budget as the caller's problem and
// everything else as an opaque server fault. Getting that backwards
// either hides a real bug behind a 400 or tells a caller "internal
// error" for a request they can fix.
func TestIncludeErrorStatusSplit(t *testing.T) {
	rr := httptest.NewRecorder()
	writeIncludeError(rr, "list", errIncludeBudget)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("budget error = %d, want 400", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "narrow the include") {
		t.Errorf("budget 400 must say what to do about it: %s", rr.Body.String())
	}

	rr = httptest.NewRecorder()
	writeIncludeError(rr, "list", errors.New("some sql driver failure"))
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("server fault = %d, want 500", rr.Code)
	}
	if strings.Contains(rr.Body.String(), "sql driver") {
		t.Errorf("a server fault must not leak its detail to the caller: %s", rr.Body.String())
	}
}

// deepConvertMap's value-shape branches: a []any of maps (JSON arrays
// round-tripped through a driver), a scalar, and a second reference to
// an already-converted map (the memo hit that keeps a shared subtree
// linear).
func TestDeepConvertMapShapes(t *testing.T) {
	ch := NewCrudHandler(entity.Define("dc", entity.EntityConfig{
		Name: "dc", Table: "dc", Fields: []schema.Field{{Name: "x", Type: schema.String}},
	}), nil).WithJSONCase(CaseCamel)

	shared := map[string]any{"inner_key": "v"}
	in := map[string]any{
		"list_field":   []any{map[string]any{"nested_key": 1}, "scalar", nil},
		"slice_field":  []map[string]any{shared},
		"shared_again": shared,
	}
	memo := map[uintptr]*convertedSubtree{}
	out, n, err := ch.deepConvertMap(in, memo, newIncludeBudget())
	if err != nil {
		t.Fatalf("deepConvertMap: %v", err)
	}
	got := out.(map[string]any)
	if _, ok := got["listField"]; !ok {
		t.Errorf("[]any key not converted: %v", got)
	}
	arr := got["listField"].([]any)
	if first, ok := arr[0].(map[string]any); !ok || first["nestedKey"] == nil {
		t.Errorf("map inside []any not converted: %v", arr)
	}
	if arr[1] != "scalar" || arr[2] != nil {
		t.Errorf("non-map elements must pass through unchanged: %v", arr)
	}
	// The shared map is one object referenced twice — converted once,
	// counted twice.
	if got["sharedAgain"].(map[string]any)["innerKey"] != "v" {
		t.Errorf("memoised subtree lost its content: %v", got)
	}
	if n < 3 {
		t.Errorf("node count %d does not include the repeated reference", n)
	}
}
