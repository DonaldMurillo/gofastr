// Package xtop pins the CheckTopLevelKeys arm: a top-level-only walk
// paired with a plain json.Unmarshal whose destination (or a call
// site's destination, through the any parameter) has nested objects —
// the walk stopped one level short (harness mcpserver shape).
package xtop

import (
	"encoding/json"
	"strings"
)

// CheckTopLevelKeys walks the top level only (core/handler in the
// tree).
func CheckTopLevelKeys(data []byte, fold func(string) string) error {
	var v any
	return json.Unmarshal(data, &v)
}

// CheckObjectKeys walks every object at every depth (core/handler in
// the tree).
func CheckObjectKeys(data []byte, fold func(string) string) error {
	var v any
	return json.Unmarshal(data, &v)
}

// nested carries an object one level down: the top-level walk never
// saw its keys.
type nested struct {
	SessionID string          `json:"sessionId"`
	Arguments json.RawMessage `json:"arguments"`
}

// flat is all scalars: the top-level walk is the whole story.
type flat struct {
	SessionID string `json:"sessionId"`
}

// unmarshalObject is the oracle: CheckTopLevelKeys then plain stdlib
// decode into an any parameter — nesting is only visible at the call
// sites.
func unmarshalObject(data []byte, dst any) error {
	if err := CheckTopLevelKeys(data, strings.ToLower); err != nil { // want `json.Unmarshal of data after only handler.CheckTopLevelKeys`
		return err
	}
	return json.Unmarshal(data, dst)
}

// callNested passes the nested destination: fire (one diagnostic at
// the helper, above).
func callNested(raw []byte) (nested, error) {
	var v nested
	err := unmarshalObject(raw, &v)
	return v, err
}

// directNested is the concrete-destination spelling of the same fire.
func directNested(raw []byte) (nested, error) {
	var v nested
	if err := CheckTopLevelKeys(raw, strings.ToLower); err != nil { // want `json.Unmarshal of raw after only handler.CheckTopLevelKeys`
		return v, err
	}
	err := json.Unmarshal(raw, &v)
	return v, err
}

// callFlat passes the flat destination through the same helper: the
// helper already fired above; a flat-only helper would be quiet.
func callFlat(raw []byte) (flat, error) {
	var v flat
	err := unmarshalObject(raw, &v)
	return v, err
}

// deepChecked swaps the walk for the any-depth one: quiet.
func deepChecked(raw []byte) (nested, error) {
	var v nested
	if err := CheckObjectKeys(raw, strings.ToLower); err != nil {
		return v, err
	}
	err := json.Unmarshal(raw, &v)
	return v, err
}

// scalarSlice is a struct whose one composite field is a slice of
// scalars: no object keys to resolve, and no RawMessage — quiet even
// when the pairing is spelled directly.
type scalarSlice struct {
	Tags []string `json:"tags"`
}

func directScalarSlice(raw []byte) (scalarSlice, error) {
	var v scalarSlice
	if err := CheckTopLevelKeys(raw, strings.ToLower); err != nil {
		return v, err
	}
	err := json.Unmarshal(raw, &v)
	return v, err
}
