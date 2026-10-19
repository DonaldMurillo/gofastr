// Package xownvia holds xown's envelope through xownmid without
// importing xown: the fact must reach it across the middle package.
package xownvia

import (
	"encoding/json"

	"xownmid"
)

func relay(raw []byte) error {
	return json.Unmarshal(raw, xownmid.Get()) // want `lax decode of xown.Request, which xown also decodes strictly`
}
