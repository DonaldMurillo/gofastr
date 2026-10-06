package crud

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// The states write-path fixture: invoices whose status Enum moves
// draft → open → paid, with paid_on stamped by the pay move and a System
// move only Go code may run. Every refusal asserts the stored row too: a
// refusal that still wrote is a failed test.
const statesInvoiceDDL = `CREATE TABLE invoices (
	id TEXT PRIMARY KEY,
	number TEXT NOT NULL,
	status TEXT DEFAULT 'draft',
	amount INTEGER,
	paid_on TEXT,
	created_at TEXT,
	updated_at TEXT
)`

func statesInvoiceFields() []schema.Field {
	return []schema.Field{
		{Name: "number", Type: schema.String, Required: true},
		{Name: "status", Type: schema.Enum, Default: "draft", Values: []string{"draft", "open", "paid", "void"}},
		{Name: "amount", Type: schema.Int},
		{Name: "paid_on", Type: schema.Date},
	}
}

func statesInvoiceStates() *entity.StatesConfig {
	return &entity.StatesConfig{
		Field:   "status",
		Initial: []string{"draft", "open"},
		Transitions: []entity.Transition{
			{Key: "issue", Label: "Issue", From: []string{"draft"}, To: "open"},
			{Key: "pay", Label: "Record payment", From: []string{"open"}, To: "paid", Stamp: "paid_on"},
			{Key: "void", From: []string{"draft", "open", "paid"}, To: "void"},
			{Key: "mark_overdue", From: []string{"open"}, To: "void", System: true},
		},
	}
}

// statesWorld builds the invoices CrudHandler over SQLite. mutators adjust
// the config (Advisory, ReadOnly state field, permissions) per test.
func statesWorld(t *testing.T, mutators ...func(*entity.EntityConfig)) (*CrudHandler, *sql.DB) {
	t.Helper()
	return statesWorldDDL(t, statesInvoiceDDL, mutators...)
}

// statesWorldDDL is statesWorld over a caller-supplied DDL, for the scoped
// variants whose table carries an extra column.
func statesWorldDDL(t *testing.T, ddl string, mutators ...func(*entity.EntityConfig)) (*CrudHandler, *sql.DB) {
	t.Helper()
	cfg := entity.EntityConfig{
		Name:     "invoices",
		Table:    "invoices",
		Fields:   statesInvoiceFields(),
		States:   statesInvoiceStates(),
		Exposure: &entity.ExposureConfig{Public: true},
	}.WithTimestamps(true)
	for _, m := range mutators {
		m(&cfg)
	}
	return setupSecurityTestHandler(t, cfg, ddl)
}

// seedStateInvoice inserts a row directly, bypassing every write path.
func seedStateInvoice(t *testing.T, db *sql.DB, id, status string, paidOn any) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO invoices (id, number, status, amount, paid_on, created_at, updated_at)
		 VALUES (?, ?, ?, 7, ?, '2020-01-01T00:00:00.000000Z', '2020-01-01T00:00:00.000000Z')`,
		id, "row "+id, status, paidOn,
	); err != nil {
		t.Fatal(err)
	}
}

// readStateInvoice returns the stored status and paid_on of row id.
func readStateInvoice(t *testing.T, db *sql.DB, id string) (string, sql.NullString) {
	t.Helper()
	var status string
	var paidOn sql.NullString
	if err := db.QueryRow(`SELECT status, paid_on FROM invoices WHERE id = ?`, id).Scan(&status, &paidOn); err != nil {
		t.Fatalf("read invoice %s: %v", id, err)
	}
	return status, paidOn
}

func countInvoices(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM invoices`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// decodeStateBody decodes a recorder body as a generic JSON object.
func decodeStateBody(t *testing.T, rr *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body %q: %v", rr.Body.String(), err)
	}
	return body
}

// movesList reads the "moves" array a StateError or conflict response carries.
func movesList(t *testing.T, body map[string]any) []string {
	t.Helper()
	raw, _ := body["moves"].([]any)
	out := make([]string, 0, len(raw))
	for _, m := range raw {
		s, _ := m.(string)
		out = append(out, s)
	}
	return out
}

// assertMovesEqual fails unless got is exactly want, in order.
func assertMovesEqual(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("moves = %v, want %v", got, want)
	}
}

