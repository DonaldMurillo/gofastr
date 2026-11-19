package handler

import (
	"encoding"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// JSONErrorDetail returns the phrase a client reads for a JSON decode
// error: its JSON path and JSON types, never the decoder's text. A
// phrased error comes back as its own phrase; nil is nil.
func JSONErrorDetail(err error) error {
	if err == nil {
		return nil
	}
	if p, ok := errors.AsType[i18n.Phrased](err); ok {
		if ph, params := p.Phrased(); !ph.IsZero() {
			return &i18n.Error{Reason: ph, Params: params}
		}
	}
	if te, ok := errors.AsType[*json.UnmarshalTypeError](err); ok {
		return typeErrorDetail(te)
	}
	if se, ok := errors.AsType[*json.SyntaxError](err); ok {
		if strings.HasPrefix(se.Error(), "unexpected end of JSON input") {
			return i18n.NewError(jsonUnexpectedEnd)
		}
		return i18n.NewError(jsonSyntax, map[string]any{"offset": se.Offset})
	}
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		return i18n.NewError(jsonTooLarge)
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return i18n.NewError(jsonUnexpectedEnd)
	}
	if errors.Is(err, io.EOF) {
		return i18n.NewError(jsonEmpty)
	}
	if ne, ok := errors.AsType[*strconv.NumError](err); ok && errors.Is(ne.Err, strconv.ErrRange) {
		return i18n.NewError(jsonNumberRangeRoot)
	}
	if rest, ok := strings.CutPrefix(err.Error(), "json: unknown field "); ok {
		if key, uerr := strconv.Unquote(rest); uerr == nil {
			return i18n.NewError(jsonUnknownField, map[string]any{"key": strconv.Quote(clip(key))})
		}
	}
	return i18n.NewError(jsonInvalid)
}

// BodyReadDetail is JSONErrorDetail for a failed body read.
func BodyReadDetail(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		return i18n.NewError(jsonTooLarge)
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return i18n.NewError(jsonUnexpectedEnd)
	}
	return i18n.NewError(jsonUnreadable)
}

func typeErrorDetail(te *json.UnmarshalTypeError) error {
	path := renderFieldPath(te.Field)
	got, gotOK := jsonValueKind(te.Value)
	want, wantOK := jsonTypeOf(te.Type)
	params := map[string]any{}
	if path != "" {
		params["path"] = path
	}
	pick := func(at, root i18n.Phrase) error {
		if path == "" {
			return i18n.NewError(root, params)
		}
		return i18n.NewError(at, params)
	}
	switch {
	case !gotOK:
		return pick(jsonInvalidAt, jsonInvalid)
	case wantOK && got == jsonTypeNumber && want == jsonTypeNumber:
		if isIntegerKind(te.Type) {
			return pick(jsonIntegerRange, jsonIntegerRangeRoot)
		}
		return pick(jsonNumberRange, jsonNumberRangeRoot)
	case wantOK && got == want:
		return pick(jsonInvalidAt, jsonInvalid)
	case wantOK:
		params["expected"] = i18n.NewError(want)
		params["got"] = i18n.NewError(got)
		return pick(jsonWrongType, jsonWrongTypeRoot)
	default:
		params["got"] = i18n.NewError(got)
		return pick(jsonUnexpectedType, jsonUnexpectedTypeRoot)
	}
}

func jsonValueKind(value string) (i18n.Phrase, bool) {
	word, _, _ := strings.Cut(value, " ")
	switch word {
	case "string":
		return jsonTypeString, true
	case "number":
		return jsonTypeNumber, true
	case "bool":
		return jsonTypeBoolean, true
	case "object":
		return jsonTypeObject, true
	case "array":
		return jsonTypeArray, true
	case "null":
		return jsonTypeNull, true
	}
	return i18n.Phrase{}, false
}

var (
	jsonUnmarshalerType = reflect.TypeFor[json.Unmarshaler]()
	textUnmarshalerType = reflect.TypeFor[encoding.TextUnmarshaler]()
	jsonNumberType      = reflect.TypeFor[json.Number]()
	timeType            = reflect.TypeFor[time.Time]()
)

func jsonTypeOf(t reflect.Type) (i18n.Phrase, bool) {
	if t == nil {
		return i18n.Phrase{}, false
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch {
	case t == jsonNumberType:
		return jsonTypeNumber, true
	case t == timeType:
		return jsonTypeString, true
	case implements(t, jsonUnmarshalerType):
		return i18n.Phrase{}, false
	case implements(t, textUnmarshalerType):
		return jsonTypeString, true
	}
	switch t.Kind() {
	case reflect.String:
		return jsonTypeString, true
	case reflect.Bool:
		return jsonTypeBoolean, true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		return jsonTypeNumber, true
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return jsonTypeString, true // []byte is base64 text
		}
		return jsonTypeArray, true
	case reflect.Array:
		return jsonTypeArray, true
	case reflect.Map, reflect.Struct:
		return jsonTypeObject, true
	}
	return i18n.Phrase{}, false
}

func implements(t, iface reflect.Type) bool {
	return t.Implements(iface) || reflect.PointerTo(t).Implements(iface)
}

func isIntegerKind(t reflect.Type) bool {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil {
		return false
	}
	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return true
	}
	return false
}

type pathSeg struct {
	key   string
	index int
	isIdx bool
}

func renderFieldPath(field string) string {
	if field == "" {
		return ""
	}
	parts := strings.Split(field, ".")
	segs := make([]pathSeg, 0, len(parts))
	for _, p := range parts {
		if n, err := strconv.Atoi(p); err == nil && n >= 0 && isDigits(p) {
			segs = append(segs, pathSeg{index: n, isIdx: true})
			continue
		}
		segs = append(segs, pathSeg{key: p})
	}
	return renderPath(segs)
}

const maxPathBytes = 200

func renderPath(segs []pathSeg) string {
	var b strings.Builder
	for i, s := range segs {
		switch {
		case s.isIdx:
			b.WriteByte('[')
			b.WriteString(strconv.Itoa(s.index))
			b.WriteByte(']')
		case isPlainKey(s.key):
			if i > 0 {
				b.WriteByte('.')
			}
			b.WriteString(s.key)
		default:
			b.WriteByte('[')
			b.WriteString(strconv.Quote(s.key))
			b.WriteByte(']')
		}
		if b.Len() > maxPathBytes {
			break
		}
	}
	return clip(b.String())
}

func clip(s string) string {
	if len(s) <= maxPathBytes {
		return s
	}
	cut := maxPathBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func isPlainKey(k string) bool {
	if k == "" {
		return false
	}
	for i := 0; i < len(k); i++ {
		c := k[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_':
		case i > 0 && (c >= '0' && c <= '9' || c == '-'):
		default:
			return false
		}
	}
	return true
}
