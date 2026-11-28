package pagination

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

const (
	maxCursorEncodedSize = 16 * 1024

	maxCursorFields = 64
)

func stripControls(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r < 0x20 || r == 0x7f || textsafe.IsInvisible(r) {
			continue
		}
		b.WriteRune(r)
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

type cursorToken struct {
	Field string `json:"f"`
	Value string `json:"v"`
}

// ParsePagination reads cursor, a limit clamped to 1..MaxPageSize, and
// the page's offset, which is 0 when a cursor is sent.
func ParsePagination(r *http.Request) (cursor string, limit int, offset int) {
	q := r.URL.Query()
	cursor = q.Get("cursor")
	limit = ParseLimit(q.Get("limit"), DefaultPageSize, MaxPageSize)

	if cursor != "" {
		return cursor, limit, 0
	}

	page := ParsePositiveInt(q.Get("page"), 1)
	offset = OffsetForPage(page, limit)
	return cursor, limit, offset
}

// OffsetForPage returns (page-1)*limit for a 1-based page, or 0 when
// page < 2, limit < 1 or the product overflows.
func OffsetForPage(page, limit int) int {
	if page < 2 || limit < 1 {
		return 0
	}
	if page-1 > math.MaxInt/limit {
		return 0
	}
	return (page - 1) * limit
}

// ParseCursorPagination reads cursor, a clamped limit, and a direction
// of "forward" or "backward", anything else read as "forward".
func ParseCursorPagination(r *http.Request) (cursor string, limit int, direction string) {
	q := r.URL.Query()
	cursor = q.Get("cursor")
	limit = ParseLimit(q.Get("limit"), DefaultPageSize, MaxPageSize)
	direction = stripControls(q.Get("direction"))

	if direction != "forward" && direction != "backward" {
		direction = "forward"
	}
	return cursor, limit, direction
}

// EncodeCursor returns the opaque cursor for one keyset field and value.
func EncodeCursor(field string, value any) string {
	token := cursorToken{
		Field: field,
		Value: toString(value),
	}
	b, _ := json.Marshal(token)
	return base64.StdEncoding.EncodeToString(b)
}

// CursorFits reports whether cursor is within the decoders' caps.
func CursorFits(cursor string) bool {
	if len(cursor) > maxCursorEncodedSize {
		return false
	}
	b, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return true
	}
	var tok multiCursorToken
	if json.Unmarshal(b, &tok) != nil {
		return true
	}
	return len(tok.Fields) > 0 && len(tok.Fields) <= maxCursorFields
}

// DecodeCursor returns a cursor's field, control and invisible codepoints
// stripped, and its value verbatim; a bad or oversize token errors.
func DecodeCursor(cursor string) (field string, value string, err error) {
	if len(cursor) > maxCursorEncodedSize {
		return "", "", i18n.NewError(reasonCursorTooLong, map[string]any{"max": maxCursorEncodedSize})
	}
	b, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return "", "", err
	}
	if err := handler.CheckObjectKeys(b, handler.FoldJSONKey); err != nil {
		return "", "", cursorTokenError(err)
	}
	var token cursorToken
	if err := json.Unmarshal(b, &token); err != nil {
		return "", "", cursorTokenError(err)
	}
	return stripControls(token.Field), token.Value, nil
}

func cursorTokenError(err error) error {
	return &i18n.Error{Reason: reasonCursorToken, Params: map[string]any{"detail": handler.JSONErrorDetail(err)}, Err: err}
}

type multiCursorToken struct {
	Fields []multiCursorField `json:"f"`
}

type multiCursorField struct {
	Name  string `json:"n"`
	Value string `json:"v"`
}

// EncodeMultiCursor returns the opaque cursor for fields' values in row.
func EncodeMultiCursor(fields []string, row map[string]any) string {
	tok := multiCursorToken{Fields: make([]multiCursorField, 0, len(fields))}
	for _, f := range fields {
		v, ok := row[f]
		if !ok || isNullKey(v) {
			tok.Fields = tok.Fields[:0]
			break
		}
		tok.Fields = append(tok.Fields, multiCursorField{Name: f, Value: toString(v)})
	}
	b, _ := json.Marshal(tok)
	return base64.StdEncoding.EncodeToString(b)
}

