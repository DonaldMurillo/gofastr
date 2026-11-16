package crud

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

const (
	storedPAN = "4111111111111111"
	maskedPAN = "****1111"
)

func maskLastFour(_ context.Context, v any) any {
	s, _ := v.(string)
	return "****" + s[max(len(s)-4, 0):]
}

type maskedCards struct {
	db      *sql.DB
	cards   *CrudHandler
	wallets *CrudHandler
	router  *router.Router
	mcp     *mcp.Server
}

// newMaskedCards builds wallets with a cascade write onto cards, whose pan
// is masked, and seeds wallet w1 holding card c1.
func newMaskedCards(t *testing.T, jsonCase JSONCase, pan schema.Field) maskedCards {
	t.Helper()
	panDDL := "pan TEXT"
	if pan.Required {
		panDDL += " NOT NULL"
	}
	db := setupDB(t,
		`CREATE TABLE wallets (id TEXT PRIMARY KEY, name TEXT)`,
		`CREATE TABLE cards (id TEXT PRIMARY KEY, wallet_id TEXT, label TEXT, `+panDDL+`)`,
		`INSERT INTO wallets (id, name) VALUES ('w1', 'w')`,
	)
	pan.Name, pan.Type, pan.Mask = "pan", schema.String, maskLastFour
	cards := entity.Define("cards", entity.EntityConfig{
		Name: "cards", Table: "cards",
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "wallet_id", Type: schema.String},
			{Name: "label", Type: schema.String},
			pan,
		},
	}.WithTimestamps(false))
	wallets := entity.Define("wallets", entity.EntityConfig{
		Name: "wallets", Table: "wallets",
		Fields:    []schema.Field{{Name: "name", Type: schema.String}},
		Relations: []entity.Relation{entity.HasMany("cards", "cards", "wallet_id").WithCascadeWrite(true)},
	}.WithTimestamps(false))
	cards.SetDB(db)
	wallets.SetDB(db)
	reg := stubRegistry{byName: map[string]*entity.Entity{"cards": cards, "wallets": wallets}}
	m := maskedCards{db: db, router: router.New(), mcp: mcp.NewServer()}
	m.cards = NewCrudHandler(cards, db).WithJSONCase(jsonCase)
	m.cards.Registry = reg
	m.wallets = NewCrudHandler(wallets, db).WithJSONCase(jsonCase)
	m.wallets.Registry = reg
	RegisterCrudRoutes(m.router, m.cards, "/cards")
	if err := RegisterEntityMCPTools(m.mcp, m.cards, m.router); err != nil {
		t.Fatal(err)
	}
	m.seed(t, storedPAN)
	return m
}

func (m maskedCards) seed(t *testing.T, pan any) {
	t.Helper()
	if _, err := m.db.Exec(`DELETE FROM cards`); err != nil {
		t.Fatal(err)
	}
	if _, err := m.db.Exec(`INSERT INTO cards (id, wallet_id, label, pan) VALUES ('c1', 'w1', 'old', ?)`, pan); err != nil {
		t.Fatal(err)
	}
}

func (m maskedCards) stored(t *testing.T) (label string, pan sql.NullString) {
	t.Helper()
	if err := m.db.QueryRow(`SELECT label, pan FROM cards WHERE id = 'c1'`).Scan(&label, &pan); err != nil {
		t.Fatal(err)
	}
	return label, pan
}

func (m maskedCards) serve(method, path string, body any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	r := withTestUser(httptest.NewRequest(method, path, bytes.NewReader(raw)), "u1")
	r.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	m.router.ServeHTTP(rec, r)
	return rec
}

func httpOK(rec *httptest.ResponseRecorder) error {
	if rec.Code != http.StatusOK {
		return errors.New(rec.Body.String())
	}
	return nil
}

