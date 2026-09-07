//go:build red

package main

// RED TEST — open finding, 2026-09-06/07 adversarial round 5, phase 2
// (family enumeration; tier T2).
// Pinned sibling: TestEmittedClientBoundsEveryBodyDecode
// (sdkclient_bodycap_security_test.go) states the property verbatim for the
// generated typed client — "every response body the emitted entities/client
// buffers ... is size-bounded before buffering" — but its fixture only
// renders renderClient (generate_client.go). The --from-openapi
// self-contained client template (renderCLISelfClient) escapes that pin.
// Property: every response body a generated client buffers — including the
// CLI self-client emitted by `gofastr generate cli --from-openapi` — is
// size-bounded before ReadAll/decode.
// Surfaces: cmd/gofastr/generate_cli_openapi_render.go::renderCLISelfClient
// — the emitted DoRaw transport does `data, err := io.ReadAll(resp.Body)`
// (~line 140) with no bound. Every --from-openapi operation funnels through
// Do → DoRaw (binary and non-JSON responses go through DoRaw directly), and
// the file is emitted verbatim as internal/client/client.go into the
// generated CLI tree. Probe-verified: exactly one unbounded resp.Body line
// in the template; the same line ships in every emitted CLI.
// Finding: whatever answers the CLI's requests (the spec'd server, or
// anything that can interpose on its URL) pins unbounded memory in the CLI
// process per request — the exact shape the round-4 pin closed for the
// typed client.
// Fix direction: mirror the typed client — wrap the read in
// io.LimitReader(resp.Body, maxBodyBytes) (1 MiB) before io.ReadAll and
// error past the cap, in the DoRaw template.

import (
	"strings"
	"testing"
)

// TestSelfClientRedBodiesBounded renders the --from-openapi self-client and
// walks every resp.Body line with the pinned sibling's grammar: each must
// be LimitReader-wrapped, Scanner-based, or Close-only. A control leg
// re-runs the same grammar on renderClient (the round-4 pinned surface) so
// the walk can only fire on the self-client template.
func TestSelfClientRedBodiesBounded(t *testing.T) {
	out := renderCLISelfClient(selfClientRedFixtureSpec())
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
			t.Errorf("SECURITY: [openapi-cli-doraw-unbounded] emitted self-client touches resp.Body with no size bound (line %d):\n%s — every --from-openapi operation funnels through this DoRaw, and it ships as internal/client/client.go; mirror the typed client's io.LimitReader(resp.Body, maxBodyBytes) (1 MiB)", i+1, strings.TrimSpace(line))
		}
	}
	if strings.Contains(out, "io.ReadAll(resp.Body)") {
		t.Error("SECURITY: [openapi-cli-doraw-unbounded] emitted self-client io.ReadAll's resp.Body uncapped")
	}
	if strings.Contains(out, "json.NewDecoder(resp.Body)") {
		t.Error("SECURITY: [openapi-cli-doraw-unbounded] emitted self-client decodes resp.Body uncapped")
	}

	// Control: the pinned typed-client template still passes the same walk.
	ctl := renderClient(bodyCapFixtureDecls())
	for i, line := range strings.Split(ctl, "\n") {
		if !strings.Contains(line, "resp.Body") {
			continue
		}
		switch {
		case strings.Contains(line, "io.LimitReader(resp.Body,"),
			strings.Contains(line, "bufio.NewScanner(resp.Body)"),
			strings.Contains(line, "resp.Body.Close()"):
			// bounded or lifecycle-only
		default:
			t.Errorf("control broken: pinned renderClient regressed (line %d):\n%s", i+1, strings.TrimSpace(line))
		}
	}
}

// selfClientRedFixtureSpec is the minimal --from-openapi spec: one GET
// operation with a path param, self-client mode on. renderCLISelfClient's
// template is static apart from names, so this exercises every line the
// assertions walk.
func selfClientRedFixtureSpec() cliSpec {
	return cliSpec{
		Binary:      "demo",
		EnvPrefix:   "DEMO",
		APIPrefix:   "",
		SelfClient:  true,
		DefaultURL:  "http://127.0.0.1:8080",
		TokenHeader: "Authorization",
		TokenPrefix: "Bearer ",
		Ops: []cliOp{{
			ID:           "getImage",
			GoName:       "GetImage",
			Command:      "get-image",
			Summary:      "Fetch one image",
			Method:       "GET",
			PathTemplate: "/images/{id}",
			PathParams: []cliOpParam{{
				Name:     "id",
				Flag:     "id",
				GoType:   "string",
				Required: true,
				In:       "path",
			}},
		}},
	}
}
