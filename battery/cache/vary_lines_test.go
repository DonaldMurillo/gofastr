package cache

import (
	"bytes"
	"net/http"
	"testing"
)

// TestVaryStarOnSecondLineNotStored: a handler that sets Vary: Accept
// and then Adds Vary: * put the star on a line Header.Get never reads,
// so the response was stored and replayed to callers it does not fit.
func TestVaryStarOnSecondLineNotStored(t *testing.T) {
	rec := &responseRecorder{
		header:     http.Header{"Vary": {"Accept", "*"}},
		body:       &bytes.Buffer{},
		statusCode: http.StatusOK,
	}
	if isStoreable(rec, http.Header{}, false, false) {
		t.Fatal("a response with Vary: * on its second Vary line was storeable")
	}
}