// maskedWriteSites is every path that updates a stored row from a body:
// each writes label and pan onto card c1.
var maskedWriteSites = []struct {
	name  string
	write func(m maskedCards, label, pan string) error
}{
	{"REST PATCH", func(m maskedCards, label, pan string) error {
		return httpOK(m.serve(http.MethodPatch, "/cards/c1", map[string]any{"label": label, "pan": pan}))
	}},
	{"REST PUT", func(m maskedCards, label, pan string) error {
		return httpOK(m.serve(http.MethodPut, "/cards/c1", map[string]any{"label": label, "pan": pan}))
	}},
	{"_batch update", func(m maskedCards, label, pan string) error {
		return httpOK(m.serve(http.MethodPatch, "/cards/_batch", map[string]any{
			"items": []map[string]any{{"id": "c1", "label": label, "pan": pan}},
		}))
	}},
	{"MCP update tool", func(m maskedCards, label, pan string) error {
		_, err := m.mcp.CallTool(ctxWithUser("u1"), "cards_update", map[string]any{"id": "c1", "label": label, "pan": pan})
		return err
	}},
	{"UpdateOne", func(m maskedCards, label, pan string) error {
		_, err := m.cards.UpdateOne(ctxWithUser("u1"), "c1", map[string]any{"label": label, "pan": pan})
		return err
	}},
	{"BatchUpdateMany", func(m maskedCards, label, pan string) error {
		_, err := m.cards.BatchUpdateMany(ctxWithUser("u1"), []string{"c1"}, []map[string]any{{"label": label, "pan": pan}})
		return err
	}},
	{"UpsertOne conflict arm", func(m maskedCards, label, pan string) error {
		_, err := m.cards.UpsertOne(ctxWithUser("u1"), map[string]any{"id": "c1", "wallet_id": "w1", "label": label, "pan": pan})
		return err
	}},
	{"cascade child update", func(m maskedCards, label, pan string) error {
		_, err := m.wallets.UpdateOne(ctxWithUser("u1"), "w1", map[string]any{
			"cards": []any{map[string]any{"id": "c1", "label": label, "pan": pan}},
		})
		return err
	}},
}

// A masked field sent back as it was read is left out of the write on
// every update path; the rest of the write lands.
func TestMaskedValueRoundTripKeepsStoredValue(t *testing.T) {
	for _, site := range maskedWriteSites {
		t.Run(site.name, func(t *testing.T) {
			m := newMaskedCards(t, CaseSnake, schema.Field{})
			if err := site.write(m, "new", maskedPAN); err != nil {
				t.Fatalf("write: %v", err)
			}
			label, pan := m.stored(t)
			if pan.String != storedPAN {
				t.Errorf("stored pan = %q, want the card number %q kept", pan.String, storedPAN)
			}
			if label != "new" {
				t.Errorf("stored label = %q, want the rest of the write to land", label)
			}
		})
	}
}

// Any other value for a masked field is a real write, on the same paths.
func TestMaskedFieldTakesANewValue(t *testing.T) {
	const next = "4222222222222222"
	for _, site := range maskedWriteSites {
		t.Run(site.name, func(t *testing.T) {
			m := newMaskedCards(t, CaseSnake, schema.Field{})
			if err := site.write(m, "new", next); err != nil {
				t.Fatalf("write: %v", err)
			}
			if _, pan := m.stored(t); pan.String != next {
				t.Errorf("stored pan = %q, want %q", pan.String, next)
			}
		})
	}
}

// The record as GET answers it, one key changed, written back whole
// under camelCase wire keys.
func TestMaskedRoundTripOfAReadRecord(t *testing.T) {
	m := newMaskedCards(t, CaseCamel, schema.Field{})
	rec := m.serve(http.MethodGet, "/cards/c1", nil)
	row := decodeSingleResponse(t, rec.Body.Bytes())
	if row["pan"] != maskedPAN {
		t.Fatalf("premise: get reads pan %v, want %s", row["pan"], maskedPAN)
	}
	row["label"] = "new"
	delete(row, "id")
	if rec := m.serve(http.MethodPut, "/cards/c1", row); rec.Code != http.StatusOK {
		t.Fatalf("PUT = %d %s", rec.Code, rec.Body.String())
	}
	if label, pan := m.stored(t); pan.String != storedPAN || label != "new" {
		t.Errorf("stored (label, pan) = (%q, %q), want (new, %s)", label, pan.String, storedPAN)
	}
}

