package appgen

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/framework"
)

// clientSSECapTest drives the generated Go client's WatchPosts against a
// server that sends one event whose data lines join to exactly the cap,
// then one a byte over it. The first is delivered whole; the second ends
// the watch with an error before fn sees it.
const clientSSECapTest = `package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWatchBoundsEventData(t *testing.T) {
	piece := strings.Repeat("A", 64<<10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		var b strings.Builder
		for i := 0; i < 16; i++ {
			b.WriteString("data: " + piece + "\n")
		}
		b.WriteString("\n")
		for i := 0; i < 16; i++ {
			b.WriteString("data: " + piece + "\n")
		}
		b.WriteString("data: A\n\n")
		w.Write([]byte(b.String()))
	}))
	defer srv.Close()
	var got []int
	err := NewClient(srv.URL, nil).WatchPosts(context.Background(), func(_ string, data []byte) error {
		got = append(got, len(data))
		return nil
	})
	if len(got) != 1 || got[0] != maxBodyBytes {
		t.Errorf("delivered events of %v bytes, want one of exactly %d (the cap)", got, maxBodyBytes)
	}
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("an event a byte past the cap ended the watch with %v, want the cap error", err)
	}
}
`

// Property: every byte the generated clients buffer from a response is
// bounded, the SSE feed included, at the 1 MiB body cap counted in bytes.
// do() and the error snapshots read through the cap, but watch()'s event
// loop kept appending: the JS client's line buffer grew until a newline
// came and both clients joined an event's data lines until a blank line
// came, so a server that never sent one grew the subscriber's heap
// without bound. The JS bound then counted string length, UTF-16 code
// units, so text outside ASCII passed at up to three times the cap.
// Surfaces: generate_sdkjs.go jsClientMethods._sse (line buffer, event
// data), generate_client.go watchSSE (event data).
func TestSDKClientsBoundSSEEventData(t *testing.T) {
	t.Run("go", func(t *testing.T) {
		dir := t.TempDir()
		clitest.WriteTestFile(t, filepath.Join(dir, "go.mod"), "module example.com/client\n\ngo 1.21\n")
		clitest.WriteTestFile(t, filepath.Join(dir, "client.go"), renderClient(bodyCapFixtureDecls()))
		clitest.WriteTestFile(t, filepath.Join(dir, "sse_test.go"), clientSSECapTest)
		cmd := exec.Command("go", "test", "-count=1", ".")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOTOOLCHAIN=local")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("generated client: %v\n%s", err, out)
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
		// The endless streams yield 64 KiB chunks forever: one never ends
		// a line, the other never ends an event. The finite ones are text
		// of two-byte characters (é), sent as raw 64 KiB byte chunks so a
		// character can split across them: a line, and an event's data,
		// of exactly the cap in bytes passes and one byte more is refused.
		// Counted in string length they sit at about half the cap.
		script := `
import { Client } from "./client.js";
const enc = new TextEncoder();
function endless(piece) {
  let sent = 0;
  return new ReadableStream({
    pull(c) {
      sent += piece.length;
      if (sent > (64 << 20)) { c.close(); return; }
      c.enqueue(enc.encode(piece));
    },
  });
}
function finite(text) {
  const bytes = enc.encode(text);
  let at = 0;
  return new ReadableStream({
    pull(c) {
      if (at >= bytes.length) { c.close(); return; }
      c.enqueue(bytes.slice(at, at + (64 << 10)));
      at += 64 << 10;
    },
  });
}
const cap = 1 << 20;
const e = (n) => "é".repeat(n);
const dataLines = (tail) => Array.from({ length: 16 }, () => "data: " + e(32 << 10) + "\n").join("") + (tail ? "data: " + tail + "\n" : "") + "\n";
const big = "A".repeat(64 << 10);
const cases = [
  ["endless-line", () => endless(big)],
  ["endless-event", () => endless("data: " + big + "\n")],
  ["line-at-cap", () => finite("x" + e(cap / 2 - 1) + "x")],
  ["line-over-cap", () => finite("x" + e(cap / 2 - 1) + "xx")],
  ["data-at-cap", () => finite(dataLines(""))],
  ["data-over-cap", () => finite(dataLines("A"))],
];
for (const [name, body] of cases) {
  const api = new Client({ baseURL: "http://x", fetch: async () => new Response(body(), { status: 200 }) });
  const sizes = [];
  let outcome = "ended";
  try {
    await api.posts.watch((_, data) => { sizes.push(enc.encode(data).length); });
  } catch (err) {
    outcome = "refused";
  }
  console.log(name + ":" + outcome + (sizes.length ? "/" + sizes.join(",") : ""));
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
		want := "endless-line:refused endless-event:refused line-at-cap:ended line-over-cap:refused data-at-cap:ended/1048576 data-over-cap:refused"
		if got := strings.Join(strings.Fields(string(out)), " "); got != want {
			t.Errorf("JS watch() outcomes =\n  %s\nwant\n  %s", got, want)
		}
	})
}
