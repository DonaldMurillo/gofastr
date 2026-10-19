// Package xownlax imports xown and decodes xown's own envelope lax: the
// fact from xown is the only way this package can learn the type is
// strict.
package xownlax

import (
	"encoding/json"

	"xown"
)

func bridge(raw []byte) error {
	var r xown.Request
	return json.Unmarshal(raw, &r) // want `lax decode of xown.Request, which xown also decodes strictly`
}

// viaConstructor reaches xown's unexported envelope through New: the
// importer cannot name the type, but it decodes it all the same.
func viaConstructor(raw []byte) error {
	return json.Unmarshal(raw, xown.New()) // want `lax decode of xown.request, which xown also decodes strictly`
}