// stateIDRequest builds a request whose {id} path value is set, the shape
// the Get/Update/Delete handlers read when invoked outside the router.
func stateIDRequest(t *testing.T, opts RequestOpts, id string) *http.Request {
	t.Helper()
	req := makeRequest(t, opts)
	req.SetPathValue("id", id)
	return req
}

// stateErr returns err as a *StateError, or fails the test.
func stateErr(t *testing.T, err error) *StateError {
	t.Helper()
	se, ok := errors.AsType[*StateError](err)
	if !ok {
		t.Fatalf("err = %v (%T), want *StateError", err, err)
	}
	return se
}

// ============================================================================
// Create
// ============================================================================

func TestStateCreateRefusesNonInitial(t *testing.T) {
	ch, db := statesWorld(t)

	rr := httptest.NewRecorder()
	ch.Create().ServeHTTP(rr, makeRequest(t, RequestOpts{
		Method: http.MethodPost, Path: "/invoices",
		Body: `{"number":"INV-1","status":"paid"}`,
	}))
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("create with non-initial status = %d (%s), want 422", rr.Code, rr.Body.String())
	}
	body := decodeStateBody(t, rr)
	fields, _ := body["fields"].(map[string]any)
	if _, ok := fields["status"]; !ok {
		t.Fatalf("422 body has no fields.status: %v", body)
	}
	if msg, _ := body["error"].(string); !strings.Contains(msg, "a new record starts at draft or open") {
		t.Fatalf("error %q does not name the initial values", msg)
	}
	assertMovesEqual(t, movesList(t, body), nil)
	if n := countInvoices(t, db); n != 0 {
		t.Fatalf("refused create still wrote %d rows", n)
	}
}

