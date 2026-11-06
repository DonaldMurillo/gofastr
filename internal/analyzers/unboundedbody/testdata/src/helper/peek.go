package helper

import (
	"io"
	"net/http"
)

// peek caps a COPY of the body's first bytes, not the request's Body
// itself: a helper whose LimitReader wraps some other reader caps
// nothing of the request it is handed, so a read after it still
// reports (the capping credit demands the rebind, like bodyRebinders).
func peek(w http.ResponseWriter, r *http.Request) {
	io.Copy(io.Discard, io.LimitReader(r.Body, 64))
	_ = w
}

func readAfterPeek(w http.ResponseWriter, r *http.Request) {
	peek(w, r)
	b, err := io.ReadAll(r.Body) // want `reads an inbound request body with no size cap`
	_, _ = b, err
}
