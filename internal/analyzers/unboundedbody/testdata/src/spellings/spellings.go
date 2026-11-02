// Package spellings holds the uncapped request-body reads the
// 2026-11-02 guard bash found unboundedbody blind to: the deprecated
// ioutil.ReadAll, io.CopyBuffer, ReadFrom on a buffer, the body read
// through bufio.NewReader, and a local alias of r.Body.
package spellings

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"io/ioutil"
	"net/http"
)

func Deprecated(w http.ResponseWriter, r *http.Request) {
	b, _ := ioutil.ReadAll(r.Body) // want `reads an inbound request body with no size cap`
	w.Write(b)
}

func CopyBuffer(w http.ResponseWriter, r *http.Request) {
	var buf bytes.Buffer
	io.CopyBuffer(&buf, r.Body, make([]byte, 512)) // want `reads an inbound request body with no size cap`
	w.Write(buf.Bytes())
}

func ReadFrom(w http.ResponseWriter, r *http.Request) {
	var buf bytes.Buffer
	buf.ReadFrom(r.Body) // want `reads an inbound request body with no size cap`
	w.Write(buf.Bytes())
}

func Wrapped(w http.ResponseWriter, r *http.Request) {
	var v map[string]string
	json.NewDecoder(bufio.NewReader(r.Body)).Decode(&v) // want `reads an inbound request body with no size cap`
}

func Aliased(w http.ResponseWriter, r *http.Request) {
	body := r.Body
	b, _ := io.ReadAll(body) // want `reads an inbound request body with no size cap`
	w.Write(b)
}

// Silent: capped first.
func Capped(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var buf bytes.Buffer
	buf.ReadFrom(r.Body)
	w.Write(buf.Bytes())
}
