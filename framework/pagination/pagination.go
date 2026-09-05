package pagination

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
)

const (
	// maxCursorEncodedSize caps the base64 text a cursor decoder will
	// look at, mirroring dsl.maxDSLInputSize. The cursor is
	// client-borne (?cursor= rides the query string, capped only by
	// net/http's ~1 MB header limit), and decode cost used to be
	// proportional to whatever the client chose: a megabyte-class
	// cursor allocated its full field set on every list route before
	// any validation refused it. A server-minted cursor for any real
	// keyset is a few hundred bytes.
	maxCursorEncodedSize = 16 * 1024

	// maxCursorFields caps how many keyset columns one cursor may
	// carry. CursorFields is developer-declared (typically 1-3
	// columns, the PK appended automatically), so 64 is two orders
	// past any legitimate composite while bounding the per-element
	// allocation (struct + two strings + control scrub per name) to a
	// fixed cost.
	maxCursorFields = 64
)

// stripControls removes bytes / codepoints that have caused cursor /
// direction injection problems in the past: NUL, CR, LF, and the rest
// of the C0 control range plus DEL, plus Unicode zero-width and bidi
// formatting codepoints. The bidi/zero-width chars are particularly
// dangerous in cursor *field names*, because a parser that sees "name"
// and a downstream allow-list that sees "na​me" will disagree.
// Applied to cursor FIELD names after decoding and to cursor direction
// strings before they reach downstream consumers, never to cursor
// VALUES, which are bound SQL args and must round-trip byte-for-byte
// (see DecodeCursor).
func stripControls(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			continue
		}
		if isUnicodeInvisible(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// isUnicodeInvisible reports whether r is a zero-width or bidi-control
// codepoint that should never survive a cursor decode. Combining marks
// and ordinary diacritics deliberately fall through, we only strip
// the codepoints that have no visible glyph and exist purely to
// rearrange or hide the surrounding text.
func isUnicodeInvisible(r rune) bool {
	switch r {
	case 0x200B, 0x200C, 0x200D, // zero-width space/non-joiner/joiner
		0x200E, 0x200F, // LRM / RLM
		0x202A, 0x202B, 0x202C, 0x202D, 0x202E, // LRE/RLE/PDF/LRO/RLO
		0x2066, 0x2067, 0x2068, 0x2069, // LRI/RLI/FSI/PDI
		0x061C,                         // Arabic letter mark
		0x180E,                         // Mongolian vowel separator
		0xFEFF,                         // BOM / zero-width no-break space
		0x2061, 0x2062, 0x2063, 0x2064, // invisible math operators
		0x202F: // narrow no-break space
		return true
	}
	return false
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

	page := max(parseIntDefault(r, "page", 1), 1)
	offset = OffsetForPage(page, limit)
	return cursor, limit, offset
}

// OffsetForPage returns the row offset for a 1-based page number and a
// page size, with the integer-overflow guard applied. A caller can
// request a huge page number (e.g. math.MaxInt) whose (page-1)*limit
// product wraps: to a negative offset, undefined in most SQL dialects
// (Postgres rejects it outright) and treated as 0 by SQLite, or, for
// carefully chosen values, to a small POSITIVE offset that silently
// serves the wrong window. page>=1 and limit>=1 required, so
// (page-1)*limit overflows iff (page-1) > math.MaxInt/limit; compute
// the threshold without the `+1` that previously wrapped to
// math.MinInt when limit==1. Overflow clamps to 0 (the first window),
// matching ParsePagination's historical behaviour. Every offset-math
// call site (buffered list, streaming list, admin table) must go
// through this, never multiply a client-supplied page by hand.
func OffsetForPage(page, limit int) int {
	// limit < 1 guards the MaxInt/limit division below (limit == 0 is an
	// integer-division panic; negative limit is nonsense arithmetic).
	// OffsetForPage is exported, so it protects its own inputs even though
	// every in-repo caller floors its page size. ServeStreamingList, for
	// one, is also exported and takes an arbitrary limit.
	if page < 2 || limit < 1 {
		return 0
	}
	if page-1 > math.MaxInt/limit {
		return 0
	}
	return (page - 1) * limit
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
//
// The field name is stripped of control / invisible codepoints: it flows
// into SQL identifiers (ORDER BY) and allow-lists, where a smuggled
// zero-width or bidi codepoint makes a parser and a downstream
// allow-list disagree. The VALUE is returned verbatim, it is compared
// against the database as a bound arg, never interpolated, so stripping
// it would not harden anything while breaking the keyset contract: a
// sort key containing e.g. U+200B must round-trip losslessly or paging
// resumes before that row and re-serves it.
func DecodeCursor(cursor string) (field string, value string, err error) {
	if len(cursor) > maxCursorEncodedSize {
		return "", "", fmt.Errorf("pagination: cursor exceeds %d bytes", maxCursorEncodedSize)
	}
	b, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return "", "", err
	}
	var token cursorToken
	if err := json.Unmarshal(b, &token); err != nil {
		return "", "", err
	}
	return stripControls(token.Field), token.Value, nil
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
// (column, value) pairs. Used for composite cursor pagination. ORDER BY
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
	if len(cursor) > maxCursorEncodedSize {
		return nil, fmt.Errorf("pagination: cursor exceeds %d bytes", maxCursorEncodedSize)
	}
	b, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return nil, err
	}
	var tok multiCursorToken
	if err := json.Unmarshal(b, &tok); err != nil {
		return nil, err
	}
	if len(tok.Fields) > maxCursorFields {
		return nil, fmt.Errorf("pagination: cursor carries %d fields (max %d)", len(tok.Fields), maxCursorFields)
	}
	// Names reach ORDER BY / allow-lists, so they get the same
	// control-byte scrub as DecodeCursor. Values are bound SQL args,
	// kept verbatim so the tuple comparison resumes at the exact row
	// (see DecodeCursor for the round-trip contract).
	for i := range tok.Fields {
		tok.Fields[i].Name = stripControls(tok.Fields[i].Name)
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
