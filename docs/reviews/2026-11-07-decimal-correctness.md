# Decimal storage correctness review — November 7, 2026

Independently reproduced at `953ccc08256e825bdc17254c463a5695a27092b6`.
One accepted root cause, P3; no production fixes are included.

## C12 — SQLite exactness verdict disagrees with actual numeric storage

Root: `core/schema/decimal.go:94`–`:98`, consumed by
`framework/crud/decimal_sqlite.go:42`. `DecimalExactOnSQLite` uses at most
18 integer digits or at most 15 literal significant digits as storage
proxies. Neither proxy checks the actual signed-int64 boundary or floating
point underflow. Both symptom directions count once, not as separate bugs.

The public utility at `core/schema/decimal.go:78` promises whole numbers
fitting 64 bits are stored as integers. The supported Decimal RawType rules
at `framework/docs/content/entity-declarations.md:698` and `:711` promise
refusal when the column cannot store a value as sent; custom NUMERIC(p,s)
sets precision/scale, while bare NUMERIC bounds the exponent alone.

### Independent public-handler observations

The coordinator created a fresh in-memory SQLite database using the repo's
`sqlite/stdlib` driver, declared Decimal fields with NUMERIC(30,0), NUMERIC
and TEXT RawTypes, and exercised real CRUD POST/GET handlers via httptest.
The probe is independently authored, not the reviewer's overlay. It also
binds the same integer strings directly into SQLite as storage controls.

| Value and column | Schema/storage control | Actual CRUD result |
| --- | --- | --- |
| `1234567890123456789`, NUMERIC(30,0) | schema accepts; direct SQLite stores identical INTEGER | 400 `decimal_significant_digits` |
| `9223372036854775807`, NUMERIC(30,0) | schema accepts; exact INTEGER | same false refusal |
| `-9223372036854775808`, NUMERIC(30,0) | schema accepts; exact INTEGER | same false refusal |
| `123456789012345678`, NUMERIC(30,0) | 18-digit boundary control | 201; GET returns same number |
| `1000000000000000000`, NUMERIC(30,0) | 19 digits but only one significant digit | 201; GET returns same number |
| `1e-400`, NUMERIC | nonzero, exponent inside supported window | 201; GET returns `"0"`; SQLite INTEGER 0 |
| `0.0000000000000000001`, NUMERIC | ordinary small-value control | 201; GET preserves decimal value |
| `1e-400`, TEXT | text-affinity control | 201; GET preserves `"1e-400"` |

The first direction unnecessarily refuses values SQLite demonstrably stores
exactly. The second silently changes a nonzero value into zero despite the
write's 201 response. Both require a supported custom Decimal RawType; the
default DECIMAL(19,4) precision/scale check rejects these examples earlier.
No live Postgres behavior is claimed. The reviewer's additional subnormal
`1e-320` example was not independently rerun and is not needed for acceptance.

The existing `core/schema/decimal_sqlite_test.go:19` asserts false for the
19-digit example. That expectation contradicts the same test's stated
64-bit model and the measured SQLite INTEGER result; a green existing suite
therefore does not resolve the contract discrepancy.

## Retained independent observation test

The assertions intentionally pin the currently observed discrepancy. Invert
them when writing desired-behavior regression coverage for a future fix.
Extract this block to a temporary test file and overlay it onto a virtual
`framework/crud/zz_coordinator_decimal_observation_test.go` in the checkout.
Run from the repository root:

```sh
go test -p 2 -count=1 -overlay /tmp/gofastr-coordinator-decimal/overlay.json \
  -run '^TestCoordinatorDecimalStorageObservation$' -v ./framework/crud
```

The current temporary source, overlay and execution log are under
`/tmp/gofastr-coordinator-decimal/`. This document retains the source so the
observation can be reconstructed after temporary artifacts disappear.

Validation: the independent observation test passes with the measured bug
expectations and all controls. Fresh full `core/schema`, `framework/crud`
and `framework/entityui` suites also pass at this revision; no live Postgres
run or full-browser validation was performed.

