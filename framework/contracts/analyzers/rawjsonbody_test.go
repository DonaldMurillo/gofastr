package analyzers_test

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// GOFASTR1407 exists because battery/auth decoded login and register
// bodies with encoding/json's default semantics (probe
// TestLoginJSONStrictTopLevelKeys, pre-fix 7bd789e9, fixed 4b7a25d2):
// stdlib keeps the last duplicate key and folds key case, so one smuggled
// body authenticated a different identity depending on Content-Type.
// Fixtures reduce that site to its shape, carry the strict-decode fix as
// the negative, and add two positives that never existed in this repo.

// The pre-fix decode, reduced: json.NewDecoder on r.Body inside a
// function that takes the request.
func TestRawJSONBodyDecodeIsReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"json_limit.go": `package auth

import (
	"encoding/json"
	"errors"
	"net/http"
)

func decodeJSONLimited(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			return false
		}
		return false
	}
	return true
}
`,
	})
	d := assertHas(t, ds, contracts.RuleRawJSONBodyDecode)
	if d.Line == 0 {
		t.Errorf("finding is not positioned on the decode: %+v", d)
	}
}

// The fixed call site: decodeAuthCredentials goes through the strict
// walker, so the handler itself decodes nothing.
func TestStrictDecodeCallSiteIsQuiet(t *testing.T) {
	ds := fixture(t, map[string]string{
		"form_decode.go": `package auth

import "net/http"

func decodeAuthCredentials(w http.ResponseWriter, r *http.Request) (email, password string, ok bool) {
	var body struct {
		Email    string ` + "`json:\"email\"`" + `
		Password string ` + "`json:\"password\"`" + `
	}
	if !decodeJSONLimitedStrict(w, r, &body, "email", "password") {
		return "", "", false
	}
	return body.Email, body.Password, true
}
`,
	})
	assertNot(t, ds, contracts.RuleRawJSONBodyDecode,
		"decodeJSONLimitedStrict is the fixed spelling: the strict walk precedes the Unmarshal")
}

// Two positives with no counterpart in this repo: Unmarshal on bytes read
// straight from the body, and the two-hop shape where the body is wrapped
// by MaxBytesReader and then read.
func TestRawJSONBodyDecodeFiresOnUnrelatedSites(t *testing.T) {
	ds := fixture(t, map[string]string{
		"guestbook/handler.go": `package guestbook

import (
	"encoding/json"
	"io"
	"net/http"
)

func signHandler(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return
	}
	var entry struct {
		Name string ` + "`json:\"name\"`" + `
	}
	json.Unmarshal(body, &entry)
	_ = entry
}
`,
		"cart/update.go": `package cart

import (
	"encoding/json"
	"io"
	"net/http"
)

func update(req *http.Request) error {
	limited := http.MaxBytesReader(nil, req.Body, 4096)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return err
	}
	var cart struct {
		SKU string ` + "`json:\"sku\"`" + `
	}
	return json.Unmarshal(raw, &cart)
}
`,
	})
	assertHas(t, ds, contracts.RuleRawJSONBodyDecode)
	if got := rules(ds)[contracts.RuleRawJSONBodyDecode]; got != 2 {
		t.Errorf("expected both synthetic sites to fire, got %d findings", got)
	}
}

// Pass-through reader constructors keep the body's bytes: a decoder
// over a buffered, NopCloser-wrapped, or tee'd body is still decoding
// the raw request. Constructors that return their own output
// (peer.CallWithID) stay excluded — that is the distinction the
// allow-list encodes.
func TestRawJSONBodyDecodeThroughReaderConstructorsIsReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"wrapped.go": `package wrapped

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
)

func decodeBuffered(r *http.Request) error {
	var v map[string]any
	return json.NewDecoder(bufio.NewReader(r.Body)).Decode(&v)
}

func decodeNopCloser(r *http.Request) error {
	var v map[string]any
	return json.NewDecoder(io.NopCloser(r.Body)).Decode(&v)
}

func decodeTee(r *http.Request, w io.Writer) error {
	var v map[string]any
	return json.NewDecoder(io.TeeReader(r.Body, w)).Decode(&v)
}
`,
	})
	assertHas(t, ds, contracts.RuleRawJSONBodyDecode)
	if got := rules(ds)[contracts.RuleRawJSONBodyDecode]; got != 3 {
		t.Errorf("expected the buffered, NopCloser, and TeeReader spellings to fire, got %d findings", got)
	}
}

