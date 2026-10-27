package a

import "encoding/json"

// Test files are skipped: a fixture's numbers are the test's own.
func decodeInTest(b []byte) any {
	var v any
	_ = json.Unmarshal(b, &v)
	return v
}
