package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
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
// io.LimitReader) before calling. The probes that pinned each surface
// live beside the callers as *_security_test.go.
func DecodeStrict(r io.Reader, dst any) error {
	body, err := io.ReadAll(r)
	if err != nil {
		return Errorf(400, "invalid JSON: %s", err.Error())
	}
	return UnmarshalStrict(body, dst)
}

// UnmarshalStrict is DecodeStrict over bytes already in memory (a
// websocket frame, a buffered body).
func UnmarshalStrict(data []byte, dst any) error {
	if err := validateBodyKeys(data, dst); err != nil {
		return err
	}
	// Every nesting level: a duplicate or case-folded key pair inside a
	// nested object reads two ways just like one at the top (the a2a
	// params object was the surface that showed it).
	if err := CheckObjectKeys(data, strings.ToLower); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if isStructPointer(dst) {
		dec.DisallowUnknownFields()
	}
	if err := dec.Decode(dst); err != nil {
		return Errorf(400, "invalid JSON: %s", err.Error())
	}
	return nil
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
			if prev == key {
				return Errorf(400, "invalid JSON: duplicate key %q", key)
			}
			return Errorf(400, "invalid JSON: keys %q and %q name the same field", prev, key)
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
	err := walkObjectKeys(dec, fold)
	if _, tokenErr := err.(*Error); err != nil && !tokenErr {
		return nil
	}
	return err
}

// walkObjectKeys consumes one JSON value from dec, checking every object
// it contains. Only *Error values are findings; any other error is a
// tokenisation failure the caller ignores.
func walkObjectKeys(dec *json.Decoder, fold func(string) string) error {
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
					if prev == key {
						return Errorf(400, "invalid JSON: duplicate key %q", key)
					}
					return Errorf(400, "invalid JSON: keys %q and %q name the same field", prev, key)
				}
				seen[norm] = key
				if err := walkObjectKeys(dec, fold); err != nil {
					return err
				}
			}
			_, err := dec.Token() // the closing '}'
			return err
		case '[':
			for dec.More() {
				if err := walkObjectKeys(dec, fold); err != nil {
					return err
				}
			}
			_, err := dec.Token() // the closing ']'
			return err
		}
	}
	return nil
}

func isStructPointer(dst any) bool {
	rv := reflect.ValueOf(dst)
	return rv.Kind() == reflect.Pointer && !rv.IsNil() && rv.Elem().Kind() == reflect.Struct
}