```go
package crud_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

func TestCoordinatorDecimalStorageObservation(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	for _, statement := range []string{
		`CREATE TABLE decimal_probe (id TEXT PRIMARY KEY, units NUMERIC(30,0), tiny NUMERIC, memo TEXT)`,
		`CREATE TABLE storage_truth (value NUMERIC)`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	fields := []schema.Field{
		{Name: "units", Type: schema.Decimal, RawType: "NUMERIC(30,0)"},
		{Name: "tiny", Type: schema.Decimal, RawType: "NUMERIC"},
		{Name: "memo", Type: schema.Decimal, RawType: "TEXT"},
	}
	definition := entity.Define("decimal_probe", entity.EntityConfig{
		Name: "decimal_probe", Table: "decimal_probe", Fields: fields,
	}.WithTimestamps(false))
	definition.SetDB(database)
	routes := router.New()
	crud.RegisterCrudRoutes(routes, crud.NewCrudHandler(definition, database), "/decimal_probe")
	send := func(method, target, body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, target, strings.NewReader(body))
		request = request.WithContext(handler.SetUser(request.Context(), "probe-user"))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		routes.ServeHTTP(response, request)
		return response
	}
	decode := func(response *httptest.ResponseRecorder) map[string]any {
		t.Helper()
		var envelope struct {
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Data == nil {
			t.Fatalf("missing data: %s", response.Body.String())
		}
		return envelope.Data
	}
	for _, value := range []string{"1234567890123456789", "9223372036854775807", "-9223372036854775808"} {
		if err := schema.Validate(fields[0], value); err != nil {
			t.Fatal(err)
		}
		if _, err := database.Exec(`DELETE FROM storage_truth`); err != nil {
			t.Fatal(err)
		}
		if _, err := database.Exec(`INSERT INTO storage_truth(value) VALUES (?)`, value); err != nil {
			t.Fatal(err)
		}
		var kind, stored string
		if err := database.QueryRow(`SELECT typeof(value), CAST(value AS TEXT) FROM storage_truth`).Scan(&kind, &stored); err != nil {
			t.Fatal(err)
		}
		if kind != "integer" || stored != value {
			t.Fatalf("SQLite truth %s: %s %s", value, kind, stored)
		}
		response := send(http.MethodPost, "/decimal_probe", fmt.Sprintf(`{"units":%q}`, value))
		t.Logf("19-digit %s: SQLite=%s/%s API=%d %s", value, kind, stored, response.Code, strings.TrimSpace(response.Body.String()))
		if response.Code != 400 || !strings.Contains(response.Body.String(), "decimal_significant_digits") {
			t.Fatal("expected observed false refusal")
		}
	}
	for _, sample := range []struct{ field, value, expected string }{
		{"units", "123456789012345678", "123456789012345678"},
		{"units", "1000000000000000000", "1000000000000000000"},
		{"tiny", "1e-400", "0"},
		{"tiny", "0.0000000000000000001", "0.0000000000000000001"},
		{"memo", "1e-400", "1e-400"},
	} {
		response := send(http.MethodPost, "/decimal_probe", fmt.Sprintf(`{%q:%q}`, sample.field, sample.value))
		if response.Code != 201 {
			t.Fatalf("create %s: %d %s", sample.value, response.Code, response.Body.String())
		}
		row := decode(response)
		read := send(http.MethodGet, "/decimal_probe/"+fmt.Sprint(row["id"]), "")
		if read.Code != 200 {
			t.Fatalf("GET: %d %s", read.Code, read.Body.String())
		}
		loaded := decode(read)
		if fmt.Sprint(loaded[sample.field]) != sample.expected {
			t.Fatalf("GET %s = %v; want %s", sample.value, loaded[sample.field], sample.expected)
		}
		var kind, stored string
		query := fmt.Sprintf(`SELECT typeof(%s), CAST(%s AS TEXT) FROM decimal_probe WHERE id = ?`, sample.field, sample.field)
		if err := database.QueryRow(query, row["id"]).Scan(&kind, &stored); err != nil {
			t.Fatal(err)
		}
		t.Logf("%s %s: POST=%d GET=%v SQLite=%s/%s", sample.field, sample.value, response.Code, loaded[sample.field], kind, stored)
		if sample.value == "1e-400" && sample.field == "tiny" && (kind != "integer" || stored != "0") {
			t.Fatal("expected observed underflow")
		}
	}
}

```
