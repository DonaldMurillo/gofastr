package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// DecodeStrict reads one JSON value from r into dst with the same
// top-level key rule Bind applies to request bodies, for every decode
// site that is not a plain handler.Bind consumer: JSON-RPC envelopes,
// websocket frames, buffered bodies replayed through a helper, dev-tool
// endpoints. Stdlib encoding/json keeps the LAST duplicate key and
// matches struct tags case-insensitively, so {"action":"safe",
// "Action":"danger"} runs the second action while a reviewer (or an
// intercepting log) reads the first. The ambiguity itself is the bug;
// refusing it is the only resolution that does not privilege one
// parser's pick.
//
// Struct destinations: every top-level key must exactly match a json
// tag (case-folded spellings and unknown keys are refused, as Bind
// does) and no key may repeat. Map and other destinations: no key may
// repeat, and no two keys may fold to the same name under ASCII case
// folding. Bodies that are not a top-level object decode unchanged.
// Errors are *Error with code 400, so a handler can hand them to
// respond/Error directly.
//
// Size is the caller's job: wrap the reader in http.MaxBytesReader (or
// io.LimitReader) before calling. The read error is wrapped, not
// flattened, so a caller can still see the cap's *http.MaxBytesError
// through errors.As and answer 413 instead of the 400 the envelope
// carries. The probes that pinned each surface live beside the callers
// as *_security_test.go.
func DecodeStrict(r io.Reader, dst any) error {
	body, err := io.ReadAll(r)
	if err != nil {
		e := Fail(400, ReasonInvalidJSON, map[string]any{"detail": BodyReadDetail(err)})
		e.Err = err
		return e
	}
	return UnmarshalStrict(body, dst)
}

// StrictOption adjusts how UnmarshalStrict decodes a body that passed
// the key checks. The key rules themselves never change.
type StrictOption func(*strictConfig)

type strictConfig struct {
	useNumber bool
}

// UseNumber decodes numbers held in interface values (any,
// map[string]any, []any) as json.Number instead of float64, so an
// integer past 2^53 keeps its exact digits. Typed numeric fields are
// unaffected.
func UseNumber() StrictOption {
	return func(c *strictConfig) { c.useNumber = true }
}

// UnmarshalStrict is DecodeStrict over bytes already in memory (a
// websocket frame, a buffered body).
func UnmarshalStrict(data []byte, dst any, opts ...StrictOption) error {
	var cfg strictConfig
	for _, o := range opts {
		o(&cfg)
	}
	if err := validateBodyKeys(data, dst); err != nil {
		return err
	}
	// Every nesting level: a duplicate or case-folded key pair inside a
	// nested object reads two ways just like one at the top (the a2a
	// params object was the surface that showed it). The fold is the
	// decoder's own: strings.ToLower kept ſource apart from source while
	// encoding/json bound both to one field.
	if err := CheckObjectKeys(data, FoldJSONKey); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if isStructPointer(dst) {
		dec.DisallowUnknownFields()
	}
	if cfg.useNumber {
		dec.UseNumber()
	}
	if err := dec.Decode(dst); err != nil {
		// The decoder's own text names Go types and struct names; the
		// detail names the JSON path and the JSON type instead.
		return Fail(400, ReasonInvalidJSON, map[string]any{"detail": JSONErrorDetail(err)})
	}
	// Exactly one value: a second Decode must find nothing. A swallowed
	// second value is the same two-ways-to-read ambiguity the duplicate
	// key rules exist to kill (the bind-trailing pin).
	var trailing json.RawMessage
	if err := dec.Decode(&trailing); err != io.EOF {
		return Fail(400, ReasonJSONMultipleValues)
	}
	return nil
}

// FoldJSONKey folds an object key the way encoding/json matches it to a
// struct field: an ASCII letter by case, any other rune to the smallest
// member of its Unicode fold set, so ſ (U+017F) lands on S and the
// Kelvin sign on K. Two keys fold equal exactly when bytes.EqualFold
// says so. Pass it as the fold to CheckObjectKeys or CheckTopLevelKeys
// ahead of a decode into a struct; strings.ToLower misses ſ.
func FoldJSONKey(key string) string {
	var b strings.Builder
	b.Grow(len(key))
	for _, r := range key {
		if r < utf8.RuneSelf {
			if 'a' <= r && r <= 'z' {
				r -= 'a' - 'A'
			}
			b.WriteByte(byte(r))
			continue
		}
		for {
			next := unicode.SimpleFold(r)
			if next <= r {
				r = next
				break
			}
			r = next
		}
		b.WriteRune(r)
	}
	return b.String()
}

