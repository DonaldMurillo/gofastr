package crud

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// roundTripHandler is one entity with a column of each type whose value
// a read must hand back in a spelling the write path and the filter
// parser both accept.
func roundTripHandler(t *testing.T) (*CrudHandler, *sql.DB) {
	t.Helper()
	return setupSecurityTestHandler(t, roundTripFields(), `CREATE TABLE things (id INTEGER PRIMARY KEY AUTOINCREMENT, amount INTEGER, occurred_at TIMESTAMP, day DATE, meta TEXT, line_2 TEXT)`)
}

func roundTripFields() entity.EntityConfig {
	return makeEntityConfig("things", "things", "", []schema.Field{
		{Name: "id", Type: schema.Int, AutoGenerate: schema.AutoIncrement},
		{Name: "amount", Type: schema.Int},
		{Name: "occurred_at", Type: schema.Timestamp},
		{Name: "day", Type: schema.Date},
		{Name: "meta", Type: schema.JSON},
		{Name: "line_2", Type: schema.String},
	}, func(c *entity.EntityConfig) { c.Exposure = &entity.ExposureConfig{Public: true} })
}

// The same round trips on Postgres: TIMESTAMPTZ, DATE and JSONB columns.
func TestPGFieldRoundTrips(t *testing.T) {
	setup := func(t *testing.T) (*CrudHandler, *sql.DB) {
		return pgCrudSetup(t, roundTripFields(), `CREATE TABLE things (id SERIAL PRIMARY KEY, amount BIGINT, occurred_at TIMESTAMPTZ, day DATE, meta JSONB, line_2 TEXT)`)
	}
	t.Run("int", func(t *testing.T) { ch, db := setup(t); checkIntFraction(t, ch, db) })
	t.Run("timestamp", func(t *testing.T) { ch, _ := setup(t); checkTimestampFilter(t, ch) })
	t.Run("date", func(t *testing.T) { ch, _ := setup(t); checkDateRoundTrip(t, ch) })
	t.Run("typed date", func(t *testing.T) { ch, _ := setup(t); checkTypedDateResave(t, ch) })
	t.Run("json huge number", func(t *testing.T) { ch, _ := setup(t); checkJSONHugeNumber(t, ch) })
	t.Run("typed timestamp", func(t *testing.T) { ch, _ := setup(t); checkTypedTimestamp(t, ch) })
	t.Run("json", func(t *testing.T) { ch, db := setup(t); checkJSONString(t, ch, db, false) })
}

func rtCreate(t *testing.T, ch *CrudHandler, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	ch.Create()(w, makeRequest(t, RequestOpts{Method: "POST", Path: "/things", Body: body, UserID: "u1"}))
	return w
}

func rtList(t *testing.T, ch *CrudHandler, query string) []map[string]any {
	t.Helper()
	w := httptest.NewRecorder()
	ch.List()(w, makeRequest(t, RequestOpts{Method: "GET", Path: "/things?" + query, UserID: "u1"}))
	if w.Code != http.StatusOK {
		t.Fatalf("list ?%s = %d: %s", query, w.Code, w.Body.String())
	}
	return decodeListResponse(t, w.Body.String()).Data
}

func rtGet(t *testing.T, ch *CrudHandler, id string) map[string]any {
	t.Helper()
	w := httptest.NewRecorder()
	req := makeRequest(t, RequestOpts{Method: "GET", Path: "/things/" + id, UserID: "u1"})
	req.SetPathValue("id", id)
	ch.Get()(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get %s = %d: %s", id, w.Code, w.Body.String())
	}
	return decodeSingleResponse(t, w.Body.Bytes())
}

func rtUpdate(t *testing.T, ch *CrudHandler, id, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := makeRequest(t, RequestOpts{Method: "PATCH", Path: "/things/" + id, Body: body, UserID: "u1"})
	req.SetPathValue("id", id)
	ch.Update()(w, req)
	return w
}

// A JSON number whose decimal text is not an integer rounds to one as a
// float64: 1.0000000000000001 is exactly 1.0, and an Int column stored 1.
func TestIntRefusesInexactFraction(t *testing.T) {
	ch, db := roundTripHandler(t)
	checkIntFraction(t, ch, db)
}