// DecodeMultiCursor is DecodeCursor for a composite cursor of up to 64 fields.
func DecodeMultiCursor(cursor string) ([]multiCursorField, error) {
	if len(cursor) > maxCursorEncodedSize {
		return nil, i18n.NewError(reasonCursorTooLong, map[string]any{"max": maxCursorEncodedSize})
	}
	b, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return nil, err
	}

	if err := handler.CheckObjectKeys(b, handler.FoldJSONKey); err != nil {
		return nil, cursorTokenError(err)
	}
	var tok multiCursorToken
	if err := json.Unmarshal(b, &tok); err != nil {
		return nil, cursorTokenError(err)
	}
	if len(tok.Fields) == 0 {
		return nil, i18n.NewError(reasonCursorNoFields)
	}
	if len(tok.Fields) > maxCursorFields {
		return nil, i18n.NewError(reasonCursorTooManyFields, map[string]any{"count": len(tok.Fields), "max": maxCursorFields})
	}

	for i := range tok.Fields {
		tok.Fields[i].Name = stripControls(tok.Fields[i].Name)
	}
	return tok.Fields, nil
}

// ErrCursorTooLong is wrapped when the last row's cursor would not fit.
var ErrCursorTooLong = errors.New("pagination: cursor value too long")

// ErrCursorKeyNull is wrapped when the last row's cursor value is NULL.
var ErrCursorKeyNull = errors.New("pagination: cursor value is null")

// ErrCursorKeyMissing is wrapped when the last row lacks the cursor field.
var ErrCursorKeyMissing = errors.New("pagination: cursor field missing from row")

// NewCursorPage builds a CursorPage from limit+1 rows; a next-page key that
// is missing, NULL or too long leaves no cursor and wraps its ErrCursor* error.
func NewCursorPage(data []map[string]any, cursorField string, limit int) (CursorPage, error) {
	if limit < 1 {
		limit = DefaultPageSize
	}
	hasMore := len(data) > limit
	if hasMore {
		data = data[:limit]
	}
	page := CursorPage{Data: data, HasMore: hasMore}
	if hasMore && len(data) > 0 {
		last := data[len(data)-1]
		val, ok := last[cursorField]
		if !ok {
			return page, &i18n.Error{Reason: reasonCursorKeyMissing, Params: map[string]any{"field": cursorField}, Err: ErrCursorKeyMissing}
		}
		if isNullKey(val) {
			return page, &i18n.Error{Reason: reasonCursorKeyNull, Params: map[string]any{"field": cursorField}, Err: ErrCursorKeyNull}
		}
		cursor := EncodeCursor(cursorField, val)
		if !CursorFits(cursor) {
			return page, &i18n.Error{
				Reason: reasonCursorValueTooLong,
				Params: map[string]any{"field": cursorField, "size": len(cursor), "max": maxCursorEncodedSize},
				Err:    ErrCursorTooLong,
			}
		}
		page.Cursor = cursor
	}
	return page, nil
}

func isNullKey(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	return rv.Kind() == reflect.Pointer && rv.IsNil()
}

// NewOffsetPage builds an OffsetPage, TotalPages rounded up.
func NewOffsetPage(data []map[string]any, page, pageSize, total int) OffsetPage {
	return OffsetPage{
		Data:       data,
		Page:       page,
		PageSize:   pageSize,
		Total:      total,
		TotalPages: TotalPages(total, pageSize),
	}
}

// TotalPages rounds total/pageSize up for a nonnegative total and returns zero for pageSize < 1.
func TotalPages(total, pageSize int) int {
	if pageSize < 1 {
		return 0
	}
	pages := total / pageSize
	if total%pageSize > 0 {
		pages++
	}
	return pages
}

// ParsePositiveInt returns the positive decimal value of raw or fallback.
func ParsePositiveInt(raw string, fallback int) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return fallback
	}
	return n
}

// ParseLimit parses a positive page size, substitutes fallback if needed and applies the cap.
func ParseLimit(raw string, fallback, limitCap int) int {
	return ClampLimit(ParsePositiveInt(raw, fallback), limitCap)
}

// ClampLimit caps a selected page size. A nonpositive cap leaves the size as given.
func ClampLimit(size, limitCap int) int {
	if limitCap > 0 {
		return min(size, limitCap)
	}
	return size
}

// KeysetString is the text a cursor carries for keyset value v.
func KeysetString(v any) string { return toString(v) }

func toString(v any) string {
	switch val := v.(type) {
	case time.Time:
		return schema.FormatTimestamp(val)
	case *time.Time:
		if val != nil {
			return schema.FormatTimestamp(*val)
		}

		return "null"
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	default:
		rv := reflect.Indirect(reflect.ValueOf(val))
		if rv.Kind() == reflect.String {
			return rv.String()
		}
		b, err := json.Marshal(val)
		if err != nil {
			slog.Warn("pagination: encode keyset value", "error", err)
			return ""
		}
		return string(b)
	}
}