// CheckTopLevelKeys walks the top level of a JSON object and refuses
// any key that repeats, or that collides with an earlier key once both
// are passed through fold. Callers with their own key normalisation
// (crud folds camelCase and snake_case spellings of one column onto
// each other) pass that normaliser as fold; nil means exact keys only.
// Non-object bodies and tokenisation failures return nil so the caller's
// decoder reports them under its own error contract.
func CheckTopLevelKeys(data []byte, fold func(string) string) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	first, err := dec.Token()
	if err != nil {
		return nil
	}
	if d, ok := first.(json.Delim); !ok || d != '{' {
		return nil
	}
	seen := map[string]string{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil
		}
		key, ok := tok.(string)
		if !ok {
			return nil
		}
		norm := key
		if fold != nil {
			norm = fold(key)
		}
		if prev, dup := seen[norm]; dup {
			return keyRefusal(nil, prev, key)
		}
		seen[norm] = key
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return nil
		}
	}
	return nil
}

// CheckObjectKeys is CheckTopLevelKeys applied to every object at every
// nesting depth: no object anywhere in the value may repeat a key or
// hold two keys that fold to the same name. Non-object values and
// tokenisation failures return nil so the caller's decoder reports
// them under its own error contract.
func CheckObjectKeys(data []byte, fold func(string) string) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	// Numbers stay text: read as float64, 1e309 failed the walk before a
	// later duplicate was seen, and a caller decoding with UseNumber
	// accepted the body.
	dec.UseNumber()
	err := walkObjectKeys(dec, fold, nil)
	if _, tokenErr := err.(*Error); err != nil && !tokenErr {
		return nil
	}
	return err
}

// walkObjectKeys consumes one JSON value from dec, checking every object
// it contains. Only *Error values are findings; any other error is a
// tokenisation failure the caller ignores. path is where the value sits;
// a finding below the top level names it.
func walkObjectKeys(dec *json.Decoder, fold func(string) string, path []pathSeg) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	switch d := tok.(type) {
	case json.Delim:
		switch d {
		case '{':
			seen := map[string]string{}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return err
				}
				key, ok := kt.(string)
				if !ok {
					return errors.New("malformed object key")
				}
				norm := key
				if fold != nil {
					norm = fold(key)
				}
				if prev, dup := seen[norm]; dup {
					return keyRefusal(path, prev, key)
				}
				seen[norm] = key
				if err := walkObjectKeys(dec, fold, append(slices.Clip(path), pathSeg{key: key})); err != nil {
					return err
				}
			}
			_, err := dec.Token() // the closing '}'
			return err
		case '[':
			for i := 0; dec.More(); i++ {
				if err := walkObjectKeys(dec, fold, append(slices.Clip(path), pathSeg{index: i, isIdx: true})); err != nil {
					return err
				}
			}
			_, err := dec.Token() // the closing ']'
			return err
		}
	}
	return nil
}

// keyRefusal is the duplicate (prev == key) or case-folded key refusal
// for an object at path; the top level keeps its path-free wording.
func keyRefusal(path []pathSeg, prev, key string) *Error {
	params := map[string]any{"key": strconv.Quote(clip(key))}
	if prev != key {
		params = map[string]any{"first": strconv.Quote(clip(prev)), "second": strconv.Quote(clip(key))}
	}
	switch {
	case len(path) == 0 && prev == key:
		return Fail(400, ReasonJSONDuplicateKey, params)
	case len(path) == 0:
		return Fail(400, ReasonJSONFoldedKeys, params)
	}
	params["path"] = renderPath(path)
	if prev == key {
		return Fail(400, reasonJSONDuplicateKeyAt, params)
	}
	return Fail(400, reasonJSONFoldedKeysAt, params)
}

// isStructPointer reports whether dst points at a struct through one or
// more pointer levels: UnmarshalStrict(data, &p) with p a *T is the
// struct case, not a lax one.
func isStructPointer(dst any) bool {
	rv := reflect.ValueOf(dst)
	return rv.Kind() == reflect.Pointer && !rv.IsNil() && derefType(rv.Type().Elem()).Kind() == reflect.Struct
}
