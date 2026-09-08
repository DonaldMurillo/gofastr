// Package helper pins the one-hop credit: the read happens in one
// function, the cap in a same-package helper it hands the body to.
package helper

import (
	"encoding/json"
	"io"
	"net/http"
)

// readCapped wraps its parameter in a limiter and reads.
func readCapped(r io.Reader) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, 1<<20))
}

// viaHelper passes resp.Body to the capping helper: quiet.
func viaHelper(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return readCapped(resp.Body)
}

// decodeCapped seats a decoder on its parameter under a limiter.
func decodeCapped(r io.Reader) error {
	dec := json.NewDecoder(io.LimitReader(r, 1<<20))
	var v map[string]any
	return dec.Decode(&v)
}

// viaDecodeHelper: quiet.
func viaDecodeHelper(url string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return decodeCapped(resp.Body)
}

// uncappedHelper reads its parameter bare. Passing resp.Body to it
// stays quiet: the read inside the helper sees a bare parameter, not a
// resp.Body selector — the analyzer's fire shape is the selector, its
// credit shape is the one-hop capped helper.
func uncappedHelper(r io.Reader) ([]byte, error) {
	return io.ReadAll(r)
}

// viaUncappedHelper is the caller of the uncapped helper: quiet.
func viaUncappedHelper(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return uncappedHelper(resp.Body)
}
