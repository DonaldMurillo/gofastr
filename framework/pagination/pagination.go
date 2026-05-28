package pagination

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"strings"
)

// stripControls removes bytes that have caused cursor / direction injection
// problems in the past: NUL, CR, LF, and (defensively) the rest of the
// C0 control range plus DEL. Applied to any user-controlled cursor token
// field after decoding and to cursor direction strings before they reach
// downstream consumers.
func stripControls(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c == 0x7f {
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

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
	// Guard against integer overflow: a malicious caller can request a
	// huge page number (e.g. math.MaxInt) which wraps to a negative
	// offset when multiplied by limit. Negative offsets are undefined
	// in most SQL dialects and can yield wraparound pagination bypass.
	offset = (page - 1) * limit
	if offset < 0 || page > math.MaxInt/limit+1 {
		offset = 0
	}
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

	direction = stripControls(r.URL.Query().Get("direction"))
	// Only "forward" and "backward" are meaningful; anything else
	// (including the empty string or a CRLF-smuggled header injection
	// payload) falls back to "forward" so downstream consumers can
	// trust the value as a static label.
	if direction != "forward" && direction != "backward" {
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
	// Cursors are opaque to the caller but their contents flow back into
	// SQL identifiers (Field → ORDER BY column) and predicate values.
	// Strip control bytes so a tampered cursor can't poison ORDER/WHERE
	// clauses, log lines, or metrics labels.
	return stripControls(token.Field), stripControls(token.Value), nil
}

// multiCursorToken is the wire shape for cursors that keyset on multiple
// fields. Each entry pairs the column name with the last row's value as a
// string so tuple-comparison reconstructs the WHERE clause deterministically.
type multiCursorToken struct {
	Fields []multiCursorField `json:"f"`
}

type multiCursorField struct {
	Name  string `json:"n"`
	Value string `json:"v"`
}

// EncodeMultiCursor builds an opaque cursor from an ordered list of
// (column, value) pairs. Used for composite cursor pagination — ORDER BY
// composes the fields in the same order, and the WHERE clause becomes a
// tuple comparison "(c1, c2, …) > ($1, $2, …)".
func EncodeMultiCursor(fields []string, row map[string]any) string {
	tok := multiCursorToken{Fields: make([]multiCursorField, 0, len(fields))}
	for _, f := range fields {
		v, ok := row[f]
		if !ok {
			continue
		}
		tok.Fields = append(tok.Fields, multiCursorField{Name: f, Value: toString(v)})
	}
	b, _ := json.Marshal(tok)
	return base64.StdEncoding.EncodeToString(b)
}

// DecodeMultiCursor returns the ordered list of (column, value) pairs the
// cursor encoded. Returns the empty slice + an error if the cursor doesn't
// match the expected shape.
func DecodeMultiCursor(cursor string) ([]multiCursorField, error) {
	b, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return nil, err
	}
	var tok multiCursorToken
	if err := json.Unmarshal(b, &tok); err != nil {
		return nil, err
	}
	// Same control-byte scrub as DecodeCursor — multi-column cursors
	// feed both column names (ORDER BY) and values (WHERE tuple comparison).
	for i := range tok.Fields {
		tok.Fields[i].Name = stripControls(tok.Fields[i].Name)
		tok.Fields[i].Value = stripControls(tok.Fields[i].Value)
	}
	return tok.Fields, nil
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
