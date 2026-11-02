// Package jsonhelp is a body reader in another package: its summary
// travels as a rawJSONErr fact, so the caller's sink sees the origin.
package jsonhelp

import (
	"encoding/json"
	"io"
)

func ReadBody(r io.Reader, v any) error {
	return json.NewDecoder(r).Decode(v)
}
