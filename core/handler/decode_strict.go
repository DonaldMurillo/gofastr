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

// DecodeStrict reads r and decodes it with UnmarshalStrict. A read
// error stays reachable through errors.As; cap r's size first.
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
	useNumber       bool
	disallowUnknown bool
}

func disallowUnknownFields() StrictOption {
	return func(c *strictConfig) { c.disallowUnknown = true }
}

// UseNumber decodes numbers held in interface values (any,
// map[string]any, []any) as json.Number instead of float64, so an
// integer past 2^53 keeps its exact digits. Typed numeric fields are
// unaffected.
func UseNumber() StrictOption {
	return func(c *strictConfig) { c.useNumber = true }
}

// UnmarshalStrict decodes one JSON value into dst, refusing with a 400
// *Error invalid UTF-8, a duplicate or FoldJSONKey-folded key in any
// object, an unknown top-level key on a struct, and trailing data.
func UnmarshalStrict(data []byte, dst any, opts ...StrictOption) error {
	var cfg strictConfig
	for _, o := range opts {
		o(&cfg)
	}
	if err := CheckUTF8(data); err != nil {
		return err
	}
	if err := validateBodyKeys(data, dst); err != nil {
		return err
	}
	if err := CheckObjectKeys(data, FoldJSONKey); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if cfg.disallowUnknown || isStructPointer(dst) {
		dec.DisallowUnknownFields()
	}
	if cfg.useNumber {
		dec.UseNumber()
	}
	if err := dec.Decode(dst); err != nil {
		return Fail(400, ReasonInvalidJSON, map[string]any{"detail": JSONErrorDetail(err)})
	}
	var trailing json.RawMessage
	if err := dec.Decode(&trailing); err != io.EOF {
		return Fail(400, ReasonJSONMultipleValues)
	}
	return nil
}

// CheckUTF8 refuses data that is not valid UTF-8 with a 400 json_not_utf8.
func CheckUTF8(data []byte) error {
	if !utf8.Valid(data) {
		return Fail(400, ReasonJSONNotUTF8)
	}
	return nil
}

// FoldJSONKey folds key as encoding/json matches it to a field: ASCII by
// case, any other rune to the smallest member of its Unicode fold set.
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

// CheckTopLevelKeys refuses a top-level key that repeats under fold (nil
// for exact keys); a body that is not an object, or fails to tokenise,
// is left to the caller's decoder.
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

// CheckObjectKeys is CheckTopLevelKeys for every object at every depth.
func CheckObjectKeys(data []byte, fold func(string) string) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	err := walkObjectKeys(dec, fold, nil)
	if _, tokenErr := err.(*Error); err != nil && !tokenErr {
		return nil
	}
	return err
}

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

func isStructPointer(dst any) bool {
	rv := reflect.ValueOf(dst)
	return rv.Kind() == reflect.Pointer && !rv.IsNil() && derefType(rv.Type().Elem()).Kind() == reflect.Struct
}
