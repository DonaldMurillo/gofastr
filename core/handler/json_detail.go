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

// JSONErrorDetail turns an error from decoding a JSON body into the
// phrase a client reads: the JSON path in the client's own key spelling
// and the JSON type expected there, the byte a syntax error sits at, an
// empty or cut-off body. It never carries the decoder's own text, which
// names Go types and struct names ("cannot unmarshal number into
// batchDeleteRequest.ids.0 of type string", "Go value of type
// map[string]interface {}"): those are the server's internals, not the
// client's mistake. An error it does not recognise reads as a generic
// "the value could not be decoded".
//
// The result is an [i18n.Phrased] error with no wrapped cause: pass it
// as the {{detail}} param of a frame ([ReasonInvalidJSON], a package's
// own "invalid request body: {{detail}}") and it translates with the
// frame while the frame's code stays the body's "reason". Its Error() is
// the English text, for a protocol (JSON-RPC) whose messages are English.
// A [i18n.Phrased] err (a strict-key refusal) comes back as its own
// phrase; a nil err is nil.
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
		// json.Unmarshal reports a cut-off body as a SyntaxError;
		// json.Decoder reports it as io.ErrUnexpectedEOF. One phrase.
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
		// jsonexact.Settle: a literal out of float64's range (1e400).
		return i18n.NewError(jsonNumberRangeRoot)
	}
	// DisallowUnknownFields below the top level (the top level is
	// refused earlier, by key): encoding/json's error is a bare string.
	if rest, ok := strings.CutPrefix(err.Error(), "json: unknown field "); ok {
		if key, uerr := strconv.Unquote(rest); uerr == nil {
			return i18n.NewError(jsonUnknownField, map[string]any{"key": strconv.Quote(clip(key))})
		}
	}
	return i18n.NewError(jsonInvalid)
}

// BodyReadDetail is JSONErrorDetail for a failure reading the body
// before any decode ran: over its cap, cut off mid-stream, or a read
// that failed (a reset connection). Like JSONErrorDetail it never
// carries the error's own text. A nil err is nil.
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

// typeErrorDetail phrases a value of the wrong JSON type. Field is the
// JSON path encoding/json walked ("ids.0", "a.b.0.c"; the struct name
// sits in Struct, not here), Value its own word for what it found
// ("number", "number 1.5", "string \"abc\""), Type the Go type it
// wanted, which is spoken as the JSON type that type takes.
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
		// The right JSON type, a value the Go type cannot hold: 1.5 or
		// 300 for a uint8, 1e400 for a float64.
		if isIntegerKind(te.Type) {
			return pick(jsonIntegerRange, jsonIntegerRangeRoot)
		}
		return pick(jsonNumberRange, jsonNumberRangeRoot)
	case wantOK:
		params["expected"] = i18n.NewError(want)
		params["got"] = i18n.NewError(got)
		return pick(jsonWrongType, jsonWrongTypeRoot)
	default:
		params["got"] = i18n.NewError(got)
		return pick(jsonUnexpectedType, jsonUnexpectedTypeRoot)
	}
}

// jsonValueKind maps encoding/json's word for the value it read to the
// JSON type's phrase. The word comes first; a literal may follow it.
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

// jsonTypeOf is the JSON type a value decoding into t must have, or
// false when t decodes itself (json.Unmarshaler) and could take any.
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

// pathSeg is one step of a JSON path: an object key or an array index.
type pathSeg struct {
	key   string
	index int
	isIdx bool
}

// renderFieldPath renders encoding/json's dotted Field ("a.b.0.c") the
// way a client writes the path: a.b[0].c. An all-digit segment is an
// array index (encoding/json spells a map's numeric key the same way;
// the bracket reads right for both). Field already holds the JSON key
// names, never Go field names, so nothing is stripped.
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

// maxPathBytes caps a rendered path: a key is the client's own text, and
// a body can hold a megabyte of it.
const maxPathBytes = 200

// renderPath writes segs as a.b[0].c. A key that is not a plain name
// (spaces, dots, quotes, control bytes, empty) is written quoted in
// brackets, ["a b"], so the path reads one way and carries no raw
// control byte.
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

// clip cuts s to maxPathBytes on a rune boundary, marking the cut.
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

// isPlainKey reports a key that reads unambiguously bare in a path: an
// ASCII letter or underscore, then letters, digits, '_' or '-'.
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
