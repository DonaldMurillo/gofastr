// Package xstrict is the strict side of the cross-package fixture:
// it decodes xwire's types through its own UnmarshalStrict (the
// declared contract, whichever package provides one), which exports
// the strictness fact to every importer.
package xstrict

import (
	"encoding/json"

	"xwire"
)

// UnmarshalStrict is the strict decoder, by name.
func UnmarshalStrict(data []byte, dst any) error {
	return json.Unmarshal(data, dst)
}

// Dispatch is the strict transport: both xwire types decoded strictly.
func Dispatch(raw []byte) error {
	var a xwire.Args
	if err := UnmarshalStrict(raw, &a); err != nil {
		return err
	}
	var b xwire.OtherArgs
	return UnmarshalStrict(raw, &b)
}
