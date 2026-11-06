// Package xtop2 is xtop split across two files: the strict helper
// with its any-dst decode lives in helper.go while the call site
// passing the nested destination lives in caller.go. The arm resolves
// any-destinations from package-local call sites, which sit in any
// file of the package.
package xtop2

import (
	"encoding/json"
	"strings"
)

// CheckTopLevelKeys walks the top level only.
func CheckTopLevelKeys(data []byte, fold func(string) string) error {
	var v any
	return json.Unmarshal(data, &v)
}

// nested2 carries an object one level down.
type nested2 struct {
	SessionID string          `json:"sessionId"`
	Arguments json.RawMessage `json:"arguments"`
}

// unmarshalThing is the oracle: the top-level-only check, then a plain
// stdlib decode into an any parameter — nesting is only visible at the
// call sites, which live in caller.go.
func unmarshalThing(data []byte, dst any) error {
	if err := CheckTopLevelKeys(data, strings.ToLower); err != nil { // want `json.Unmarshal of data after only handler.CheckTopLevelKeys`
		return err
	}
	return json.Unmarshal(data, dst)
}