func checkIntFraction(t *testing.T, ch *CrudHandler, db *sql.DB) {
	if w := rtCreate(t, ch, `{"amount":1.0000000000000001}`); w.Code != http.StatusBadRequest {
		t.Fatalf("POST amount 1.0000000000000001 = %d, want 400: %s", w.Code, w.Body.String())
	}
	if w := rtCreate(t, ch, `{"amount":"1.0000000000000001"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("POST amount as text = %d, want 400: %s", w.Code, w.Body.String())
	}
	for lit, want := range map[string]int64{"1.0": 1, "1e3": 1000, "-2.50e1": -25, "0.0e5": 0} {
		if w := rtCreate(t, ch, `{"amount":`+lit+`}`); w.Code != http.StatusCreated {
			t.Fatalf("POST amount %s = %d, want 201: %s", lit, w.Code, w.Body.String())
		}
		var got int64
		if err := db.QueryRow(`SELECT amount FROM things ORDER BY id DESC LIMIT 1`).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("amount %s stored %d, want %d", lit, got, want)
		}
	}
}

func TestIntegerText(t *testing.T) {
	for s, want := range map[string]bool{
		"1": true, "1.0": true, "1e3": true, "1.5e1": true, "-0.0": true,
		"0e-999999999999999999999": true, "100e-2": true,
		"1.0000000000000001": false, "1.5": false, "1e-1": false,
		"12e-1": false, "1e-99999999999999999999": false,
	} {
		if got := integerText(s); got != want {
			t.Errorf("integerText(%q) = %v, want %v", s, got, want)
		}
	}
}

// crud stores every Timestamp in one layout; a filter value bound as the
// text sent compared against it as text on SQLite and matched nothing.
func TestTimestampFilterMatchesStored(t *testing.T) {
	ch, _ := roundTripHandler(t)
	checkTimestampFilter(t, ch)
}

func checkTimestampFilter(t *testing.T, ch *CrudHandler) {
	if w := rtCreate(t, ch, `{"occurred_at":"2026-10-27T10:00:00Z"}`); w.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", w.Code, w.Body.String())
	}
	cases := map[string]int{
		"occurred_at=2026-10-27T10:00:00Z":                               1,
		"occurred_at=" + url.QueryEscape("2026-10-27T12:00:00+02:00"):    1,
		"occurred_at=2026-10-27T10:00:00.000Z":                           1,
		"occurred_at=2026-10-27T10:00":                                   1,
		"occurred_at=" + url.QueryEscape("2026-10-27 10:00:00"):          1,
		"occurred_at_gte=2026-10-27T10:00:00Z":                           1,
		"occurred_at_lt=2026-10-27T10:00:00Z":                            0,
		"occurred_at_gt=" + url.QueryEscape("2026-10-27T11:00:00+02:00"): 1,
		"occurred_at_gte=2026-10-27":                                     1,
		"occurred_at_lt=2026-10-27":                                      0,
		"occurred_at_lt=2026-10-28":                                      1,
	}
	for q, want := range cases {
		if got := len(rtList(t, ch, q)); got != want {
			t.Errorf("?%s matched %d rows, want %d", q, got, want)
		}
	}
}

func TestTypedTimestampMatchesStored(t *testing.T) {
	ch, _ := roundTripHandler(t)
	checkTypedTimestamp(t, ch)
}

// A typed condition on a Timestamp column bound its value as given, so on
// SQLite, which compares the column as text, "…10:00:00Z" missed the row
// stored as "…10:00:00.000000Z" that the same ?occurred_at= filter finds.
func checkTypedTimestamp(t *testing.T, ch *CrudHandler) {
	if w := rtCreate(t, ch, `{"occurred_at":"2026-10-27T10:00:00Z"}`); w.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", w.Code, w.Body.String())
	}
	col := entity.NewTimestampColumn("occurred_at")
	at := time.Date(2026, 10, 27, 12, 0, 0, 0, time.FixedZone("", 2*3600))
	cases := []struct {
		name string
		cond entity.Condition
		want int
	}{
		{"eq Z text", col.Eq("2026-10-27T10:00:00Z"), 1},
		{"eq offset text", col.Eq("2026-10-27T12:00:00+02:00"), 1},
		{"eq time.Time", col.Eq(at), 1},
		{"eq *time.Time", col.Eq(&at), 1},
		{"gte Z text", col.Gte("2026-10-27T10:00:00Z"), 1},
		{"lte time.Time", col.Lte(at), 1},
		{"lt Z text", col.Lt("2026-10-27T10:00:00Z"), 0},
		{"gt offset text", col.Gt("2026-10-27T11:00:00+02:00"), 1},
		{"gt time.Time", col.Gt(at), 0},
	}
	for _, c := range cases {
		type row struct {
			ID int `json:"id"`
		}
		n, err := NewTypedQuery[row](ch).Where(c.cond).Count(context.Background())
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if n != c.want {
			t.Errorf("%s matched %d rows, want %d", c.name, n, c.want)
		}
	}
}

// Both drivers scan DATE into a time.Time, which read back as a full
// timestamp that Date validation and the Date filter refuse.
func TestDateReadsBackAsDate(t *testing.T) {
	ch, _ := roundTripHandler(t)
	checkDateRoundTrip(t, ch)
}

func checkDateRoundTrip(t *testing.T, ch *CrudHandler) {
	w := rtCreate(t, ch, `{"day":"2026-10-27"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", w.Code, w.Body.String())
	}
	if got := decodeSingleResponse(t, w.Body.Bytes())["day"]; got != "2026-10-27" {
		t.Errorf("create response day = %v, want 2026-10-27", got)
	}
	row := rtGet(t, ch, "1")
	if row["day"] != "2026-10-27" {
		t.Fatalf("get day = %v, want 2026-10-27", row["day"])
	}
	rows := rtList(t, ch, "day=2026-10-27")
	if len(rows) != 1 || rows[0]["day"] != "2026-10-27" {
		t.Fatalf("list day = %v, want one row with 2026-10-27", rows)
	}
	page := rtList(t, ch, "cursor=&limit=5")
	if len(page) != 1 || page[0]["day"] != "2026-10-27" {
		t.Fatalf("cursor page day = %v", page)
	}
	back, _ := json.Marshal(map[string]any{"day": row["day"]})
	if w := rtUpdate(t, ch, "1", string(back)); w.Code != http.StatusOK {
		t.Fatalf("PATCH the read-back day = %d: %s", w.Code, w.Body.String())
	}
}

func TestTypedDateReadsIntoTimeAndString(t *testing.T) {
	ch, _ := roundTripHandler(t)
	if w := rtCreate(t, ch, `{"day":"2026-10-27"}`); w.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", w.Code, w.Body.String())
	}
	type asTime struct {
		Day time.Time `json:"day"`
	}
	got, err := NewTypedQuery[asTime](ch).First(context.Background())
	if err != nil {
		t.Fatalf("typed First into time.Time: %v", err)
	}
	if want := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC); !got.Day.Equal(want) {
		t.Errorf("time.Time day = %v, want %v", got.Day, want)
	}
	type asPtr struct {
		Day *time.Time `json:"day"`
	}
	ptr, err := NewTypedQuery[asPtr](ch).First(context.Background())
	if err != nil || ptr.Day == nil || ptr.Day.Format(time.DateOnly) != "2026-10-27" {
		t.Fatalf("typed First into *time.Time = %v, %v", ptr, err)
	}
	type asString struct {
		Day string `json:"day"`
	}
	s, err := NewTypedQuery[asString](ch).First(context.Background())
	if err != nil || s.Day != "2026-10-27" {
		t.Fatalf("typed First into string = %+v, %v", s, err)
	}
}

