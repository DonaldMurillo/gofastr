package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// Destinations named the way the leak showed them: a refusal that says
// "BatchDeleteReq.ids.0" or "batchItem" is the server's own source.
type BatchDeleteReq struct {
	IDs []string `json:"ids"`
}

type batchItem struct {
	Price float64        `json:"price"`
	Qty   int            `json:"qty"`
	Tags  map[string]int `json:"tags"`
	When  time.Time      `json:"when"`
	OK    bool           `json:"ok"`
	Small uint8          `json:"small"`
}

type BatchEnvelope struct {
	Items []batchItem `json:"items"`
	Meta  struct {
		Note string `json:"note"`
	} `json:"meta"`
}

// goLeak matches what encoding/json's own text carries and a client
// must never read: Go type spellings, struct names, the decoder prefix.
var goLeak = regexp.MustCompile(`interface \{\}|map\[|struct|\b[A-Z][A-Za-z0-9_]*\.[a-z]|Go value|Go struct|of type|json: |cannot unmarshal|invalid character|float64|uint8|time\.Time|BatchDeleteReq|batchItem|BatchEnvelope`)

// TestJSONDecodeErrorsArePhrased runs each decode failure shape through
// UnmarshalStrict and pins the client's text: the JSON path in the
// client's spelling and the JSON type, the byte of a syntax error, and
// never a Go type or struct name.
func TestJSONDecodeErrorsArePhrased(t *testing.T) {
	cases := []struct {
		name string
		body string
		dst  func() any
		want string
		code string
	}{
		{"array element type", `{"ids":[1]}`, func() any { return &BatchDeleteReq{} },
			`invalid JSON: ids[0]: expected a string, got a number`, "invalid_json"},
		{"bool in string array", `{"ids":["a",true]}`, func() any { return &BatchDeleteReq{} },
			`invalid JSON: ids[1]: expected a string, got a boolean`, "invalid_json"},
		{"object in string array", `{"ids":[{}]}`, func() any { return &BatchDeleteReq{} },
			`invalid JSON: ids[0]: expected a string, got an object`, "invalid_json"},
		{"nested struct field", `{"items":[{"price":"x"}]}`, func() any { return &BatchEnvelope{} },
			`invalid JSON: items[0].price: expected a number, got a string`, "invalid_json"},
		{"nested object field", `{"meta":{"note":5}}`, func() any { return &BatchEnvelope{} },
			`invalid JSON: meta.note: expected a string, got a number`, "invalid_json"},
		{"array where number", `{"items":[{},{"qty":[1]}]}`, func() any { return &BatchEnvelope{} },
			`invalid JSON: items[1].qty: expected a number, got an array`, "invalid_json"},
		{"bool field", `{"items":[{"ok":"yes"}]}`, func() any { return &BatchEnvelope{} },
			`invalid JSON: items[0].ok: expected a boolean, got a string`, "invalid_json"},
		{"time field", `{"items":[{"when":5}]}`, func() any { return &BatchEnvelope{} },
			`invalid JSON: items[0].when: expected a string, got a number`, "invalid_json"},
		{"map value with odd key", `{"items":[{"tags":{"a b":"x"}}]}`, func() any { return &BatchEnvelope{} },
			`invalid JSON: items[0].tags["a b"]: expected a number, got a string`, "invalid_json"},
		{"fraction for integer", `{"items":[{"qty":1.5}]}`, func() any { return &BatchEnvelope{} },
			`invalid JSON: items[0].qty: expected a whole number in range`, "invalid_json"},
		{"integer overflow", `{"items":[{"small":300}]}`, func() any { return &BatchEnvelope{} },
			`invalid JSON: items[0].small: expected a whole number in range`, "invalid_json"},
		{"float overflow", `{"items":[{"price":1e400}]}`, func() any { return &BatchEnvelope{} },
			`invalid JSON: items[0].price: number out of range`, "invalid_json"},
		{"array into map", `[1]`, func() any { return &map[string]any{} },
			`invalid JSON: expected an object, got an array`, "invalid_json"},
		{"number into map", `5`, func() any { return &map[string]any{} },
			`invalid JSON: expected an object, got a number`, "invalid_json"},
		{"array into struct", `[1]`, func() any { return &BatchDeleteReq{} },
			`invalid JSON: expected an object, got an array`, "invalid_json"},
		{"unparseable time", `{"items":[{"when":"nope"}]}`, func() any { return &BatchEnvelope{} },
			`invalid JSON: the value could not be decoded`, "invalid_json"},
		{"syntax error", `{"ids": [1,}`, func() any { return &BatchDeleteReq{} },
			`invalid JSON: syntax error at byte 12`, "invalid_json"},
		{"syntax error in map", `{"a" 1}`, func() any { return &map[string]any{} },
			`invalid JSON: syntax error at byte 6`, "invalid_json"},
		{"cut off", `{"ids":[`, func() any { return &BatchDeleteReq{} },
			`invalid JSON: the body ends before the JSON value is complete`, "invalid_json"},
		{"empty body", ``, func() any { return &BatchDeleteReq{} },
			`invalid JSON: the body is empty`, "invalid_json"},
		{"nested unknown field", `{"meta":{"zz":1}}`, func() any { return &BatchEnvelope{} },
			`invalid JSON: unknown field "zz"`, "invalid_json"},
		{"top-level unknown field", `{"zz":1}`, func() any { return &BatchDeleteReq{} },
			`invalid JSON: unknown field "zz"`, "json_unknown_field"},
		{"top-level duplicate", `{"ids":[],"ids":[]}`, func() any { return &map[string]any{} },
			`invalid JSON: duplicate key "ids"`, "json_duplicate_key"},
		{"nested duplicate", `{"meta":{"note":"a","note":"b"}}`, func() any { return &map[string]any{} },
			`invalid JSON: duplicate key "note" in meta`, "json_duplicate_key"},
		{"nested folded keys", `{"items":[{"qty":1},{"qty":1,"QTY":2}]}`, func() any { return &map[string]any{} },
			`invalid JSON: keys "qty" and "QTY" in items[1] name the same field`, "json_folded_keys"},
		{"top-level folded keys", `{"note":1,"Note":2}`, func() any { return &map[string]any{} },
			`invalid JSON: keys "note" and "Note" name the same field`, "json_folded_keys"},
		{"trailing value", `{"ids":[]} {}`, func() any { return &BatchDeleteReq{} },
			`invalid JSON: body must contain exactly one JSON value`, "json_multiple_values"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := UnmarshalStrict([]byte(tc.body), tc.dst())
			herr, ok := errors.AsType[*Error](err)
			if !ok {
				t.Fatalf("err = %v (%T), want *Error", err, err)
			}
			got := herr.Reason.Text(context.Background(), herr.Params)
			if got != tc.want {
				t.Errorf("text = %q\n want  %q", got, tc.want)
			}
			if code := herr.Reason.Code(); code != tc.code {
				t.Errorf("reason = %q, want %q", code, tc.code)
			}
			if m := goLeak.FindString(got); m != "" {
				t.Errorf("text %q carries Go internals %q", got, m)
			}
			// Error() is what a log line and a JSON-RPC message read.
			if m := goLeak.FindString(err.Error()); m != "" {
				t.Errorf("Error() %q carries Go internals %q", err.Error(), m)
			}
		})
	}
}

