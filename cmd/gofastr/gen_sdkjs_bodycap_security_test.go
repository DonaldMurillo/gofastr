package main

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// Pins the uncapped response-body reads in the GENERATED JS SDK client,
// found by the 2026-09-05 red-probe round (round 4); fixed by reading
// every buffered body through Client._readBody, a reader loop capped at
// 1 MiB (the same cap the Go client enforces via io.LimitReader), used
// by both do() and _sse's error snapshot.
// Family: F19 resource exhaustion (unbounded allocation from an untrusted response)
// Property: every response body the emitted JS SDK client buffers — the 2xx
// parse and the non-2xx error snapshot — must be size-bounded before it is
// buffered, matching the 1 MiB cap the Go client has carried since the
// 2026-09-04 fix (TestEmittedClientBoundsEveryBodyDecode).
// Surfaces: cmd/gofastr/generate_sdkjs.go::jsClientMethods (client.do's
// `await resp.text()` + JSON.parse, and client._sse's error-path
// `await resp.text()`), served to browsers by framework/sdkdocs at
// <base>/sdk/client.js; the same bytes ship in dist/sdk-js.zip.
// Threat: a hostile or misbehaving server (a compromised upstream, a
// response-splitting proxy, a list endpoint under an oversized limit)
// answers huge; JSON.parse then doubles the buffer and a Node consumer
// of the zip OOMs on a single response.
func TestJSClientBoundsEveryBodyRead(t *testing.T) {
	decl := framework.EntityDeclaration{
		Name:   "posts",
		Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}},
	}
	spec := sdkSpec{
		App: "app", SDKVersion: "0.0.0", GofastrVersion: "dev", Module: "local/app-sdk",
		Decls:    []framework.EntityDeclaration{decl},
		Entities: []cliEntity{buildEntityModel(decl, cliVerbs)},
	}
	var js string
	for _, f := range renderSDKJSFiles(spec) {
		if f.name == "client.js" {
			js = f.content
		}
	}
	if js == "" {
		t.Fatal("renderSDKJSFiles emitted no client.js")
	}
	for i, line := range strings.Split(js, "\n") {
		if !strings.Contains(line, "resp.text(") && !strings.Contains(line, "resp.json(") {
			continue
		}
		t.Errorf("SECURITY: [sdkjs-bodycap] emitted client.js buffers the response body with no size bound (line %d):\n%s",
			i+1, strings.TrimSpace(line))
	}
}
