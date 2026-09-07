// Package xlax is the lax side of the cross-package fixture: it never
// decodes xwire's types strictly itself, but imports xstrict, so the
// strictness fact arrives over the import edge. The kiln/agent shape
// (a sibling with no import edge) is covered by the module index in
// the repo tree, not here: testdata paths are excluded from the index
// by design.
package xlax

import (
	"encoding/json"

	"xstrict"
	"xwire"
)

// direct is the plain cross-package fire: xwire.Args is
// strict-decoded by xstrict, lax here.
func direct(raw []byte) error {
	var a xwire.Args
	return json.Unmarshal(raw, &a) // want `lax decode of xwire.Args, which xstrict also decodes strictly`
}

// viaClosure is the kiln/agent shape: the decode is wrapped in a
// closure over an any parameter, and the concrete types are only
// visible where the closure is called. Both call sites feed strict
// types, so one aggregate diagnostic names the pair.
func viaClosure(buf []byte) error {
	dec := func(out any) error { return json.Unmarshal(buf, out) } // want `lax decode into 2 types`
	var a xwire.Args
	if err := dec(&a); err != nil {
		return err
	}
	return dec(new(xwire.OtherArgs))
}

// untouched never touches a strict type: quiet by design.
func untouched(raw []byte) error {
	var m map[string]any
	return json.Unmarshal(raw, &m)
}

// keepImports pins both imports through a real reference, so the
// package fact from xstrict is genuinely on the import edge.
func keepImports(raw []byte) error {
	return xstrict.Dispatch(raw)
}
