package analyzers_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// GOFASTR1407 exists because battery/auth decoded login and register
// bodies with encoding/json's default semantics (probe
// TestLoginJSONStrictTopLevelKeys, pre-fix 7bd789e9, fixed 4b7a25d2):
// stdlib keeps the last duplicate key and folds key case, so one smuggled
// body authenticated a different identity depending on Content-Type.
// Fixtures reduce that site to its shape, carry the strict-decode fix as
// the negative, and reduce the two later real sites: the harness
// mcpserver body replayed across files into Server.handle, and the
// control/ws frame decoded in Conn.handleText.

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

// The body ferried through a helper, anywhere in the package: io.ReadAll
// wrapped in a one-line function is the same stdlib decode of raw
// request bytes, whichever file of the package holds the call. The pass
// parses every file of the package, so the helper's return expression
// is visible at every call site; a helper whose return calls another
// helper is still not followed (one bounded level).
func TestRawJSONBodyDecodeThroughPkgHelper(t *testing.T) {
	ds := fixture(t, map[string]string{
		"update.go": `package cart

import (
	"encoding/json"
	"io"
	"net/http"
)

// readBody is a one-line helper, same file, fully visible.
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

// updateViaCrossFile: identical shape, the helper declared in another
// file of the same package. The mcpserver replay (below) is this
// ferry with a struct field in the middle.
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
	if got := rules(ds)[contracts.RuleRawJSONBodyDecode]; got != 2 {
		t.Errorf("expected both helper ferries to fire (same-file and cross-file are the same shape), got %d findings", got)
	}
}

// The harness mcpserver replay, reduced: http.go handlePOST buffers
// r.Body into bytes, wraps them in a bytes.Reader, and hands the reader
// to Server.WithIO in another file, which stores it in a field;
// Server.Serve scans that field line by line and Server.handle decodes
// each line with plain json.Unmarshal (probe
// TestHarnessMcpRedRejectsDuplicateKeys). The decode site has no
// *http.Request parameter: taint must follow the argument into
// WithIO, through the struct field, and along the scanner.
func TestRawJSONBodyDecodeThroughReplayFerry(t *testing.T) {
	ds := fixture(t, map[string]string{
		"httpside.go": `package mcp

import (
	"bytes"
	"io"
	"net/http"
)

func handlePOST(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 4096))
	if err != nil {
		return
	}
	in := bytes.NewReader(append(bytes.TrimSpace(raw), '\n'))
	s := &server{}
	s.WithIO(in)
	_ = s.Serve()
}
`,
		"server.go": `package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
)

type server struct {
	in io.Reader
}

func (s *server) WithIO(in io.Reader) *server {
	s.in = in
	return s
}

func (s *server) Serve() error {
	scanner := bufio.NewScanner(s.in)
	for scanner.Scan() {
		line := scanner.Bytes()
		s.handle(context.Background(), line)
	}
	return nil
}

func (s *server) handle(_ context.Context, line []byte) {
	var req struct {
		Method string ` + "`json:\"method\"`" + `
	}
	if err := json.Unmarshal(line, &req); err != nil {
		return
	}
}
`,
	})
	d := assertHas(t, ds, contracts.RuleRawJSONBodyDecode)
	if !strings.HasSuffix(d.File, "server.go") {
		t.Errorf("finding should sit at the decode in server.go, got %s", d.File)
	}
	if d.Evidence["source"] != "request body" {
		t.Errorf("source should name the request body, got %q", d.Evidence["source"])
	}
}

// The harness control/ws frame, reduced: Conn.run reads a frame with
// the repo's own reader (Conn.readFrame) and hands the payload to
// Conn.handleText, whose json.Unmarshal never sees an *http.Request
// (probe TestHarnessWsRedRejectsDuplicateKeys). Frame bytes are as
// client-controlled as a body, so the frame read is a second taint
// source. A waived twin that receives the same frame stays quiet, and
// a decode of bytes nobody ever taints stays quiet with it.
func TestRawJSONBodyDecodeOnWebsocketFrame(t *testing.T) {
	ds := fixture(t, map[string]string{
		"frame.go": `package ws

import "encoding/json"

type frame struct {
	Frame string          ` + "`json:\"frame\"`" + `
	Body  json.RawMessage ` + "`json:\"body\"`" + `
}

type conn struct{}

func (c *conn) readFrame() (op byte, payload []byte, err error) {
	payload = make([]byte, 4)
	return 1, payload, nil
}

func (c *conn) run() {
	op, payload, err := c.readFrame()
	if err != nil {
		return
	}
	_ = op
	c.handleText(payload)
	c.handleTextAllowed(payload)
}

func (c *conn) handleText(payload []byte) {
	var f frame
	if err := json.Unmarshal(payload, &f); err != nil {
		return
	}
}

func (c *conn) handleTextAllowed(payload []byte) {
	var f frame
	//gofastr:allow(GOFASTR1407) control-protocol envelope: the whole object is the frame, no field is an identity
	if err := json.Unmarshal(payload, &f); err != nil {
		return
	}
}

func decodeStored(b []byte) error {
	var f frame
	return json.Unmarshal(b, &f)
}
`,
	})
	d := assertHas(t, ds, contracts.RuleRawJSONBodyDecode)
	if got := rules(ds)[contracts.RuleRawJSONBodyDecode]; got != 1 {
		t.Errorf("expected exactly the unwaived frame decode to fire, got %d findings", got)
	}
	if d.Evidence["source"] != "websocket frame" {
		t.Errorf("source should name the websocket frame, got %q", d.Evidence["source"])
	}
}