// The body ferried through a helper: io.ReadAll wrapped in a one-line
// local function is the same stdlib decode of raw request bytes — the
// pass parses this file, so the helper's return is visible. One bounded
// level of same-file tracking; a helper in another file stays invisible
// (documented silence), because the pass never loads another file's
// bodies.
func TestRawJSONBodyDecodeThroughSameFileHelperIsReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"update.go": `package cart

import (
	"encoding/json"
	"io"
	"net/http"
)

// readBody is a one-line local helper, same file, fully visible.
func readBody(r *http.Request) ([]byte, error) {
	return io.ReadAll(r.Body)
}

// updateViaHelper: the battery/auth bug with io.ReadAll wrapped in the
// local helper above. json.Unmarshal still decodes raw request bytes
// with stdlib semantics.
func updateViaHelper(w http.ResponseWriter, r *http.Request) {
	raw, err := readBody(r)
	if err != nil {
		return
	}
	var cart struct {
		SKU string ` + "`json:\"sku\"`" + `
	}
	json.Unmarshal(raw, &cart)
}
`,
		"caller.go": `package cart

import (
	"encoding/json"
	"net/http"
)

// updateViaCrossFile: identical shape, but the helper lives in another
// file of the same package — invisible to a per-file pass, and the
// documented silence.
func updateViaCrossFile(w http.ResponseWriter, r *http.Request) {
	raw, err := readBodyCrossFile(r)
	if err != nil {
		return
	}
	var note struct {
		ID int ` + "`json:\"id\"`" + `
	}
	json.Unmarshal(raw, &note)
}
`,
		"helper.go": `package cart

import (
	"io"
	"net/http"
)

func readBodyCrossFile(r *http.Request) ([]byte, error) {
	return io.ReadAll(r.Body)
}
`,
	})
	assertHas(t, ds, contracts.RuleRawJSONBodyDecode)
	if got := rules(ds)[contracts.RuleRawJSONBodyDecode]; got != 1 {
		t.Errorf("expected exactly the same-file ferry to fire (got %d findings): the cross-file ferry is the documented silence", got)
	}
}

// Posture pinned by the whole-repo run: the module proxy buffers the
// request body into params via a same-file helper, but the bytes it
// decodes afterwards are the child module's RESPONSE (peer.CallWithID),
// not the request body — taint does not flow through an arbitrary call
// that merely receives tainted arguments, only through reader wrappers
// (ReadAll/MaxBytesReader/LimitReader) and the same-file helper ferry.
func TestRawJSONBodyDecodeDoesNotTaintThroughArbitraryCalls(t *testing.T) {
	ds := fixture(t, map[string]string{
		"proxy.go": `package proxy

import (
	"encoding/json"
	"io"
	"net/http"
)

func buildParams(r *http.Request) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r.Body, 1<<20))
}

func serve(w http.ResponseWriter, r *http.Request) {
	params, err := buildParams(r)
	if err != nil {
		return
	}
	raw, err := peer.Call(r.Context(), params)
	if err != nil {
		return
	}
	var res struct {
		Status int ` + "`json:\"status\"`" + `
	}
	json.Unmarshal(raw, &res)
}
`,
	})
	assertNot(t, ds, contracts.RuleRawJSONBodyDecode,
		"raw is the module's response; the body only shaped the request params")
}

// The documented silences: core/handler owns the strict binder and decodes
// raw bytes by design; an outbound response body is not a request body; a
// function with no *http.Request parameter never sees one; _test.go is
// exempt; and the allow directive records a deliberate transport decision.
func TestRawJSONBodyDecodeStaysQuiet(t *testing.T) {
	ds := fixture(t, map[string]string{
		"core/handler/bind.go": `package handler

import (
	"encoding/json"
	"net/http"
)

func Bind(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}
`,
		"outbound.go": `package main

import (
	"encoding/json"
	"net/http"
)

func callAPI(client *http.Client) error {
	resp, err := client.Get("https://upstream.example/v1")
	if err != nil {
		return err
	}
	var out struct {
		Kind string ` + "`json:\"kind\"`" + `
	}
	return json.NewDecoder(resp.Body).Decode(&out)
}

func decodeStored(payload []byte, v any) error {
	return json.Unmarshal(payload, v)
}
`,
		"envelope_test.go": `package envelope

import (
	"encoding/json"
	"net/http"
)

func probe(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID int ` + "`json:\"id\"`" + `
	}
	json.NewDecoder(r.Body).Decode(&body)
}
`,
		"transport.go": `package transport

import (
	"encoding/json"
	"io"
	"net/http"
)

func handle(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var req struct {
		Method string ` + "`json:\"method\"`" + `
	}
	//gofastr:allow(GOFASTR1407) JSON-RPC envelope transport: the whole object is the protocol unit, no field is an identity
	json.Unmarshal(raw, &req)
	_ = req
}
`,
	})
	assertNot(t, ds, contracts.RuleRawJSONBodyDecode,
		"core/handler, outbound bodies, functions without a request, _test.go, and an allowed transport are all documented silences")
}
