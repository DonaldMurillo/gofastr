package xtop2

// callNested passes the nested destination from another file of the
// same package: fire at the helper's top-level-only check.
func callNested(raw []byte) (nested2, error) {
	var v nested2
	err := unmarshalThing(raw, &v)
	return v, err
}

// callFlat passes a flat destination: the top-level walk covered it.
func callFlat(raw []byte) (flat2, error) {
	var v flat2
	err := unmarshalThing(raw, &v)
	return v, err
}

// flat2 is all scalars.
type flat2 struct {
	SessionID string `json:"sessionId"`
}
