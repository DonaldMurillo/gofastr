// Package a pins the unbounded-response reads: fetched bodies read
// with no limiter anywhere on the chain.
package a

import (
	"encoding/json"
	"io"
	"net/http"
)

func fetchRaw(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body) // want `unbounded read of an \*http\.Response body`
}

func fetchDecode(url string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var v map[string]any
	return json.NewDecoder(resp.Body).Decode(&v) // want `unbounded read of an \*http\.Response body`
}

// seatedDecoder splits the NewDecoder and Decode lines: the fire is at
// the seat.
func seatedDecoder(url string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	dec := json.NewDecoder(resp.Body) // want `unbounded read of an \*http\.Response body`
	return dec.Decode(&v0)
}

var v0 any

// capped is the fix posture: LimitReader on the chain.
func capped(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// cappedDecode seats the decoder on a limited reader.
func cappedDecode(url string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	dec := json.NewDecoder(io.LimitReader(resp.Body, 1<<20))
	var v map[string]any
	return dec.Decode(&v)
}

// maxBytes is the http.MaxBytesReader spelling of the fix.
func maxBytes(w http.ResponseWriter, url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(http.MaxBytesReader(w, resp.Body, 4<<10))
}

// reqBody is unboundedbody's surface: quiet here.
func reqBody(r *http.Request) ([]byte, error) {
	return io.ReadAll(r.Body)
}

// constructedResponse reads a body the program itself built: the byte
// count is not the network's.
func constructedResponse() ([]byte, error) {
	resp := &http.Response{Body: http.NoBody}
	return io.ReadAll(resp.Body)
}