// A body that carries nothing but the echo has no field left to write.
func TestMaskedEchoAloneWritesNothing(t *testing.T) {
	m := newMaskedCards(t, CaseSnake, schema.Field{})
	rec := m.serve(http.MethodPatch, "/cards/c1", map[string]any{"pan": maskedPAN})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("PATCH = %d %s, want 400 (no fields to update)", rec.Code, rec.Body.String())
	}
	if _, pan := m.stored(t); pan.String != storedPAN {
		t.Errorf("stored pan = %q, want %q", pan.String, storedPAN)
	}
}

// An upsert that carries only the echo writes nothing and answers the row.
func TestMaskedEchoAloneUpsertAnswersTheRow(t *testing.T) {
	m := newMaskedCards(t, CaseSnake, schema.Field{Required: true})
	row, err := m.cards.UpsertOne(ctxWithUser("u1"), map[string]any{"id": "c1", "pan": maskedPAN})
	if err != nil || row["label"] != "old" {
		t.Fatalf("UpsertOne = %v, %v; want the stored row", row, err)
	}
	if label, pan := m.stored(t); pan.String != storedPAN || label != "old" {
		t.Errorf("stored (label, pan) = (%q, %q), want it untouched", label, pan.String)
	}
}

// A stored NULL has no masked form, so a value sent for it is written.
func TestMaskedStoredNullIsNeverAnEcho(t *testing.T) {
	m := newMaskedCards(t, CaseSnake, schema.Field{})
	m.seed(t, nil)
	if rec := m.serve(http.MethodPatch, "/cards/c1", map[string]any{"pan": "****"}); rec.Code != http.StatusOK {
		t.Fatalf("PATCH = %d %s", rec.Code, rec.Body.String())
	}
	if _, pan := m.stored(t); pan.String != "****" {
		t.Errorf("stored pan = %q, want the sent value", pan.String)
	}
}

// A stored empty string is masked like any value, and its echo is dropped.
func TestMaskedStoredEmptyEchoIsDropped(t *testing.T) {
	m := newMaskedCards(t, CaseSnake, schema.Field{})
	m.seed(t, "")
	if rec := m.serve(http.MethodPatch, "/cards/c1", map[string]any{"label": "new", "pan": "****"}); rec.Code != http.StatusOK {
		t.Fatalf("PATCH = %d %s", rec.Code, rec.Body.String())
	}
	if _, pan := m.stored(t); !pan.Valid || pan.String != "" {
		t.Errorf("stored pan = %+v, want the empty string kept", pan)
	}
}

// A required masked field's echo passes validation on every path: the
// upsert, which validates the whole row, reads it as held.
func TestMaskedRequiredFieldEchoIsAccepted(t *testing.T) {
	for _, site := range maskedWriteSites {
		t.Run(site.name, func(t *testing.T) {
			m := newMaskedCards(t, CaseSnake, schema.Field{Required: true, Pattern: `^[0-9]{16}$`})
			if err := site.write(m, "new", maskedPAN); err != nil {
				t.Fatalf("write: %v", err)
			}
			if label, pan := m.stored(t); pan.String != storedPAN || label != "new" {
				t.Errorf("stored (label, pan) = (%q, %q), want (new, %s)", label, pan.String, storedPAN)
			}
		})
	}
}

// A create has no stored row, so it stores what it is sent.
func TestMaskedCreateStoresWhatItIsSent(t *testing.T) {
	m := newMaskedCards(t, CaseSnake, schema.Field{})
	rec := m.serve(http.MethodPost, "/cards", map[string]any{"id": "c2", "label": "l", "pan": maskedPAN})
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST = %d %s", rec.Code, rec.Body.String())
	}
	var pan string
	if err := m.db.QueryRow(`SELECT pan FROM cards WHERE id = 'c2'`).Scan(&pan); err != nil {
		t.Fatal(err)
	}
	if pan != maskedPAN {
		t.Errorf("created pan = %q, want %q", pan, maskedPAN)
	}
}

