package crud

import (
	"net/http"
	"net/url"
	"slices"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// singleValueQueryKeys are the control keys read as one value, sorted.
var singleValueQueryKeys = [...]string{
	"cursor", "direction", "fields", "include", "limit", "offset",
	"page", "per_page", "q", "stream", "trashed", "where",
}

// readQuery parses the wire query once for every CRUD HTTP read.
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
// AllowedFilterParams, there are no SearchFields, and no filterable column is named q.
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

// softDeleteQuery is readQuery for helpers that cannot return an error: invalid input yields nil.
func softDeleteQuery(r *http.Request) url.Values {
	q, err := filter.ParseQueryStrict(r.URL.RawQuery)
	if err != nil || refuseRepeatedKey(q, "trashed") != nil {
		return nil
	}
	return q
}
