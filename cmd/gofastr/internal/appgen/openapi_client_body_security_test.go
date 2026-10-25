package appgen

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