// The stored row is read under the write's owner scope: another owner's
// echo finds no row, as any write of theirs does.
func TestMaskedEchoReadsUnderOwnerScope(t *testing.T) {
	installOwnerExtractor(t)
	db := setupDB(t,
		`CREATE TABLE vault (id TEXT PRIMARY KEY, user_id TEXT, pan TEXT)`,
		`INSERT INTO vault (id, user_id, pan) VALUES ('v1', 'alice', '`+storedPAN+`')`,
	)
	ent := entity.Define("vault", makeEntityConfig("vault", "vault", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String},
		{Name: "pan", Type: schema.String, Mask: maskLastFour},
	}).WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	for user, want := range map[string]error{"alice": errNoFieldsToUpdate, "bob": errNotFound} {
		_, err := ch.UpdateOne(ctxWithUser(user), "v1", map[string]any{"pan": maskedPAN})
		if !errors.Is(err, want) {
			t.Errorf("%s echoing the masked pan: err = %v, want %v", user, err, want)
		}
	}
}

func TestSameWireValue(t *testing.T) {
	for _, c := range []struct {
		masked, given any
		want          bool
	}{
		{"****1111", "****1111", true},
		{[]byte("****1111"), "****1111", true},
		{"****1111", "4111", false},
		{"0", json.Number("0"), false},
		{0, json.Number("0"), true},
		{0, float64(0), true},
		{0, "0", true},
		{true, "true", true},
		{nil, nil, true},
		{nil, "null", false},
		{map[string]any{"a": 1}, map[string]any{"a": json.Number("1")}, true},
	} {
		if got := sameWireValue(c.masked, c.given); got != c.want {
			t.Errorf("sameWireValue(%#v, %#v) = %v, want %v", c.masked, c.given, got, c.want)
		}
	}
}

// The update and the upsert's conflict arm keep a NOT NULL masked column
// on Postgres, where the upsert binds the held value into its VALUES.
func TestPGMaskedRoundTripKeepsStoredValue(t *testing.T) {
	ch, db := pgCrudSetup(t, entity.EntityConfig{
		Name: "mask_cards", Table: "mask_cards",
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "label", Type: schema.String},
			{Name: "pan", Type: schema.String, Required: true, Mask: maskLastFour},
		},
	}.WithTimestamps(false), `CREATE TABLE mask_cards (id TEXT PRIMARY KEY, label TEXT, pan TEXT NOT NULL)`)
	pgSeed(t, db, "mask_cards", []map[string]any{{"id": "c1", "label": "old", "pan": storedPAN}})
	ctx := ctxWithUser("u1")
	if _, err := ch.UpdateOne(ctx, "c1", map[string]any{"label": "patched", "pan": maskedPAN}); err != nil {
		t.Fatalf("UpdateOne: %v", err)
	}
	if _, err := ch.UpsertOne(ctx, map[string]any{"id": "c1", "label": "upserted", "pan": maskedPAN}); err != nil {
		t.Fatalf("UpsertOne: %v", err)
	}
	var label, pan string
	if err := db.QueryRow(`SELECT label, pan FROM mask_cards WHERE id = 'c1'`).Scan(&label, &pan); err != nil {
		t.Fatal(err)
	}
	if label != "upserted" || pan != storedPAN {
		t.Errorf("stored (label, pan) = (%q, %q), want (upserted, %s)", label, pan, storedPAN)
	}
}

