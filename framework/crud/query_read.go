package crud

import (
	"net/http"
	"net/url"
	"slices"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// singleValueQueryKeys classifies the control keys read as one value. Keep
// sorted so a query with several repeated keys always refuses the same one.
var singleValueQueryKeys = [...]string{
	"cursor", "direction", "fields", "include", "limit", "offset",
	"page", "per_page", "q", "stream", "trashed", "where",
}

// readQuery is the entry gate for every CRUD HTTP read (list offset, cursor
// and stream pages, a direct ServeStreamingList call, read-one). Helpers
// consume its values and never inspect the wire query again;
// TestReadQueryGuard holds that.
//
// Strictly: URL.Query drops a pair that does not decode (%ZZ, a bare %, a
// ';'), so ?cursor=%ZZ restarted a walk at page 1, ?status=paid%ZZ read as
// no filter and ?trashed=true%ZZ hid the row asked for. Once: each helper
// used to re-parse the request leniently, and the cursor page parsed it up
// to eight times, so a gate on one parse said nothing about the others.
// A single-value key sent twice is refused because every reader takes the
// first value: ?fields=title&fields=bogus answered 200 where ?fields=bogus
// answered 400. sort reads every value and may repeat.
func (ch *CrudHandler) readQuery(r *http.Request) (url.Values, error) {
	q, err := filter.ParseQueryStrict(r.URL.RawQuery)
	if err != nil {
		return nil, err
	}
	for _, key := range singleValueQueryKeys {
		if key == "q" && ch.hostOwnsQParam() {
			continue
		}
		if err := refuseRepeatedKey(q, key); err != nil {
			return nil, err
		}
	}
	return q, nil
}

// hostOwnsQParam reports whether ?q= belongs to the host: q is in
// AllowedFilterParams, the entity declares no SearchFields, and no column
// the client can filter is named q. A BeforeList hook reads every value
// itself, so a repeat is the host's to judge. A column named q is the
// framework's filter and is refused at the gate like any control, on
// read-one too, where no filter parser would see it.
func (ch *CrudHandler) hostOwnsQParam() bool {
	if len(ch.Entity.Config.SearchFields) > 0 || !slices.Contains(ch.Entity.Config.AllowedFilterParams, "q") {
		return false
	}
	for _, f := range ch.queryFields() {
		if !f.Hidden && !f.NoQuery && (f.Name == "q" || f.WireName == "q") {
			return false
		}
	}
	return true
}

// refuseRepeatedKey refuses a control whose later values would be ignored.
func refuseRepeatedKey(q url.Values, key string) error {
	if n := len(q[key]); n > 1 {
		if key == "include" {
			return i18n.NewError(reasonIncludeRepeated, map[string]any{"count": n})
		}
		return i18n.NewError(reasonQueryKeyRepeated, map[string]any{"key": strconv.Quote(key), "count": n})
	}
	return nil
}

// softDeleteQuery serves exported helpers used by hosts and write paths.
// They cannot return a parse error, so invalid input grants no visibility.
func softDeleteQuery(r *http.Request) url.Values {
	q, err := filter.ParseQueryStrict(r.URL.RawQuery)
	if err != nil || refuseRepeatedKey(q, "trashed") != nil {
		return nil
	}
	return q
}
