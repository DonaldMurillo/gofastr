package a

//gofastr:allow-file(GOFASTR1407) fixture for the unboundedbody vet analyzer: the raw uncapped decodes are the defect under test

import (
	ej "encoding/json"
	"io"
	"net/http"
)

func readAll(w http.ResponseWriter, r *http.Request) {
	_, _ = io.ReadAll(r.Body) // want `reads an inbound request body with no size cap`
}

func decode(w http.ResponseWriter, r *http.Request) {
	var v struct{ N int }
	_ = ej.NewDecoder(r.Body).Decode(&v) // want `reads an inbound request body with no size cap`
}

// An aliased import is still the real encoding/json.
func decodeAliased(w http.ResponseWriter, req *http.Request) {
	var v struct{ N int }
	_ = ej.NewDecoder(req.Body).Decode(&v) // want `reads an inbound request body with no size cap`
}

func copyBody(w http.ResponseWriter, r *http.Request, dst io.Writer) {
	_, _ = io.Copy(dst, r.Body) // want `reads an inbound request body with no size cap`
}

// An OUTBOUND response body is a different risk class: the peer was
// chosen by this code. Not flagged.
func outbound(c *http.Client, u string) {
	resp, err := c.Get(u)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	_, _ = io.ReadAll(resp.Body)
}