// A BeforeCreate hook that moves the key moves the echo check with it: the
// held value of the row first named never reaches the row written.
func TestMaskedUpsertEchoFollowsAHookChangedKey(t *testing.T) {
	for _, c := range []struct {
		name, seedLower, sent, want string
	}{
		{"new key has no row", "", maskedPAN, maskedPAN},
		{"new key holds another value", "5500000000002222", maskedPAN, maskedPAN},
		{"new key's own echo", "5500000000002222", "****2222", "5500000000002222"},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := newMaskedCards(t, CaseSnake, schema.Field{Required: true})
			m.cards.Hooks = hook.NewHookRegistry()
			m.cards.Hooks.RegisterHook(hook.BeforeCreate, func(_ context.Context, data any) error {
				body := data.(map[string]any)
				body["id"] = strings.ToLower(body["id"].(string))
				return nil
			})
			if _, err := m.db.Exec(`INSERT INTO cards (id, label, pan) VALUES ('ABC', 'upper', ?)`, storedPAN); err != nil {
				t.Fatal(err)
			}
			if c.seedLower != "" {
				if _, err := m.db.Exec(`INSERT INTO cards (id, label, pan) VALUES ('abc', 'lower', ?)`, c.seedLower); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := m.cards.UpsertOne(ctxWithUser("u1"), map[string]any{"id": "ABC", "label": "new", "pan": c.sent}); err != nil {
				t.Fatalf("UpsertOne: %v", err)
			}
			var lower, upper string
			if err := m.db.QueryRow(`SELECT pan FROM cards WHERE id = 'abc'`).Scan(&lower); err != nil {
				t.Fatal(err)
			}
			if err := m.db.QueryRow(`SELECT pan FROM cards WHERE id = 'ABC'`).Scan(&upper); err != nil {
				t.Fatal(err)
			}
			if lower != c.want {
				t.Errorf("row abc pan = %q, want %q", lower, c.want)
			}
			if upper != storedPAN {
				t.Errorf("row ABC pan = %q, want it untouched", upper)
			}
		})
	}
}

// The hook-changed key moves the echo check on Postgres too, where the
// upsert binds the held value into its VALUES.
func TestPGMaskedUpsertEchoFollowsAHookChangedKey(t *testing.T) {
	for _, c := range []struct {
		name, table, seedLower, sent, want string
	}{
		{"new key has no row", "mask_hook_cards_a", "", maskedPAN, maskedPAN},
		{"new key holds another value", "mask_hook_cards_b", "5500000000002222", maskedPAN, maskedPAN},
		{"new key's own echo", "mask_hook_cards_c", "5500000000002222", "****2222", "5500000000002222"},
	} {
		t.Run(c.name, func(t *testing.T) {
			ch, db := pgCrudSetup(t, entity.EntityConfig{
				Name: c.table, Table: c.table,
				Fields: []schema.Field{
					{Name: "id", Type: schema.String},
					{Name: "label", Type: schema.String},
					{Name: "pan", Type: schema.String, Required: true, Mask: maskLastFour},
				},
			}.WithTimestamps(false), `CREATE TABLE `+c.table+` (id TEXT PRIMARY KEY, label TEXT, pan TEXT NOT NULL)`)
			ch.Hooks = hook.NewHookRegistry()
			ch.Hooks.RegisterHook(hook.BeforeCreate, func(_ context.Context, data any) error {
				body := data.(map[string]any)
				body["id"] = strings.ToLower(body["id"].(string))
				return nil
			})
			seed := []map[string]any{{"id": "ABC", "label": "upper", "pan": storedPAN}}
			if c.seedLower != "" {
				seed = append(seed, map[string]any{"id": "abc", "label": "lower", "pan": c.seedLower})
			}
			pgSeed(t, db, c.table, seed)
			if _, err := ch.UpsertOne(ctxWithUser("u1"), map[string]any{"id": "ABC", "label": "new", "pan": c.sent}); err != nil {
				t.Fatalf("UpsertOne: %v", err)
			}
			var lower, upper string
			if err := db.QueryRow(`SELECT pan FROM ` + c.table + ` WHERE id = 'abc'`).Scan(&lower); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(`SELECT pan FROM ` + c.table + ` WHERE id = 'ABC'`).Scan(&upper); err != nil {
				t.Fatal(err)
			}
			if lower != c.want {
				t.Errorf("row abc pan = %q, want %q", lower, c.want)
			}
			if upper != storedPAN {
				t.Errorf("row ABC pan = %q, want it untouched", upper)
			}
		})
	}
}