// The fix posture: a strict top-level key walk is a decoder used only
// for Token/More iteration and Decode into a json.RawMessage skip value
// (battery/auth rejectAmbiguousTopLevelKeys, crud checkEnvelopeKeys,
// reduced here into another file). Package-wide taint reaches its
// bytes and it still stays quiet: the walk examines keys, it never
// binds them onto fields. The Unmarshal of the vetted bytes is the
// allow directive's job — the unwaived twin without a walk fires.
func TestRawJSONBodyKeyWalkStaysQuiet(t *testing.T) {
	ds := fixture(t, map[string]string{
		"walk.go": `package keys

import (
	"encoding/json"
	"io"
	"net/http"
)

type creds struct {
	Email    string ` + "`json:\"email\"`" + `
	Password string ` + "`json:\"password\"`" + `
}

func login(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
	if err != nil {
		return
	}
	if err := rejectAmbiguous(body); err != nil {
		return
	}
	//gofastr:allow(GOFASTR1407) rejectAmbiguous above already refused duplicate and case-folded keys; this Unmarshal only decodes a vetted body
	if err := json.Unmarshal(body, &creds{}); err != nil {
		return
	}
}

func loginUnvetted(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
	if err != nil {
		return
	}
	var c creds
	if err := json.Unmarshal(body, &c); err != nil {
		return
	}
}
`,
		"walker.go": `package keys

import (
	"bytes"
	"encoding/json"
)

func rejectAmbiguous(body []byte) error {
	dec := json.NewDecoder(bytes.NewReader(body))
	if _, err := dec.Token(); err != nil {
		return err
	}
	for dec.More() {
		if _, err := dec.Token(); err != nil {
			return err
		}
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return err
		}
	}
	return nil
}
`,
	})
	d := assertHas(t, ds, contracts.RuleRawJSONBodyDecode)
	if !strings.Contains(d.Message, "request body") {
		t.Errorf("the unvetted handler should be the one finding, got %q", d.Message)
	}
	if got := rules(ds)[contracts.RuleRawJSONBodyDecode]; got != 1 {
		t.Errorf("expected only the unvetted decode to fire, got %d findings", got)
	}
}

// The wrapped-reader swap on the request itself (r.Body =
// http.MaxBytesReader(...), the repo's standard idiom in battery/auth,
// framework/crud, kiln/chat) must not poison the FIELD name Body for
// the package: an outbound resp.Body decoded in the same package is
// still the provider's response, never a request body. Pinned after
// the package-wide field taint briefly fired every oauth2/oidc
// token-exchange response decode in battery/auth.
func TestRawJSONBodySwapDoesNotTaintRespBody(t *testing.T) {
	ds := fixture(t, map[string]string{
		"exchange.go": `package oauth

import (
	"encoding/json"
	"io"
	"net/http"
)

func login(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return
	}
	var env struct {
		Code string ` + "`json:\"code\"`" + `
	}
	//gofastr:allow(GOFASTR1407) envelope transport, no identity field
	_ = json.Unmarshal(body, &env)
}

func fetchToken(client *http.Client, url string) error {
	resp, err := client.Post(url, "application/json", nil)
	if err != nil {
		return err
	}
	var tok struct {
		AccessToken string ` + "`json:\"access_token\"`" + `
	}
	return json.NewDecoder(resp.Body).Decode(&tok)
}
`,
	})
	assertNot(t, ds, contracts.RuleRawJSONBodyDecode,
		"the swap idiom is the request's own reader, and resp.Body is the provider's response, not this request's body")
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
