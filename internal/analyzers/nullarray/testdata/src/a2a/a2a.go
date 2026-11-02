// Package a2a declares an exported required array and a non-nil helper;
// package user fills them from outside.
package a2a

type Part struct{ Text string }

type Message struct {
	MessageID string `json:"messageId"`
	Parts     []Part `json:"parts"` //gofastr:required-array // want Parts:"requiredArray"
}

// PartsOrEmpty never returns nil.
func PartsOrEmpty(p []Part) []Part { // want PartsOrEmpty:"nonNilResult"
	if len(p) == 0 {
		return []Part{}
	}
	return p
}
