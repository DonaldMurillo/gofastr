package framework

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"net/http"
	"strconv"
)

// DefaultPageSize is the default number of items per page.
const DefaultPageSize = 25

// MaxPageSize is the maximum allowed page size.
const MaxPageSize = 100

// CursorPage represents a page of results using cursor-based pagination.
type CursorPage struct {
	Data    []map[string]any `json:"data"`
	Cursor  string           `json:"cursor"`
	HasMore bool             `json:"hasMore"`
	Total   int              `json:"total,omitempty"`
}

// OffsetPage represents a page of results using offset-based pagination.
type OffsetPage struct {
	Data       []map[string]any `json:"data"`
	Page       int              `json:"page"`
	PageSize   int              `json:"pageSize"`
	Total      int              `json:"total"`
	TotalPages int              `json:"totalPages"`
}

// cursorToken is the internal structure encoded into a cursor string.
type cursorToken struct {
	Field string `json:"f"`
	Value string `json:"v"`
}

// ParsePagination extracts cursor, limit, and offset from query parameters.
// If cursor is present, offset will be 0 (cursor takes precedence).
func ParsePagination(r *http.Request) (cursor string, limit int, offset int) {
	cursor = r.URL.Query().Get("cursor")
	limit = parseIntDefault(r, "limit", DefaultPageSize)
	if limit < 1 {
		limit = DefaultPageSize
	}
	if limit > MaxPageSize {
		limit = MaxPageSize
	}

	if cursor != "" {
		return cursor, limit, 0
	}

	page := parseIntDefault(r, "page", 1)
	if page < 1 {
		page = 1
	}
	offset = (page - 1) * limit
	return cursor, limit, offset
}

// ParseCursorPagination extracts cursor, limit, and direction from query parameters.
// Direction defaults to "forward"; can be set via ?direction=backward.
func ParseCursorPagination(r *http.Request) (cursor string, limit int, direction string) {
	cursor = r.URL.Query().Get("cursor")
	limit = parseIntDefault(r, "limit", DefaultPageSize)
	if limit < 1 {
		limit = DefaultPageSize
	}
	if limit > MaxPageSize {
		limit = MaxPageSize
	}

	direction = r.URL.Query().Get("direction")
	if direction == "" {
		direction = "forward"
	}
	return cursor, limit, direction
}

// EncodeCursor creates a base64-encoded opaque cursor from a field name and value.
func EncodeCursor(field string, value any) string {
	token := cursorToken{
		Field: field,
		Value: toString(value),
	}
	b, _ := json.Marshal(token)
	return base64.StdEncoding.EncodeToString(b)
}

// DecodeCursor decodes a base64 cursor string into its field and value components.
func DecodeCursor(cursor string) (field string, value string, err error) {
	b, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return "", "", err
	}
	var token cursorToken
	if err := json.Unmarshal(b, &token); err != nil {
		return "", "", err
	}
	return token.Field, token.Value, nil
}

// NewCursorPage builds a CursorPage from data. It fetches limit+1 rows to
// determine HasMore, and encodes the next cursor from the last row's cursorField.
func NewCursorPage(data []map[string]any, cursorField string, limit int) CursorPage {
	hasMore := len(data) > limit
	if hasMore {
		data = data[:limit]
	}

	var cursor string
	if hasMore && len(data) > 0 {
		last := data[len(data)-1]
		if val, ok := last[cursorField]; ok {
			cursor = EncodeCursor(cursorField, val)
		}
	}

	return CursorPage{
		Data:    data,
		Cursor:  cursor,
		HasMore: hasMore,
	}
}

// NewOffsetPage builds an OffsetPage with computed TotalPages from total and pageSize.
func NewOffsetPage(data []map[string]any, page, pageSize, total int) OffsetPage {
	totalPages := 0
	if pageSize > 0 {
		totalPages = int(math.Ceil(float64(total) / float64(pageSize)))
	}
	return OffsetPage{
		Data:       data,
		Page:       page,
		PageSize:   pageSize,
		Total:      total,
		TotalPages: totalPages,
	}
}

// parseIntDefault parses an integer query parameter with a default fallback.
func parseIntDefault(r *http.Request, key string, def int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// toString converts any value to its string representation for cursor encoding.
func toString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	default:
		b, _ := json.Marshal(val)
		return string(b)
	}
}
