package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// cliMutationFixtureYAML is the field-type zoo behind the verb pinning
// golden: every flag kind the generator emits (string, int, float, bool,
// json), the string-flag spellings (text, enum with values, decimal,
// date), and a read_only field that must stay off the mutation flags.
const cliMutationFixtureYAML = `app:
  name: myapp
entities:
  - name: gadgets
    crud: true
    fields:
      - name: title
        type: string
        required: true
      - name: notes
        type: text
      - name: status
        type: enum
        values: [draft, shipped]
      - name: views
        type: int
      - name: rating
        type: float
      - name: price
        type: decimal
      - name: published
        type: bool
      - name: meta
        type: json
      - name: released_on
        type: date
      - name: sku
        type: string
        read_only: true
`

// cliMutationCases is the fixed argv set the golden pins: one entry per
// observable behaviour of the generated mutation verbs, in run order.
var cliMutationCases = []struct {
	name string
	argv []string
}{
	{"create help", []string{"gadgets", "create", "--help"}},
	// id verbs pop the positional id before flag parsing, so their help
	// text shows when the id precedes --help.
	{"update help", []string{"gadgets", "update", "rec-9", "--help"}},
	{"patch help", []string{"gadgets", "patch", "rec-9", "--help"}},
	{"create all flag kinds", []string{"gadgets", "create",
		"--title", "hello", "--notes", "second", "--status", "draft",
		"--views", "3", "--rating", "4.5", "--price", "19.99",
		"--published", "--meta", `{"a":[1,2]}`, "--released-on", "2026-01-02"}},
	{"create explicit zeros", []string{"gadgets", "create",
		"--views", "0", "--rating", "0", "--published=false"}},
	{"update path-escapes id", []string{"gadgets", "update", "a/b?x", "--title", "edited"}},
	{"patch keeps explicit zero", []string{"gadgets", "patch", "rec-9",
		"--views", "0", "--published"}},
	{"update via --json", []string{"gadgets", "update", "rec-9",
		"--json", `{"title":"edited","meta":{"k":1}}`}},
	{"rejects --json plus field flags", []string{"gadgets", "create",
		"--title", "x", "--json", `{}`}},
	{"rejects empty create", []string{"gadgets", "create"}},
	{"rejects bad json field value", []string{"gadgets", "create", "--meta", "nope"}},
	{"update without id", []string{"gadgets", "update"}},
	{"rejects unknown flag", []string{"gadgets", "create", "--nope"}},
	{"read-only field not a mutation flag", []string{"gadgets", "create", "--sku", "S1"}},
	{"batch-delete without ids", []string{"gadgets", "batch-delete"}},
	{"batch-delete flag after id", []string{"gadgets", "batch-delete", "--url", "http://127.0.0.1:9", "rec-1", "--json"}},
}

// TestCLIMutationVerbBytesStable pins the observable behaviour of the
// generated create/update/patch verbs: the full --help text (flag names,
// types, defaults, usage lines), the exact request line and JSON body each
// fixed argv sends, stdout, stderr, and the exit code. The bodies live in
// verbs.go behind a per-entity field table; this golden is the proof that
// restructuring changed no byte a customer or a server can see. It runs a
// throwaway recorder server, so no framework app is needed. Regenerate
// with GOFASTR_UPDATE_GOLDEN=1 after an INTENDED behaviour change, never
// to make a refactor pass.
func TestCLIMutationVerbBytesStable(t *testing.T) {
	// Resolve before cliModuleFromYAML chdirs into the temp module.
	goldenPath, err := filepath.Abs(filepath.Join("testdata", "cli_mutation_verbs.golden"))
	if err != nil {
		t.Fatal(err)
	}

	dir := cliModuleFromYAML(t, cliMutationFixtureYAML)
	runGenerateCLI([]string{"--binary=myapp"})
	bin := testExecutablePath(filepath.Join(dir, "myapp"))
	build := exec.Command("go", "build", "-o", bin, "./cmd/myapp")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, out)
	}

	var mu sync.Mutex
	var request string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		request = r.Method + " " + r.RequestURI + "\n" + strings.TrimRight(string(body), "\n")
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":{"id":"rec-1","title":"echo"}}`)
	}))
	defer srv.Close()

	home := t.TempDir()
	env := append(os.Environ(),
		"HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"MYAPP_URL="+srv.URL, "MYAPP_TOKEN=tok")

	var transcript strings.Builder
	for _, tc := range cliMutationCases {
		mu.Lock()
		request = ""
		mu.Unlock()
		cmd := exec.Command(bin, tc.argv...)
		cmd.Env = env
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		code := 0
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else if err != nil {
			t.Fatalf("run %v: %v\nstdout: %s\nstderr: %s", tc.argv, err, stdout.String(), stderr.String())
		}
		mu.Lock()
		req := request
		mu.Unlock()
		if req == "" {
			req = "(no request)"
		}
		fmt.Fprintf(&transcript, "== %s: %s (exit %d)\n-- request\n%s\n-- stdout\n%s\n-- stderr\n%s\n\n",
			tc.name, strings.Join(tc.argv, " "), code, req,
			strings.TrimRight(stdout.String(), "\n"), strings.TrimRight(stderr.String(), "\n"))
	}

	got := transcript.String()
	if os.Getenv("GOFASTR_UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run with GOFASTR_UPDATE_GOLDEN=1 to create): %v", err)
	}
	if got != string(want) {
		t.Fatalf("generated mutation-verb behaviour drifted from the golden:\n--- got ---\n%s--- want ---\n%s", got, want)
	}
}
