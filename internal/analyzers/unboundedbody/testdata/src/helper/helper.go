// Package helper factors the cap into a named function in one file and
// reads the body in another — the shape a package reaches for once more
// than one handler needs the same limit. The read must NOT be reported.
package helper

import (
	"net/http"
)

const maxBody = 1 << 20

func limitBody(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
}
