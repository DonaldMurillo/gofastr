package prose

import "encoding/json"

type v struct{ A int }

// proseAbove: a stand-alone comment that merely MENTIONS the marker in
// prose. The marker is not the directive; the diagnostic on the next
// line must survive.
func proseAbove(b []byte) {
	var x v
	// The //gofastr:allow(discardeddecode) marker is documented in allow.go.
	_ = json.Unmarshal(b, &x) // want `json.Unmarshal error discarded`
}

// proseTrailing: the trigger's own trailing comment is prose mentioning
// the marker, with the want expectation carried in the same comment
// (same shape as fixture a's otherRule line).
func proseTrailing(b []byte) {
	var x v
	_ = json.Unmarshal(b, &x) // the //gofastr:allow(discardeddecode) marker is documented in allow.go // want `json.Unmarshal error discarded`
}
