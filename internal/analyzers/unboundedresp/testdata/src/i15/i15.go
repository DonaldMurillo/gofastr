// Package i15 pins bug bash 2026-10-27 I15: a limiter that wraps the
// body but is never the reader. unboundedresp.go wrapsInLimiter marked
// the response capped when ANY limiter in the function mentioned it, so
// `_ = io.LimitReader(resp.Body, 1); io.ReadAll(resp.Body)` stayed
// quiet. A read of resp.Body itself is bounded only when resp.Body was
// REBOUND to a limited reader before the read.
package i15

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// discardedLimiter is the ledger's reduction.
func discardedLimiter(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	_ = io.LimitReader(resp.Body, 1)
	return io.ReadAll(resp.Body) // want `unbounded read of an \*http\.Response body`
}

// errorPathRead caps the success read and reads the error body raw:
// the error path is the unbounded one.
func errorPathRead(url string) (map[string]any, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(resp.Body) // want `unbounded read of an \*http\.Response body`
		return nil, errors.New(string(msg))
	}
	var v map[string]any
	err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&v)
	return v, err
}

// rebound is the rebind spelling of the fix: resp.Body itself is the
// limited reader by the time it is read. Quiet.
func rebound(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	resp.Body = io.NopCloser(io.LimitReader(resp.Body, 1<<20))
	return io.ReadAll(resp.Body)
}

// reboundMaxBytes rebinds through http.MaxBytesReader. Quiet.
func reboundMaxBytes(url string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	resp.Body = http.MaxBytesReader(nil, resp.Body, 1<<20)
	var v any
	return json.NewDecoder(resp.Body).Decode(&v)
}

// readBeforeRebind reads first and rebinds after: the read was raw.
func readBeforeRebind(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body) // want `unbounded read of an \*http\.Response body`
	resp.Body = io.NopCloser(io.LimitReader(resp.Body, 1<<20))
	return b, err
}

// reboundOther rebinds a DIFFERENT response's body: no credit here.
func reboundOther(url string, other *http.Response) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	other.Body = io.NopCloser(io.LimitReader(resp.Body, 1<<20))
	return io.ReadAll(resp.Body) // want `unbounded read of an \*http\.Response body`
}

// capResponse rebinds its parameter's body: a one-hop helper credit.
func capResponse(r *http.Response, n int64) {
	r.Body = io.NopCloser(io.LimitReader(r.Body, n))
}

// viaRebindHelper hands the response to the rebinding helper first.
// Quiet.
func viaRebindHelper(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	capResponse(resp, 1<<20)
	return io.ReadAll(resp.Body)
}

// readCapped reads its parameter under a limiter (the old one-hop
// credit). Passing resp.Body to it bounds THAT read, not the second,
// direct one.
func readCapped(r io.Reader) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, 1<<20))
}

func cappedThenRaw(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if _, err := readCapped(resp.Body); err != nil {
		return nil, err
	}
	return io.ReadAll(resp.Body) // want `unbounded read of an \*http\.Response body`
}
