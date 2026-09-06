package main

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// Pins the uncapped response-body reads and the redirect-following default
// client in the GENERATED typed client, found by the 2026-09-04 red-probe
// round; fixed by wrapping every body read in
// io.LimitReader(resp.Body, maxBodyBytes) (1 MiB) before ReadAll/decode
// and building NewClient's default with CheckRedirect returning
// http.ErrUseLastResponse.
// Family: unbounded response-body decode on credential-bearing fetches +
// F2 redirect re-check, in the generated typed client.
// Property: every response body the emitted entities/client buffers — the
// 2xx JSON decode and the non-2xx error snapshot — is size-bounded before
// buffering, and the client's own default http.Client never follows a
// redirect while requests carry the bearer token.
// Surfaces: cmd/gofastr/generate_client.go — the renderClient template.
// Its output is the client every generated app embeds as
// entities/client/client.go AND the standalone Go SDK from
// `gofastr generate sdk` (generate_sdk.go reuses renderClient verbatim).

// TestEmittedClientBoundsEveryBodyDecode renders the client and asserts
// no line touches resp.Body without a size bound.
func TestEmittedClientBoundsEveryBodyDecode(t *testing.T) {
	out := renderClient(bodyCapFixtureDecls())
	for i, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, "resp.Body") {
			continue
		}
		switch {
		case strings.Contains(line, "io.LimitReader(resp.Body,"),
			strings.Contains(line, "bufio.NewScanner(resp.Body)"),
			strings.Contains(line, "resp.Body.Close()"):
			// bounded or lifecycle-only
		default:
			t.Errorf("emitted client touches resp.Body with no size bound (line %d):\n%s", i+1, strings.TrimSpace(line))
		}
	}
	if strings.Contains(out, "io.ReadAll(resp.Body)") {
		t.Error("emitted client io.ReadAll's resp.Body uncapped")
	}
	if strings.Contains(out, "json.NewDecoder(resp.Body)") {
		t.Error("emitted client decodes resp.Body uncapped")
	}
}

// TestEmittedClientDefaultRefusesRedirect asserts NewClient's built-in
// default does not follow redirects (the bearer token rides every
// request when Token is set).
func TestEmittedClientDefaultRefusesRedirect(t *testing.T) {
	out := renderClient(bodyCapFixtureDecls())
	if !strings.Contains(out, "http.ErrUseLastResponse") {
		t.Error("emitted NewClient default follows redirects; requests carry Authorization: Bearer <Token> and a 3xx would re-send the token to whatever origin the response names")
	}
}

// bodyCapFixtureDecls is one plain CRUD entity: enough for renderClient
// to emit the full shared template (doJSON, doBatch, watchSSE, NewClient)
// the assertions walk.
func bodyCapFixtureDecls() []framework.EntityDeclaration {
	crud := true
	tsOff := false
	return []framework.EntityDeclaration{{
		Scope:      &framework.ScopeDeclaration{},
		Pagination: &framework.PaginationDeclaration{},
		Name:       "posts",
		Table:      "posts",
		Exposure:   &framework.ExposureDeclaration{CRUD: &crud},
		Timestamps: &tsOff,
		Fields: []framework.FieldDeclaration{
			{Name: "title", Type: "string", Required: true},
		},
	}}
}