// The other decode errors a caller hands JSONErrorDetail directly: a
// body over its cap, a cut-off body json.Unmarshal reports as a
// SyntaxError, jsonexact's out-of-range literal, a programmer error.
func TestJSONErrorDetailOtherShapes(t *testing.T) {
	var unmarshalCut error
	if err := json.Unmarshal([]byte(`{"a":`), &map[string]any{}); err != nil {
		unmarshalCut = err
	}
	_, rangeErr := strconv.ParseFloat("1e400", 64)
	var nilDst *map[string]any
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"max bytes", &http.MaxBytesError{Limit: 10}, "the body is too large"},
		{"unmarshal cut off", unmarshalCut, "the body ends before the JSON value is complete"},
		{"decoder cut off", fmt.Errorf("read: %w", io.ErrUnexpectedEOF), "the body ends before the JSON value is complete"},
		{"settle range", rangeErr, "a number is out of range"},
		{"nil destination", json.Unmarshal([]byte(`{}`), nilDst), "the value could not be decoded"},
		{"anything else", errors.New("map[string]interface {} reflect: Go value of type main.T"), "the value could not be decoded"},
		{"phrased passes through", Fail(400, ReasonJSONDuplicateKey, map[string]any{"key": `"a"`}), `invalid JSON: duplicate key "a"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err == nil {
				t.Fatal("the shape produced no error")
			}
			d := JSONErrorDetail(tc.err)
			p, ok := d.(i18n.Phrased)
			if !ok {
				t.Fatalf("%T is not Phrased", d)
			}
			ph, params := p.Phrased()
			if got := ph.Text(context.Background(), params); got != tc.want {
				t.Errorf("text = %q, want %q", got, tc.want)
			}
			if d.Error() != tc.want {
				t.Errorf("Error() = %q, want %q", d.Error(), tc.want)
			}
			if errors.Unwrap(d) != nil {
				t.Error("the detail wraps its cause: the frame's code would give way to the detail's")
			}
		})
	}
}

// A path is the client's own key text: control bytes come back escaped
// and a long key is cut, never echoed whole.
func TestJSONPathEscapesAndCaps(t *testing.T) {
	err := UnmarshalStrict([]byte(`{"items":[{"tags":{"a\u001b[31m":"x"}}]}`), &BatchEnvelope{})
	herr, _ := errors.AsType[*Error](err)
	if herr == nil {
		t.Fatalf("err = %v", err)
	}
	got := herr.Reason.Format(herr.Params)
	if strings.ContainsRune(got, 0x1b) || !strings.Contains(got, `tags["a\x1b[31m"]`) {
		t.Errorf("text = %q, want the key quoted with its escape spelled out", got)
	}

	long := strings.Repeat("k", 5000)
	err = UnmarshalStrict([]byte(`{"items":[{"tags":{"`+long+`":"x"}}]}`), &BatchEnvelope{})
	herr, _ = errors.AsType[*Error](err)
	if herr == nil {
		t.Fatalf("err = %v", err)
	}
	if got := herr.Reason.Format(herr.Params); len(got) > 400 {
		t.Errorf("text is %d bytes; a key the client sent must be cut", len(got))
	}
}

// The detail translates with its frame: each fragment is a phrase, so a
// catalog renders the whole sentence in the client's language, and the
// body's reason stays the frame's code.
func TestJSONDecodeErrorTranslates(t *testing.T) {
	cat := i18n.NewMapCatalog()
	cat.Set("es", "error.invalid_json", i18n.Message{Text: "JSON no válido: {{detail}}"})
	cat.Set("es", "json.decode.wrong_type", i18n.Message{Text: "{{path}}: se esperaba {{expected}}, se recibió {{got}}"})
	cat.Set("es", "json.type.string", i18n.Message{Text: "una cadena"})
	cat.Set("es", "json.type.number", i18n.Message{Text: "un número"})
	ctx := i18n.WithTranslator(i18n.WithContext(context.Background(), i18n.Locale{Tag: "es"}), i18n.NewTranslator(cat, "en"))
	r := httptest.NewRequest("POST", "/", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	WriteError(w, r, UnmarshalStrict([]byte(`{"ids":[1]}`), &BatchDeleteReq{}))
	var body errorResponse
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if w.Code != 400 || body.Error.Reason != "invalid_json" {
		t.Fatalf("status %d reason %q", w.Code, body.Error.Reason)
	}
	if want := "JSON no válido: ids[0]: se esperaba una cadena, se recibió un número"; body.Error.Message != want {
		t.Fatalf("message %q, want %q", body.Error.Message, want)
	}
}

// Bind answers the same phrases: its body decode and its Settle pass
// share JSONErrorDetail with UnmarshalStrict.
func TestBindDecodeErrorsArePhrased(t *testing.T) {
	cases := []struct{ body, want string }{
		{`{"ids":[1]}`, `invalid JSON: ids[0]: expected a string, got a number`},
		{`{"ids":[1,}`, `invalid JSON: syntax error at byte 11`},
		{`{"ids":[`, `invalid JSON: the body ends before the JSON value is complete`},
	}
	for _, tc := range cases {
		r := httptest.NewRequest("POST", "/", strings.NewReader(tc.body))
		r.Header.Set("Content-Type", "application/json")
		var dst BatchDeleteReq
		err := Bind(r, &dst)
		herr, _ := errors.AsType[*Error](err)
		if herr == nil {
			t.Fatalf("%s: err = %v", tc.body, err)
		}
		if got := herr.Reason.Format(herr.Params); got != tc.want {
			t.Errorf("%s: text = %q, want %q", tc.body, got, tc.want)
		}
	}
	var loose struct {
		Data map[string]any `json:"data"`
	}
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"data":{"n":1e400}}`))
	r.Header.Set("Content-Type", "application/json")
	err := Bind(r, &loose)
	herr, _ := errors.AsType[*Error](err)
	if herr == nil {
		t.Fatalf("1e400 under an interface: err = %v", err)
	}
	if got := herr.Reason.Format(herr.Params); got != "invalid JSON: a number is out of range" {
		t.Errorf("settle text = %q", got)
	}
}

type failingReader struct{ err error }

func (f failingReader) Read([]byte) (int, error) { return 0, f.err }

// A read that fails before any decode says so, without the read error's
// own text, and keeps the cause reachable for a 413 decision.
func TestBodyReadDetail(t *testing.T) {
	err := DecodeStrict(failingReader{errors.New("read tcp 10.0.0.1:443: connection reset by peer")}, &BatchDeleteReq{})
	herr, _ := errors.AsType[*Error](err)
	if herr == nil {
		t.Fatalf("err = %v", err)
	}
	if got := herr.Reason.Format(herr.Params); got != "invalid JSON: the body could not be read" {
		t.Errorf("text = %q", got)
	}
	err = DecodeStrict(failingReader{&http.MaxBytesError{Limit: 8}}, &BatchDeleteReq{})
	if _, ok := errors.AsType[*http.MaxBytesError](err); !ok {
		t.Errorf("the cap's error is no longer reachable: %v", err)
	}
	herr, _ = errors.AsType[*Error](err)
	if got := herr.Reason.Format(herr.Params); got != "invalid JSON: the body is too large" {
		t.Errorf("text = %q", got)
	}
	if JSONErrorDetail(nil) != nil || BodyReadDetail(nil) != nil {
		t.Error("a nil error must stay nil")
	}
}
