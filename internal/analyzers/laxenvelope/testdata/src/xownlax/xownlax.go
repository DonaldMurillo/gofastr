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