func TestStateCreateStoresInitialAndDefault(t *testing.T) {
	ch, db := statesWorld(t)

	for _, tc := range []struct {
		body string
		want string
	}{
		{`{"number":"INV-1","status":"open"}`, "open"},
		{`{"number":"INV-2"}`, "draft"}, // omitted → the Default
	} {
		rr := httptest.NewRecorder()
		ch.Create().ServeHTTP(rr, makeRequest(t, RequestOpts{
			Method: http.MethodPost, Path: "/invoices", Body: tc.body,
		}))
		if rr.Code != http.StatusCreated {
			t.Fatalf("create %s = %d (%s), want 201", tc.body, rr.Code, rr.Body.String())
		}
		if got := decodeStateBody(t, rr)["data"].(map[string]any)["status"]; got != tc.want {
			t.Fatalf("create %s returned status %v, want %q", tc.body, got, tc.want)
		}
	}
	var open, draft int
	if err := db.QueryRow(`SELECT COUNT(*) FROM invoices WHERE status = 'open'`).Scan(&open); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM invoices WHERE status = 'draft'`).Scan(&draft); err != nil {
		t.Fatal(err)
	}
	if open != 1 || draft != 1 {
		t.Fatalf("stored statuses: open=%d draft=%d, want 1 and 1", open, draft)
	}
}

func TestStateCreateRefusesStamp(t *testing.T) {
	ch, db := statesWorld(t)

	rr := httptest.NewRecorder()
	ch.Create().ServeHTTP(rr, makeRequest(t, RequestOpts{
		Method: http.MethodPost, Path: "/invoices",
		Body: `{"number":"INV-1","paid_on":"2026-01-01"}`,
	}))
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("create with a stamp = %d (%s), want 422", rr.Code, rr.Body.String())
	}
	body := decodeStateBody(t, rr)
	fields, _ := body["fields"].(map[string]any)
	if _, ok := fields["paid_on"]; !ok {
		t.Fatalf("422 body has no fields.paid_on: %v", body)
	}
	if msg, _ := body["error"].(string); !strings.Contains(msg, "paid_on is set by a move, not on create") {
		t.Fatalf("error %q does not name the stamp rule", msg)
	}
	if n := countInvoices(t, db); n != 0 {
		t.Fatalf("refused create still wrote %d rows", n)
	}
}

func TestStateCreateAcceptsNullStamp(t *testing.T) {
	ch, db := statesWorld(t)

	rr := httptest.NewRecorder()
	ch.Create().ServeHTTP(rr, makeRequest(t, RequestOpts{
		Method: http.MethodPost, Path: "/invoices",
		Body: `{"number":"INV-1","status":"open","paid_on":null}`,
	}))
	if rr.Code != http.StatusCreated {
		t.Fatalf("create with null stamp = %d (%s), want 201", rr.Code, rr.Body.String())
	}
	_, paidOn := readStateInvoice(t, db, decodeStateBody(t, rr)["data"].(map[string]any)["id"].(string))
	if paidOn.Valid {
		t.Fatalf("null paid_on stored as %q", paidOn.String)
	}
}

// ============================================================================
// Update (PATCH/PUT)
// ============================================================================

func TestStateUpdateRefusesStatusChange(t *testing.T) {
	ch, db := statesWorld(t)
	seedStateInvoice(t, db, "i1", "open", nil)

	rr := httptest.NewRecorder()
	ch.Update().ServeHTTP(rr, stateIDRequest(t, RequestOpts{
		Method: http.MethodPatch, Path: "/invoices/i1", Body: `{"status":"paid"}`,
	}, "i1"))
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("PATCH status change = %d (%s), want 422", rr.Code, rr.Body.String())
	}
	body := decodeStateBody(t, rr)
	fields, _ := body["fields"].(map[string]any)
	if _, ok := fields["status"]; !ok {
		t.Fatalf("422 body has no fields.status: %v", body)
	}
	// The non-system moves open from "open"; mark_overdue is System and
	// must not be offered to a caller.
	assertMovesEqual(t, movesList(t, body), []string{"pay", "void"})
	if status, _ := readStateInvoice(t, db, "i1"); status != "open" {
		t.Fatalf("refused PATCH still moved the row to %q", status)
	}
}

func TestStateUpdateSameStatusPasses(t *testing.T) {
	ch, db := statesWorld(t)
	seedStateInvoice(t, db, "i1", "open", nil)

	rr := httptest.NewRecorder()
	ch.Update().ServeHTTP(rr, stateIDRequest(t, RequestOpts{
		Method: http.MethodPatch, Path: "/invoices/i1", Body: `{"status":"open","amount":42}`,
	}, "i1"))
	if rr.Code != http.StatusOK {
		t.Fatalf("PATCH sending the stored status back = %d (%s), want 200", rr.Code, rr.Body.String())
	}
	var amount int
	if err := db.QueryRow(`SELECT amount, status FROM invoices WHERE id = 'i1'`).Scan(&amount, new(string)); err != nil {
		t.Fatal(err)
	}
	if amount != 42 {
		t.Fatalf("amount = %d, want 42", amount)
	}
	if status, _ := readStateInvoice(t, db, "i1"); status != "open" {
		t.Fatalf("status = %q, want open", status)
	}
}

func TestStateUpdatePutRoundTrip(t *testing.T) {
	ch, db := statesWorld(t)
	seedStateInvoice(t, db, "i1", "open", nil)
	if _, err := ch.RunTransition(context.Background(), "i1", "pay"); err != nil {
		t.Fatal(err)
	}
	_, paidOn := readStateInvoice(t, db, "i1")
	if !paidOn.Valid {
		t.Fatal("pay move did not stamp paid_on")
	}

	// GET the whole row and PUT it back verbatim.
	grr := httptest.NewRecorder()
	ch.Get().ServeHTTP(grr, stateIDRequest(t, RequestOpts{Method: http.MethodGet, Path: "/invoices/i1"}, "i1"))
	if grr.Code != http.StatusOK {
		t.Fatalf("GET = %d", grr.Code)
	}
	rowBody, err := json.Marshal(decodeStateBody(t, grr)["data"])
	if err != nil {
		t.Fatal(err)
	}
	urr := httptest.NewRecorder()
	ch.Update().ServeHTTP(urr, stateIDRequest(t, RequestOpts{
		Method: http.MethodPut, Path: "/invoices/i1", Body: string(rowBody),
	}, "i1"))
	if urr.Code != http.StatusOK {
		t.Fatalf("PUT round-trip = %d (%s), want 200", urr.Code, urr.Body.String())
	}
	status, paidAfter := readStateInvoice(t, db, "i1")
	if status != "paid" {
		t.Fatalf("PUT round-trip moved status to %q", status)
	}
	if paidAfter.String != paidOn.String {
		t.Fatalf("PUT round-trip rewrote paid_on: %q → %q", paidOn.String, paidAfter.String)
	}
}

func TestStateUpdateRefusesStampChange(t *testing.T) {
	ch, db := statesWorld(t)
	seedStateInvoice(t, db, "i1", "open", nil)
	if _, err := ch.RunTransition(context.Background(), "i1", "pay"); err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	ch.Update().ServeHTTP(rr, stateIDRequest(t, RequestOpts{
		Method: http.MethodPatch, Path: "/invoices/i1", Body: `{"paid_on":"2020-01-01"}`,
	}, "i1"))
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("PATCH stamp change = %d (%s), want 422", rr.Code, rr.Body.String())
	}
	body := decodeStateBody(t, rr)
	fields, _ := body["fields"].(map[string]any)
	if _, ok := fields["paid_on"]; !ok {
		t.Fatalf("422 body has no fields.paid_on: %v", body)
	}
	_, paidOn := readStateInvoice(t, db, "i1")
	if want := time.Now().UTC().Format(time.DateOnly); paidOn.String != want {
		t.Fatalf("refused PATCH moved paid_on to %q, want %q", paidOn.String, want)
	}

	// The NULL → value direction refuses too.
	seedStateInvoice(t, db, "i2", "open", nil)
	rr2 := httptest.NewRecorder()
	ch.Update().ServeHTTP(rr2, stateIDRequest(t, RequestOpts{
		Method: http.MethodPatch, Path: "/invoices/i2", Body: `{"paid_on":"2026-01-01"}`,
	}, "i2"))
	if rr2.Code != http.StatusUnprocessableEntity {
		t.Fatalf("PATCH paid_on NULL→value = %d (%s), want 422", rr2.Code, rr2.Body.String())
	}
	if _, paidOn2 := readStateInvoice(t, db, "i2"); paidOn2.Valid {
		t.Fatalf("refused PATCH still set paid_on = %q", paidOn2.String)
	}
}

// ============================================================================
// Batch
// ============================================================================

func TestStateBatchCreateRolledBack(t *testing.T) {
	ch, db := statesWorld(t)

	rr := httptest.NewRecorder()
	ch.BatchCreate().ServeHTTP(rr, makeRequest(t, RequestOpts{
		Method: http.MethodPost, Path: "/invoices/_batch",
		Body: `{"items":[{"number":"A"},{"number":"B","status":"paid"}]}`,
	}))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("batch create with a state change = %d (%s), want 400", rr.Code, rr.Body.String())
	}
	body := decodeStateBody(t, rr)
	if committed, _ := body["committed"].(bool); committed {
		t.Fatalf("batch reported committed: %v", body)
	}
	if n := countInvoices(t, db); n != 0 {
		t.Fatalf("rolled-back batch left %d rows", n)
	}
}

func TestStateBatchUpdateRolledBack(t *testing.T) {
	ch, db := statesWorld(t)
	seedStateInvoice(t, db, "i1", "open", nil)

	rr := httptest.NewRecorder()
	ch.BatchUpdate().ServeHTTP(rr, makeRequest(t, RequestOpts{
		Method: http.MethodPatch, Path: "/invoices/_batch",
		Body: `{"items":[{"id":"i1","status":"paid"}]}`,
	}))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("batch update with a state change = %d (%s), want 400", rr.Code, rr.Body.String())
	}
	body := decodeStateBody(t, rr)
	if committed, _ := body["committed"].(bool); committed {
		t.Fatalf("batch reported committed: %v", body)
	}
	if status, _ := readStateInvoice(t, db, "i1"); status != "open" {
		t.Fatalf("rolled-back batch still moved the row to %q", status)
	}
}

// ============================================================================
// Cascade writes
// ============================================================================

// statesCascadeWorld builds a customers parent with a cascading has_many of
// the invoices states entity, both on one SQLite database.
func statesCascadeWorld(t *testing.T) (*CrudHandler, *sql.DB) {
	t.Helper()
	db := setupDB(t, `CREATE TABLE customers (
		id TEXT PRIMARY KEY, name TEXT NOT NULL,
		created_at TEXT, updated_at TEXT
	)`, `CREATE TABLE invoices (
		id TEXT PRIMARY KEY,
		number TEXT NOT NULL,
		status TEXT DEFAULT 'draft',
		amount INTEGER,
		paid_on TEXT,
		created_at TEXT,
		updated_at TEXT,
		customer_id TEXT
	)`)
	invCfg := entity.EntityConfig{
		Name: "invoices", Table: "invoices",
		Fields:   append(statesInvoiceFields(), schema.Field{Name: "customer_id", Type: schema.String}),
		States:   statesInvoiceStates(),
		Exposure: &entity.ExposureConfig{Public: true},
	}.WithTimestamps(true)
	inv := entity.Define("invoices", invCfg)
	inv.SetDB(db)
	cust := entity.Define("customers", entity.EntityConfig{
		Name:   "customers",
		Table:  "customers",
		Fields: []schema.Field{{Name: "name", Type: schema.String, Required: true}},
		Relations: []entity.Relation{
			entity.HasMany("invoices", "invoices", "customer_id").WithCascadeWrite(true),
		},
		Exposure: &entity.ExposureConfig{Public: true},
	}.WithTimestamps(true))
	cust.SetDB(db)
	installOwnerExtractor(t)
	ch := NewCrudHandler(cust, db).WithJSONCase(CaseSnake)
	ch.Registry = stubRegistry{byName: map[string]*entity.Entity{"invoices": inv, "customers": cust}}
	return ch, db
}

func countTable(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestStateCascadeRefusesChildState(t *testing.T) {
	ch, db := statesCascadeWorld(t)

	// Create arm: a nested child in a non-initial state refuses the whole
	// request, parent included.
	rr := httptest.NewRecorder()
	ch.Create().ServeHTTP(rr, makeRequest(t, RequestOpts{
		Method: http.MethodPost, Path: "/customers",
		Body: `{"name":"Acme","invoices":[{"number":"INV-9","status":"paid"}]}`,
	}))
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("cascade create with child status paid = %d (%s), want 422", rr.Code, rr.Body.String())
	}
	if n := countTable(t, db, "customers"); n != 0 {
		t.Fatalf("refused cascade left %d parents", n)
	}
	if n := countTable(t, db, "invoices"); n != 0 {
		t.Fatalf("refused cascade left %d children", n)
	}

	// Update arm: a nested child PATCHed to a different state refuses.
	seedStateInvoice(t, db, "i1", "open", nil)
	if _, err := db.Exec(`INSERT INTO customers (id, name) VALUES ('c1', 'Acme')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE invoices SET customer_id = 'c1' WHERE id = 'i1'`); err != nil {
		t.Fatal(err)
	}
	urr := httptest.NewRecorder()
	ch.Update().ServeHTTP(urr, stateIDRequest(t, RequestOpts{
		Method: http.MethodPatch, Path: "/customers/c1",
		Body: `{"name":"Acme Ltd","invoices":[{"id":"i1","status":"paid"}]}`,
	}, "c1"))
	if urr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("cascade update with child status change = %d (%s), want 422", urr.Code, urr.Body.String())
	}
	if status, _ := readStateInvoice(t, db, "i1"); status != "open" {
		t.Fatalf("refused cascade update moved the child to %q", status)
	}
	var name string
	if err := db.QueryRow(`SELECT name FROM customers WHERE id = 'c1'`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "Acme" {
		t.Fatalf("refused cascade update still renamed the parent to %q", name)
	}
}

// ============================================================================
// UpsertOne
// ============================================================================

func TestStateUpsertInsertArmRefused(t *testing.T) {
	ch, db := statesWorld(t)

	_, err := ch.UpsertOne(context.Background(), map[string]any{
		"id": "i9", "number": "INV-9", "status": "paid",
	})
	stateErr(t, err)
	if n := countInvoices(t, db); n != 0 {
		t.Fatalf("refused upsert still inserted %d rows", n)
	}
}

func TestStateUpsertRefusesStatusChange(t *testing.T) {
	ch, db := statesWorld(t)
	seedStateInvoice(t, db, "i1", "open", nil)
	if _, err := ch.RunTransition(context.Background(), "i1", "pay"); err != nil {
		t.Fatal(err)
	}

	_, err := ch.UpsertOne(context.Background(), map[string]any{
		"id": "i1", "number": "renamed", "status": "draft",
	})
	stateErr(t, err)
	if status, _ := readStateInvoice(t, db, "i1"); status != "paid" {
		t.Fatalf("refused upsert moved the row to %q", status)
	}
}

func TestStateUpsertKeepsStoredStatus(t *testing.T) {
	ch, db := statesWorld(t)
	seedStateInvoice(t, db, "i1", "open", nil)
	if _, err := ch.RunTransition(context.Background(), "i1", "pay"); err != nil {
		t.Fatal(err)
	}

	// status omitted: the insert arm's Default must not clobber the stored
	// state through DO UPDATE SET.
	row, err := ch.UpsertOne(context.Background(), map[string]any{
		"id": "i1", "number": "renamed",
	})
	if err != nil {
		t.Fatalf("upsert without status: %v", err)
	}
	if row["status"] != "paid" {
		t.Fatalf("upsert returned status %v, want paid", row["status"])
	}
	status, paidOn := readStateInvoice(t, db, "i1")
	if status != "paid" {
		t.Fatalf("stored status = %q, want paid", status)
	}
	if !paidOn.Valid {
		t.Fatal("upsert cleared paid_on")
	}
	if want := time.Now().UTC().Format(time.DateOnly); paidOn.String != want {
		t.Fatalf("paid_on = %q, want %q", paidOn.String, want)
	}
	var number string
	if err := db.QueryRow(`SELECT number FROM invoices WHERE id = 'i1'`).Scan(&number); err != nil {
		t.Fatal(err)
	}
	if number != "renamed" {
		t.Fatalf("number = %q, want renamed", number)
	}

	// status sent back as the stored value passes.
	if _, err := ch.UpsertOne(context.Background(), map[string]any{
		"id": "i1", "number": "again", "status": "paid",
	}); err != nil {
		t.Fatalf("upsert with the stored status: %v", err)
	}
	if status, _ := readStateInvoice(t, db, "i1"); status != "paid" {
		t.Fatalf("stored status = %q, want paid", status)
	}
}

// ============================================================================
// TypedQuery.UpdateAll
// ============================================================================

func TestStateBulkUpdateRefusesGuarded(t *testing.T) {
	ch, db := statesWorld(t)
	seedStateInvoice(t, db, "i1", "open", nil)
	seedStateInvoice(t, db, "i2", "draft", nil)

	for _, tc := range []struct {
		name   string
		fields map[string]any
	}{
		{"state field", map[string]any{"status": "void"}},
		{"stamp", map[string]any{"paid_on": "2026-01-01"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := NewTypedQuery[map[string]any](ch)
			if _, err := q.UpdateAll(context.Background(), tc.fields); !errors.Is(err, ErrBulkStateWrite) {
				t.Fatalf("UpdateAll naming %v = %v, want ErrBulkStateWrite", tc.fields, err)
			}
			// The override does not release a bulk write: no hooks run,
			// so there would be no audit row.
			q2 := NewTypedQuery[map[string]any](ch)
			_, err := q2.UpdateAll(WithStateOverride(context.Background(), "backfill"), tc.fields)
			if !errors.Is(err, ErrBulkStateWrite) {
				t.Fatalf("UpdateAll under override = %v, want ErrBulkStateWrite", err)
			}
		})
	}
	for _, id := range []string{"i1", "i2"} {
		status, paidOn := readStateInvoice(t, db, id)
		if want := map[string]string{"i1": "open", "i2": "draft"}[id]; status != want {
			t.Fatalf("row %s status = %q, want %q", id, status, want)
		}
		if paidOn.Valid {
			t.Fatalf("row %s paid_on = %q, want untouched NULL", id, paidOn.String)
		}
	}
}

// ============================================================================
// In-process writes and hooks
// ============================================================================

func TestStateInProcessRefused(t *testing.T) {
	ch, db := statesWorld(t)
	seedStateInvoice(t, db, "i1", "open", nil)

	if _, err := ch.CreateOne(context.Background(), map[string]any{
		"number": "INV-9", "status": "paid",
	}); err == nil {
		t.Fatal("CreateOne accepted a non-initial status")
	} else {
		stateErr(t, err)
	}
	if n := countInvoices(t, db); n != 1 {
		t.Fatalf("refused CreateOne wrote rows (have %d, want 1)", n)
	}
	if _, err := ch.UpdateOne(context.Background(), "i1", map[string]any{"status": "paid"}); err == nil {
		t.Fatal("UpdateOne accepted a state change")
	} else {
		stateErr(t, err)
	}
	if status, _ := readStateInvoice(t, db, "i1"); status != "open" {
		t.Fatalf("refused UpdateOne moved the row to %q", status)
	}
}

func TestStateHookCannotSetStatus(t *testing.T) {
	ch, db := statesWorld(t)
	seedStateInvoice(t, db, "i1", "open", nil)
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.BeforeCreate, func(_ context.Context, data any) error {
		data.(map[string]any)["status"] = "paid"
		return nil
	})
	ch.Hooks.RegisterHook(hook.BeforeUpdate, func(_ context.Context, data any) error {
		data.(map[string]any)["status"] = "paid"
		return nil
	})

	if _, err := ch.CreateOne(context.Background(), map[string]any{"number": "INV-9"}); err == nil {
		t.Fatal("BeforeCreate hook setting status accepted")
	} else {
		stateErr(t, err)
	}
	if n := countInvoices(t, db); n != 1 {
		t.Fatalf("hook-driven create wrote rows (have %d, want 1)", n)
	}
	// The hook fires on a legitimate edit too and still cannot smuggle the
	// state field past the check.
	if _, err := ch.UpdateOne(context.Background(), "i1", map[string]any{"amount": 9}); err == nil {
		t.Fatal("BeforeUpdate hook setting status accepted")
	} else {
		stateErr(t, err)
	}
	if status, _ := readStateInvoice(t, db, "i1"); status != "open" {
		t.Fatalf("hook-driven update moved the row to %q", status)
	}
}

// ============================================================================
// Advisory: the same direct writes all pass
// ============================================================================

func TestAdvisoryDirectWritesPass(t *testing.T) {
	ch, db := statesWorld(t, func(c *entity.EntityConfig) { c.States.Advisory = true })

	rr := httptest.NewRecorder()
	ch.Create().ServeHTTP(rr, makeRequest(t, RequestOpts{
		Method: http.MethodPost, Path: "/invoices",
		Body: `{"number":"INV-1","status":"paid","paid_on":"2026-01-01"}`,
	}))
	if rr.Code != http.StatusCreated {
		t.Fatalf("advisory create with status paid = %d (%s), want 201", rr.Code, rr.Body.String())
	}
	status, paidOn := readStateInvoice(t, db, decodeStateBody(t, rr)["data"].(map[string]any)["id"].(string))
	if status != "paid" || paidOn.String != "2026-01-01" {
		t.Fatalf("advisory create stored status=%q paid_on=%v", status, paidOn)
	}

	urr := httptest.NewRecorder()
	ch.Update().ServeHTTP(urr, stateIDRequest(t, RequestOpts{
		Method: http.MethodPatch, Path: "/invoices/INV1", Body: `{"status":"void"}`,
	}, statusRowID(t, db)))
	if urr.Code != http.StatusOK {
		t.Fatalf("advisory PATCH status change = %d (%s), want 200", urr.Code, urr.Body.String())
	}
	if status, _ := readStateInvoice(t, db, statusRowID(t, db)); status != "void" {
		t.Fatalf("advisory PATCH stored status %q", status)
	}

	seedStateInvoice(t, db, "i2", "draft", nil)
	q := NewTypedQuery[map[string]any](ch)
	n, err := q.UpdateAll(context.Background(), map[string]any{"status": "void"})
	if err != nil {
		t.Fatalf("advisory UpdateAll naming status: %v", err)
	}
	if n < 1 {
		t.Fatalf("advisory UpdateAll touched %d rows", n)
	}
	if status, _ := readStateInvoice(t, db, "i2"); status != "void" {
		t.Fatalf("advisory UpdateAll stored status %q", status)
	}
}

// statusRowID returns the id of the single invoices row.
func statusRowID(t *testing.T, db *sql.DB) string {
	t.Helper()
	var id string
	if err := db.QueryRow(`SELECT id FROM invoices LIMIT 1`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// ============================================================================
// WithServerWrites does not release the state field
// ============================================================================

func TestServerWritesDoesNotReleaseStatus(t *testing.T) {
	ch, db := statesWorld(t)
	seedStateInvoice(t, db, "i1", "open", nil)
	ctx := WithServerWrites(context.Background())

	if _, err := ch.CreateOne(ctx, map[string]any{"number": "INV-9", "status": "paid"}); err == nil {
		t.Fatal("WithServerWrites released the state field on create")
	} else {
		stateErr(t, err)
	}
	if n := countInvoices(t, db); n != 1 {
		t.Fatalf("refused server-write create wrote rows (have %d, want 1)", n)
	}
	if _, err := ch.UpdateOne(ctx, "i1", map[string]any{"status": "paid"}); err == nil {
		t.Fatal("WithServerWrites released the state field on update")
	} else {
		stateErr(t, err)
	}
	if status, _ := readStateInvoice(t, db, "i1"); status != "open" {
		t.Fatalf("server-write update moved the row to %q", status)
	}
}

// ============================================================================
// WithStateOverride
// ============================================================================

func TestStateOverrideNeedsReason(t *testing.T) {
	ch, db := statesWorld(t)

	_, err := ch.CreateOne(WithStateOverride(context.Background(), "  "), map[string]any{
		"number": "INV-9", "status": "paid",
	})
	if !errors.Is(err, ErrStateOverrideNoReason) {
		t.Fatalf("blank reason = %v, want ErrStateOverrideNoReason", err)
	}
	if n := countInvoices(t, db); n != 0 {
		t.Fatalf("refused override create wrote %d rows", n)
	}
}

func TestStateOverrideNeedsAuditLog(t *testing.T) {
	ch, db := statesWorld(t)

	_, err := ch.CreateOne(WithStateOverride(context.Background(), "seed import"), map[string]any{
		"number": "INV-9", "status": "paid",
	})
	if !errors.Is(err, ErrStateOverrideUnaudited) {
		t.Fatalf("unaudited entity override = %v, want ErrStateOverrideUnaudited", err)
	}
	if n := countInvoices(t, db); n != 0 {
		t.Fatalf("refused override create wrote %d rows", n)
	}
}

func TestStateOverrideWritesStatus(t *testing.T) {
	ch, db := statesWorld(t)
	ch.Entity.MarkAudited()
	seedStateInvoice(t, db, "i1", "open", nil)
	ctx := WithStateOverride(context.Background(), "seed import")

	created, err := ch.CreateOne(ctx, map[string]any{"number": "INV-9", "status": "paid"})
	if err != nil {
		t.Fatalf("override create: %v", err)
	}
	if created["status"] != "paid" {
		t.Fatalf("override create returned status %v", created["status"])
	}
	if status, _ := readStateInvoice(t, db, created["id"].(string)); status != "paid" {
		t.Fatalf("override create stored status %q", status)
	}

	if _, err := ch.UpdateOne(ctx, "i1", map[string]any{"status": "void", "paid_on": "2026-02-02"}); err != nil {
		t.Fatalf("override update: %v", err)
	}
	status, paidOn := readStateInvoice(t, db, "i1")
	if status != "void" {
		t.Fatalf("override update stored status %q", status)
	}
	if paidOn.String != "2026-02-02" {
		t.Fatalf("override update stored paid_on %q", paidOn.String)
	}
}

// A ReadOnly state field is still written under the override: the override
// releases the column, not just the value check.
func TestStateOverrideWritesReadOnlyStatus(t *testing.T) {
	ch, db := statesWorld(t, func(c *entity.EntityConfig) {
		for i := range c.Fields {
			if c.Fields[i].Name == "status" {
				c.Fields[i].ReadOnly = true
			}
		}
	})
	ch.Entity.MarkAudited()
	seedStateInvoice(t, db, "i1", "open", nil)

	if _, err := ch.UpdateOne(WithStateOverride(context.Background(), "repair"), "i1", map[string]any{
		"status": "void",
	}); err != nil {
		t.Fatalf("override on a ReadOnly state field: %v", err)
	}
	if status, _ := readStateInvoice(t, db, "i1"); status != "void" {
		t.Fatalf("override stored status %q, want void", status)
	}
}

// ============================================================================
// MCP update tool
// ============================================================================

func TestMCPUpdateRefusesStateChange(t *testing.T) {
	ch, db := statesWorld(t)
	seedStateInvoice(t, db, "i1", "open", nil)
	r := router.New()
	RegisterCrudRoutes(r, ch, "/invoices")
	srv := mcp.NewServer()
	if err := RegisterEntityMCPTools(srv, ch, r); err != nil {
		t.Fatal(err)
	}

	if _, err := srv.CallTool(ctxWithUser("u1"), "invoices_update", map[string]any{
		"id": "i1", "status": "paid",
	}); err == nil {
		t.Fatal("MCP update tool accepted a state change")
	}
	if status, _ := readStateInvoice(t, db, "i1"); status != "open" {
		t.Fatalf("MCP update moved the row to %q", status)
	}
}