func TestJSONHugeNumberResaves(t *testing.T) {
	ch, _ := roundTripHandler(t)
	checkJSONHugeNumber(t, ch)
}

// A document may hold a number past float64's range: validation takes the
// JSON text, and a read emits the number as stored. The PATCH that sent
// that read back failed decoding with "out of range", a 400 for a value
// the API itself returned.
func checkJSONHugeNumber(t *testing.T, ch *CrudHandler) {
	if w := rtCreate(t, ch, `{"meta":"{\"a\":[1e400]}"}`); w.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", w.Code, w.Body.String())
	}
	rawMeta := func() json.RawMessage {
		t.Helper()
		w := httptest.NewRecorder()
		req := makeRequest(t, RequestOpts{Method: "GET", Path: "/things/1", UserID: "u1"})
		req.SetPathValue("id", "1")
		ch.Get()(w, req)
		var got struct {
			Data struct {
				Meta json.RawMessage `json:"meta"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || w.Code != http.StatusOK {
			t.Fatalf("get = %d, %v: %s", w.Code, err, w.Body.String())
		}
		return got.Data.Meta
	}
	read := rawMeta()
	if w := rtUpdate(t, ch, "1", `{"meta":`+string(read)+`}`); w.Code != http.StatusOK {
		t.Fatalf("PATCH the read-back meta %s = %d: %s", read, w.Code, w.Body.String())
	}
	if again := rawMeta(); string(again) != string(read) {
		t.Fatalf("meta after resave = %s, want %s", again, read)
	}
	// Outside a JSON field such a number is still refused.
	if w := rtUpdate(t, ch, "1", `{"amount":1e400}`); w.Code != http.StatusBadRequest {
		t.Errorf("PATCH amount 1e400 = %d, want 400: %s", w.Code, w.Body.String())
	}
	if w := rtUpdate(t, ch, "1", `{"line_2":1e400}`); w.Code != http.StatusBadRequest {
		t.Errorf("PATCH line_2 1e400 = %d, want 400: %s", w.Code, w.Body.String())
	}
}

func TestTypedDateResavesFromTime(t *testing.T) {
	ch, _ := roundTripHandler(t)
	checkTypedDateResave(t, ch)
}

// A typed read gives a Date in a time.Time as that date's midnight, and
// MarshalEntity spells it in RFC 3339, which Date validation refuses: the
// typed read, MarshalEntity, UpdateOne round trip failed on every row.
func checkTypedDateResave(t *testing.T, ch *CrudHandler) {
	ctx := context.Background()
	if w := rtCreate(t, ch, `{"day":"2026-10-27"}`); w.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", w.Code, w.Body.String())
	}
	type asTime struct {
		Day time.Time `json:"day"`
	}
	got, err := NewTypedQuery[asTime](ch).First(ctx)
	if err != nil {
		t.Fatalf("typed First: %v", err)
	}
	body, err := MarshalEntity(got)
	if err != nil {
		t.Fatalf("MarshalEntity: %v", err)
	}
	if _, err := ch.UpdateOne(ctx, "1", body); err != nil {
		t.Fatalf("UpdateOne(MarshalEntity(typed read)) = %v", err)
	}
	if day := rtGet(t, ch, "1")["day"]; day != "2026-10-27" {
		t.Fatalf("day after resave = %v, want 2026-10-27", day)
	}
	// A midnight in another zone names the date it is in that zone.
	east := time.Date(2026, 10, 28, 0, 0, 0, 0, time.FixedZone("", 5*3600))
	if _, err := ch.UpdateOne(ctx, "1", map[string]any{"day": east}); err != nil {
		t.Fatalf("UpdateOne(time.Time midnight) = %v", err)
	}
	if day := rtGet(t, ch, "1")["day"]; day != "2026-10-28" {
		t.Fatalf("day after a +05:00 midnight = %v, want 2026-10-28", day)
	}
	// A time of day is not a date, and the HTTP body keeps YYYY-MM-DD only.
	if _, err := ch.UpdateOne(ctx, "1", map[string]any{"day": "2026-10-27T10:00:00Z"}); err == nil {
		t.Error("UpdateOne took a Date with a time of day")
	}
	if _, err := ch.UpdateOne(ctx, "1", map[string]any{"day": east.Add(time.Nanosecond)}); err == nil {
		t.Error("UpdateOne took a time.Time a nanosecond past midnight")
	}
	if w := rtUpdate(t, ch, "1", `{"day":"2026-10-27T00:00:00Z"}`); w.Code != http.StatusBadRequest {
		t.Errorf("PATCH an RFC 3339 day = %d, want 400: %s", w.Code, w.Body.String())
	}
}

// A document whose top-level value is a JSON string read back as the Go
// string, which the write path then stored as JSON text: "123" became 123.
func TestJSONStringRoundTrips(t *testing.T) {
	ch, db := roundTripHandler(t)
	checkJSONString(t, ch, db, true)
}

// legacy writes a non-JSON value straight into the column, which a JSONB
// column refuses.
func checkJSONString(t *testing.T, ch *CrudHandler, db *sql.DB, legacy bool) {
	if w := rtCreate(t, ch, `{"meta":"\"123\""}`); w.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", w.Code, w.Body.String())
	}
	row := rtGet(t, ch, "1")
	if row["meta"] != `"123"` {
		t.Fatalf("meta read back %#v, want the JSON text %q", row["meta"], `"123"`)
	}
	back, _ := json.Marshal(map[string]any{"meta": row["meta"]})
	if w := rtUpdate(t, ch, "1", string(back)); w.Code != http.StatusOK {
		t.Fatalf("PATCH the read-back meta = %d: %s", w.Code, w.Body.String())
	}
	var stored string
	if err := db.QueryRow(`SELECT meta FROM things WHERE id = 1`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != `"123"` {
		t.Fatalf("meta stored %q after the round trip, want %q", stored, `"123"`)
	}
	// Containers and nested strings decode as before.
	if w := rtUpdate(t, ch, "1", `{"meta":{"a":"x","n":[1,"y"]}}`); w.Code != http.StatusOK {
		t.Fatalf("PATCH object meta = %d: %s", w.Code, w.Body.String())
	}
	obj, ok := rtGet(t, ch, "1")["meta"].(map[string]any)
	if !ok || obj["a"] != "x" {
		t.Fatalf("object meta read back %#v", rtGet(t, ch, "1")["meta"])
	}
	if !legacy {
		return
	}
	// Legacy text that never was JSON reads back unchanged.
	if _, err := db.Exec(`UPDATE things SET meta = 'plain words' WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	if got := rtGet(t, ch, "1")["meta"]; got != "plain words" {
		t.Fatalf("legacy meta read back %#v", got)
	}
}

// MarshalEntity snake-cases a json tag with casing.ToSnake, which turns
// "line2" into "line2", not the declared column line_2, so a typed
// create dropped the field.
func TestTypedCreateKeepsDigitColumn(t *testing.T) {
	for _, jc := range []JSONCase{CaseSnake, CaseCamel} {
		ch, db := roundTripHandler(t)
		ch = ch.WithJSONCase(jc)
		type input struct {
			Line2 string `json:"line2"`
		}
		body, err := MarshalEntity(input{Line2: "Suite 4"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ch.CreateOne(context.Background(), body); err != nil {
			t.Fatalf("CreateOne: %v", err)
		}
		var got sql.NullString
		if err := db.QueryRow(`SELECT line_2 FROM things WHERE id = 1`).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got.String != "Suite 4" {
			t.Fatalf("case %v: line_2 stored %q, want Suite 4", jc, got.String)
		}
		patch, _ := MarshalEntity(input{Line2: "Suite 5"})
		if _, err := ch.UpdateOne(context.Background(), "1", patch); err != nil {
			t.Fatalf("UpdateOne: %v", err)
		}
		if _, err := ch.BatchCreateMany(context.Background(), []map[string]any{patch}); err != nil {
			t.Fatalf("BatchCreateMany: %v", err)
		}
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM things WHERE line_2 = 'Suite 5'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 2 {
			t.Fatalf("case %v: %d rows hold Suite 5 after UpdateOne + BatchCreateMany, want 2", jc, n)
		}
	}
}

// Two spellings of one column in an in-process body are refused, the way
// the HTTP body refuses them, instead of one winning in map order.
func TestInProcessFoldCollisionRefused(t *testing.T) {
	ch, db := roundTripHandler(t)
	_, err := ch.CreateOne(context.Background(), map[string]any{"line_2": "a", "line2": "b"})
	if err == nil {
		t.Fatal("CreateOne with line_2 and line2 succeeded, want a refusal")
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM things`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("rows = %d, %v; want none written", n, err)
	}
}

// An included row and a group value carry a Date the way a list read does.
func TestIncludedAndGroupedDateIsDate(t *testing.T) {
	db := openBooleanReadDB(t)
	for _, stmt := range []string{
		`CREATE TABLE orders (id TEXT PRIMARY KEY)`,
		`CREATE TABLE lines (id TEXT PRIMARY KEY, order_id TEXT, due DATE)`,
		`INSERT INTO orders (id) VALUES ('o1')`,
		`INSERT INTO lines (id, order_id, due) VALUES ('l1', 'o1', '2026-10-27')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	lines := entity.Define("lines", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "order_id", Type: schema.String},
			{Name: "due", Type: schema.Date},
		},
	}.WithTimestamps(false))
	lines.SetDB(db)
	orders := entity.Define("orders", entity.EntityConfig{
		Relations: []entity.Relation{entity.HasMany("lines", "lines", "order_id")},
	}.WithTimestamps(false))
	orders.SetDB(db)
	ch := NewCrudHandler(orders, db)
	ch.Registry = stubRegistry{byName: map[string]*entity.Entity{"orders": orders, "lines": lines}}
	got, err := ch.GetOne(context.Background(), "o1", []string{"lines"})
	if err != nil {
		t.Fatal(err)
	}
	included, _ := got["lines"].([]map[string]any)
	if len(included) != 1 || included[0]["due"] != "2026-10-27" {
		t.Fatalf("included lines = %#v, want due 2026-10-27", got["lines"])
	}
	groups, err := NewCrudHandler(lines, db).GroupCountAll(context.Background(), "due", ListOptions{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].Value != "2026-10-27" {
		t.Fatalf("groups = %#v, want one group 2026-10-27", groups)
	}
}
