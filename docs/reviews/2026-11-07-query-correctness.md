# Query correctness review — November 7, 2026

Reviewed revision: `953ccc08256e825bdc17254c463a5695a27092b6`.
This records an independently reproduced compatibility issue, not a fix or
a release-readiness assessment.

## Confirmed — P2: legacy SQLite timestamp spellings break cursor continuity

Locations: `framework/filter/typed.go:106`,
`framework/crud/crud_cursor.go:130`, `framework/crud/crud_cursor.go:390`, and
`framework/pagination/pagination.go:444`.

An unmanaged entity over an existing SQLite table can return timestamp
strings written with a space separator or RFC 3339 without fixed fractional
digits. Listing and cursor encoding accept these values. However, cursor
rebinding converts the anchor to `schema.TimestampLayout`, while the query
compares it against the original stored text. The anchor consequently no
longer has the same position in the column's sort order.

Normal public call path: an `entity.EntityConfig` with `Unmanaged: true`,
`schema.Timestamp`, and cursor fields `at,id` →
`crud.NewCrudHandler(...).List()` → successive `?limit=2&cursor=` requests.
Unmanaged tables are a documented extension point for existing tables; see
`framework/docs/content/migrations.md:1143`.

Independent results for five rows, using both `TEXT` and `DATETIME` columns:

- Canonical stored timestamps: `m1,m2 | m3,m4 | m5`.
- Space-separated timestamps: `m1,m2 | m5`; the walk omits `m3,m4`.
- RFC 3339 timestamps without fractional digits:
  `m1,m2 | m2,m3 | m3,m4 | m4,m5`; each subsequent page repeats a row.
- A range filter strictly after the second space-separated timestamp returns
  only `m5`, instead of `m3,m4,m5`.
- The same filter over the RFC 3339 rows includes the anchor `m2` despite
  using `gt`. Canonical controls return `m3,m4,m5` as expected.

Rows are not deleted from storage; records disappear from or repeat in the
API traversal. The direct sorted-list control returns all five stored rows.

### Scope and contract boundary

This is conditional on noncanonical stored data, such as a legacy table or
direct SQL writer. It is not a demonstrated failure for data written in
the canonical layout by framework CRUD. `core/schema/timestamp.go` explicitly
defines that canonical layout, and normalizing a filter parameter is correct
for canonical columns. Do not remove that normalization globally as a fix.

Decide how unmanaged Timestamp columns participate in this storage contract:
normalize existing data or expose a normalized view, support a consistent
comparison representation, or explicitly validate and refuse unsupported
storage layouts rather than returning a cursor that skips or repeats rows.
The reproduction establishes the behavior on SQLite only; Postgres behavior
was not independently executed and is not claimed here.

### Retained reproduction

From the repository root, save the following block to a temporary `main.go`
and run `go run /absolute/path/to/main.go`. It uses an in-memory database
and invokes the actual CRUD HTTP handler through `httptest`, without a
network listener or external database. Every case includes both a full
sorted-list control and a cursor walk, with explicit assertions that the
canonical controls pass and the reported discrepancies reproduce.

```go
package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

type page struct {
	Data    []struct{ ID string }
	Cursor  string
	HasMore bool
}

func require(err error) {
	if err != nil {
		panic(err)
	}
}

func readPage(handler *crud.CrudHandler, query string) page {
	request := httptest.NewRequest(http.MethodGet, "/logs?"+query, nil)
	response := httptest.NewRecorder()
	handler.List()(response, request)
	if response.Code != http.StatusOK {
		panic(response.Body.String())
	}
	var result page
	require(json.Unmarshal(response.Body.Bytes(), &result))
	return result
}

func identifiers(result page) []string {
	var output []string
	for _, row := range result.Data {
		output = append(output, row.ID)
	}
	return output
}

func runScenario(name, columnType string, timestamps []string) {
	database, err := sql.Open("sqlite3", ":memory:")
	require(err)
	defer database.Close()
	database.SetMaxOpenConns(1)
	_, err = database.Exec("CREATE TABLE logs (id TEXT PRIMARY KEY, at " + columnType + " NOT NULL)")
	require(err)
	for index, timestamp := range timestamps {
		_, err = database.Exec("INSERT INTO logs (id, at) VALUES (?, ?)", fmt.Sprintf("m%d", index+1), timestamp)
		require(err)
	}
	definition := entity.Define("logs", entity.EntityConfig{
		Table: "logs", Unmanaged: true,
		Fields:     []schema.Field{{Name: "at", Type: schema.Timestamp, Required: true}},
		Exposure:   &entity.ExposureConfig{Public: true},
		Pagination: &entity.PaginationConfig{CursorFields: []string{"at", "id"}},
	}.WithTimestamps(false))
	definition.SetDB(database)
	handler := crud.NewCrudHandler(definition, database).WithJSONCase(crud.CaseSnake)
	baseline := identifiers(readPage(handler, "limit=10&sort=at"))
	var pages []string
	var seen []string
	cursor := ""
	for range 8 {
		result := readPage(handler, "limit=2&cursor="+url.QueryEscape(cursor))
		current := identifiers(result)
		pages = append(pages, strings.Join(current, ","))
		seen = append(seen, current...)
		if !result.HasMore {
			break
		}
		if result.Cursor == "" {
			panic("Missing cursor on unfinished page")
		}
		cursor = result.Cursor
	}
	filterIDs := identifiers(readPage(handler, "limit=10&sort=at&at_gt="+url.QueryEscape(timestamps[1])))
	wrong := !slices.Equal(baseline, seen)
	filterWrong := !slices.Equal(filterIDs, baseline[2:])
	fmt.Printf("%s/%s baseline=%v pages=%s cursor_wrong=%v range_ids=%v range_wrong=%v\n", name, columnType, baseline, strings.Join(pages, " | "), wrong, filterIDs, filterWrong)
	if name == "canonical" && (wrong || filterWrong) {
		panic("Canonical control failed")
	}
	if name != "canonical" && (!wrong || !filterWrong) {
		panic("Reported failures did not reproduce")
	}
}

func main() {
	space := []string{"2026-03-03 08:00:00", "2026-03-03 09:00:00", "2026-03-03 10:00:00", "2026-03-03 11:00:00", "2026-03-04 08:00:00"}
	rfc := []string{"2026-01-01T12:00:00Z", "2026-01-02T12:00:00Z", "2026-01-03T12:00:00Z", "2026-01-04T12:00:00Z", "2026-01-05T12:00:00Z"}
	var canonical []string
	for _, value := range space {
		parsed, err := time.Parse(time.DateTime, value)
		require(err)
		canonical = append(canonical, schema.FormatTimestamp(parsed))
	}
	for _, columnType := range []string{"TEXT", "DATETIME"} {
		runScenario("canonical", columnType, canonical)
		runScenario("space", columnType, space)
		runScenario("rfc3339", columnType, rfc)
	}
}
```

The reproduction's assertions confirm the present discrepancy. They are
not expectations to preserve when adding a regression test for a fix.

## Independently run checks

- `go test ./framework/pagination/ ./framework/filter/ ./core/query/ ./framework/crud/ -count=1`
- `go vet ./framework/pagination/ ./framework/filter/ ./core/query/ ./framework/crud/`

Existing tests cover canonical timestamp binding and remain green even
while the legacy-table discrepancy reproduces. No production implementation
was changed by this review.
