package appgen

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// Property: every byte the generated clients buffer from a response is
// bounded, the SSE feed included. do() and the error snapshots read
// through the 1 MiB cap, but watch()'s event loop kept appending: the JS
// client's line buffer grew until a newline came and both clients joined
// an event's data lines until a blank line came, so a server that never
// sent one grew the subscriber's heap without bound.
// Surfaces: generate_sdkjs.go jsClientMethods._sse (line buffer, event
// data), generate_client.go watchSSE (event data).
func TestSDKClientsBoundSSEEventData(t *testing.T) {
	t.Run("go", func(t *testing.T) {
		src := renderClient(bodyCapFixtureDecls())
		loop := src[strings.Index(src, "func (c *Client) watchSSE("):]
		loop = loop[:strings.Index(loop, "\n}\n")]
		if !strings.Contains(loop, "len(data)+") || !strings.Contains(loop, "maxBodyBytes") {
			t.Errorf("watchSSE appends every data line to the event with no bound:\n%s", loop)
		}
	})
	t.Run("js", func(t *testing.T) {
		node, err := exec.LookPath("node")
		if err != nil {
			t.Skip("node is not installed")
		}
		decl := framework.EntityDeclaration{
			Name:   "posts",
			Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}},
		}
		spec := sdkSpec{
			App: "app", SDKVersion: "0.0.0", GofastrVersion: "dev", Module: "local/app-sdk",
			Decls:    []framework.EntityDeclaration{decl},
			Entities: []cliEntity{buildEntityModel(decl, cliVerbs)},
		}
		dir := t.TempDir()
		for _, f := range renderSDKJSFiles(spec) {
			if err := os.WriteFile(filepath.Join(dir, f.Name), []byte(f.Content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		// Each fake stream yields 64 KiB chunks forever: one never ends a
		// line, the other never ends an event.
		script := `
import { Client } from "./client.js";
const chunk = (s) => new TextEncoder().encode(s);
function feed(piece) {
  let sent = 0;
  return new ReadableStream({
    pull(c) {
      sent += piece.length;
      if (sent > (64 << 20)) { c.close(); return; }
      c.enqueue(chunk(piece));
    },
  });
}
const big = "A".repeat(64 << 10);
for (const [name, piece] of [["line", big], ["event", "data: " + big + "\n"]]) {
  const api = new Client({ baseURL: "http://x", fetch: async () => new Response(feed(piece), { status: 200 }) });
  let outcome = "unbounded";
  try {
    await api.posts.watch(() => {});
  } catch (e) {
    outcome = "refused";
  }
  console.log(name + ":" + outcome);
}
`
		if err := os.WriteFile(filepath.Join(dir, "sse.mjs"), []byte(script), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(node, "sse.mjs")
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("node: %v\n%s", err, out)
		}
		if got := strings.Fields(string(out)); strings.Join(got, " ") != "line:refused event:refused" {
			t.Errorf("JS watch() over a stream that never ends a line or an event = %q, want both refused at the cap", got)
		}
	})
}
