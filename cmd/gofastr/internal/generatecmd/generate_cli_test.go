package generatecmd

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

func defaultCLIOptions() appgen.CliOptions {
	return appgen.CliOptions{OutDir: "cli", Binary: "myapp", ApiPrefix: "api"}
}

// cliTempModule scaffolds a temp Go module with a two-entity project
// (generateProject at the module root) and returns its dir. The module
// replaces gofastr with the repo so generated code builds offline.
func cliTempModule(t *testing.T) string {
	return cliModuleFromYAML(t, `app:
  name: myapp
entities:
  - name: posts
    crud: true
    fields:
      - name: title
        type: string
        required: true
      - name: views
        type: int
      - name: published
        type: bool
  - name: documents
    crud: true
    soft_delete: true
    search_fields: [title]
    fields:
      - name: title
        type: string
        required: true
      - name: body
        type: text
`)
}

// cliModuleFromYAML scaffolds a temp Go module whose project comes from the
// given gofastr.yml (generateProject at the module root) and returns its
// dir. It chdirs into the module; t.Cleanup restores the working directory.
// The module replaces gofastr with the repo so generated code builds offline.
func cliModuleFromYAML(t *testing.T, yaml string) string {
	t.Helper()
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	goVersion, err := clitest.RepoGoVersion(repoRoot)
	if err != nil {
		t.Fatalf("repoGoVersion: %v", err)
	}
	goMod := "module example.com/myapp\n\ngo " + goVersion + "\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => " + repoRoot + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := clitest.CopyGoSum(repoRoot, dir); err != nil {
		t.Fatalf("copy go.sum: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gofastr.yml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(oldWD) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	appgen.RunGenerateApp([]string{"gofastr.yml"})
	return dir
}

// End-to-end compile: generate the project, then the CLI, and `go build` it.
func TestGenerateCLI_BuildsCleanly(t *testing.T) {
	dir := cliTempModule(t)
	appgen.RunGenerateCLI([]string{"--binary=myapp"})

	for _, f := range []string{"main.gen.go", "config.gen.go", "auth.gen.go", "output.gen.go", "custom.go", "posts.gen.go", "documents.gen.go"} {
		if _, err := os.Stat(filepath.Join(dir, "cmd", "myapp", f)); err != nil {
			t.Fatalf("cmd/myapp/%s not written: %v", f, err)
		}
	}
	cmd := exec.Command("go", "build", "-o", filepath.Join(dir, "myapp-bin"), "./cmd/myapp")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated CLI did not build: %v\n%s", err, out)
	}
}

// custom.go is the dev-owned seam: --force regen must not touch it, and its
// commands must merge over the generated set.
func TestGenerateCLI_CustomSeamSurvives(t *testing.T) {
	dir := cliTempModule(t)
	appgen.RunGenerateCLI([]string{"--binary=myapp"})

	customPath := filepath.Join(dir, "cmd", "myapp", "custom.go")
	custom := `package main

import (
	"fmt"

	client "example.com/myapp/entities/client"
)

// sentinel: owned-by-dev
func customCommands() []command {
	return []command{
		{name: "hello", summary: "custom command", run: func(args []string) int {
			fmt.Println("hello from custom.go")
			return 0
		}},
		{name: "login", summary: "replaced login", run: func(args []string) int {
			fmt.Println("login from custom.go")
			return 0
		}},
	}
}

func configureClient(c *client.Client) {}
`
	if err := os.WriteFile(customPath, []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}

	appgen.RunGenerateCLI([]string{"--binary=myapp", "--force"})

	after, err := os.ReadFile(customPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != custom {
		t.Fatalf("--force overwrote custom.go")
	}

	bin := clitest.TestExecutablePath(filepath.Join(dir, "myapp-cli"))
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/myapp")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("CLI with custom.go did not build: %v\n%s", err, out)
	}
	out, err := exec.Command(bin, "hello").CombinedOutput()
	if err != nil {
		t.Fatalf("custom command failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "hello from custom.go") {
		t.Fatalf("custom command not merged into dispatch: %s", out)
	}
	// A custom entry named like a built-in replaces it.
	out, err = exec.Command(bin, "login").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "login from custom.go") {
		t.Fatalf("custom login did not replace the built-in: %v\n%s", err, out)
	}
}

// The full loop: token-authenticated server in-process, generated CLI binary
// driven via os/exec. Covers login/config, every CRUD verb, batch, watch,
// table output, and the auth exit code on a revoked token.
func TestGenerateCLI_RoundTripLiveServer(t *testing.T) {
	dir := cliTempModule(t)
	appgen.RunGenerateCLI([]string{"--binary=myapp"})
	bin := clitest.TestExecutablePath(filepath.Join(dir, "myapp"))
	build := exec.Command("go", "build", "-o", bin, "./cmd/myapp")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, out)
	}

	// Server: posts entity (session-required by default) + token middleware.
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	// A pooled sqlite :memory: DSN gives every NEW connection a fresh,
	// EMPTY database. Under the concurrency this test creates (a
	// long-lived SSE watch plus parallel CLI calls) the pool opens a
	// second connection whose token lookup then hits "no such table:
	// auth_api_tokens" and the request silently degrades to anonymous:
	// the CLI 401s or never sees an event. One connection = one database.
	db.SetMaxOpenConns(1)
	defer db.Close()
	app := framework.NewApp(framework.WithDB(db), framework.WithoutDefaultMiddleware(),
		framework.WithAPIPrefix("/api"))
	app.Entity("posts", framework.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "views", Type: schema.Int},
			{Name: "published", Type: schema.Bool},
		},
	}.WithTimestamps(false))
	if err := framework.AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	ctx := context.Background()
	tokens, err := auth.NewSQLAPITokenStore(db)
	if err != nil {
		t.Fatal(err)
	}
	accounts, err := auth.NewSQLServiceAccountStore(db)
	if err != nil {
		t.Fatal(err)
	}
	sa := auth.NewServiceAccount("cli-e2e", []string{"admin"})
	if err := accounts.Create(ctx, sa); err != nil {
		t.Fatal(err)
	}
	plaintext, issued, err := auth.IssueToken(ctx, tokens, auth.TokenSpec{
		Name: "e2e", OwnerKind: "service", OwnerID: sa.ID, Scopes: []string{"posts:*"},
	})
	if err != nil {
		t.Fatal(err)
	}
	app.Use(auth.TokenMiddleware(nil, accounts, tokens))
	srv := httptest.NewServer(app.Router())
	defer srv.Close()

	home := t.TempDir()
	env := append(os.Environ(),
		"HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"MYAPP_URL="+srv.URL, "MYAPP_TOKEN="+plaintext)
	run := func(args ...string) (string, int) {
		cmd := exec.Command(bin, args...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		code := 0
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else if err != nil {
			t.Fatalf("run %v: %v\n%s", args, err, out)
		}
		return string(out), code
	}

	// help surfaces: root usage, bare entity group, verb --help; all exit 0;
	// an unknown subcommand exits 2 but still shows the group usage
	out, code := run("--help")
	if code != 0 || !strings.Contains(out, "posts list") {
		t.Fatalf("root help exited %d: %s", code, out)
	}
	if out, code = run("posts"); code != 0 || !strings.Contains(out, "posts watch") {
		t.Fatalf("bare entity group help exited %d: %s", code, out)
	}
	if out, code = run("posts", "list", "--help"); code != 0 || !strings.Contains(out, "-views-gt") {
		t.Fatalf("verb --help exited %d: %s", code, out)
	}
	if out, code = run("login", "--help"); code != 0 || !strings.Contains(out, "with-token") {
		t.Fatalf("login --help exited %d: %s", code, out)
	}
	if out, code = run("posts", "frobnicate"); code != 2 || !strings.Contains(out, "posts list") {
		t.Fatalf("unknown subcommand should exit 2 with group usage, got %d: %s", code, out)
	}

	// create via field flags
	out, code = run("posts", "create", "--title", "hello", "--views", "3")
	if code != 0 {
		t.Fatalf("create exited %d: %s", code, out)
	}
	var created map[string]any
	if err := json.Unmarshal([]byte(out), &created); err != nil {
		t.Fatalf("create output not JSON: %v\n%s", err, out)
	}
	id, _ := created["id"].(string)
	if id == "" || created["title"] != "hello" {
		t.Fatalf("create round-trip: %v", created)
	}

	// get
	if out, code = run("posts", "get", id); code != 0 || !strings.Contains(out, "hello") {
		t.Fatalf("get exited %d: %s", code, out)
	}

	// patch: explicit zero value must survive (presence-faithful body)
	if out, code = run("posts", "patch", id, "--views", "0", "--published"); code != 0 {
		t.Fatalf("patch exited %d: %s", code, out)
	}
	var patched map[string]any
	if err := json.Unmarshal([]byte(out), &patched); err != nil {
		t.Fatalf("patch output: %v\n%s", err, out)
	}
	if patched["views"] != float64(0) || patched["published"] != true {
		t.Fatalf("patch lost explicit zero/bool: %v", patched)
	}

	// update via --json
	if out, code = run("posts", "update", id, "--json", `{"title":"edited"}`); code != 0 || !strings.Contains(out, "edited") {
		t.Fatalf("update exited %d: %s", code, out)
	}

	// list with filter + table output
	if out, code = run("posts", "list", "--title", "edited", "-o", "table"); code != 0 {
		t.Fatalf("list exited %d: %s", code, out)
	}
	if !strings.Contains(out, "edited") || !strings.Contains(out, "title") {
		t.Fatalf("table output missing row/header: %s", out)
	}

	// batch-create + batch-delete
	if out, code = run("posts", "batch-create", "--json", `[{"title":"batch-row-1"},{"title":"batch-row-2"}]`); code != 0 {
		t.Fatalf("batch-create exited %d: %s", code, out)
	}
	var batch struct {
		Committed bool `json:"committed"`
		Results   []struct {
			Data map[string]any `json:"data"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(out), &batch); err != nil || !batch.Committed || len(batch.Results) != 2 {
		t.Fatalf("batch-create envelope: %v %s", err, out)
	}
	bid1, _ := batch.Results[0].Data["id"].(string)
	bid2, _ := batch.Results[1].Data["id"].(string)
	// ids straddling a flag: the trailing id must not be silently dropped
	// (--url is the value flag; the token rides MYAPP_TOKEN env, never argv)
	if out, code = run("posts", "batch-delete", bid1, "--url", srv.URL, bid2); code != 0 {
		t.Fatalf("batch-delete exited %d: %s", code, out)
	}
	if out, code = run("posts", "list", "-o", "table"); code != 0 || strings.Contains(out, "batch-row-") {
		t.Fatalf("batch-delete left rows behind (exit %d): %s", code, out)
	}

	// a rolled-back batch prints its envelope on stdout and exits 1
	out, code = run("posts", "batch-create", "--json", `[{"title":"ok"},{"views":1}]`)
	if code != 1 {
		t.Fatalf("rolled-back batch should exit 1, got %d: %s", code, out)
	}
	if !strings.Contains(out, `"committed": false`) {
		t.Fatalf("rollback envelope missing from stdout: %s", out)
	}

	// watch: subscribe, create until an event line arrives. The
	// subscription attaches asynchronously, so creates retry until one
	// lands inside the live window; the budget is generous because CI
	// runners under load have delivered the first event only after
	// several seconds. stderr is captured so a watch process that dies
	// immediately (empty stdout, fast EOF) explains itself instead of
	// failing as a bare timeout.
	wctx, wcancel := context.WithTimeout(ctx, 60*time.Second)
	defer wcancel()
	watch := exec.CommandContext(wctx, bin, "posts", "watch")
	watch.Env = env
	var watchStderr bytes.Buffer
	watch.Stderr = &watchStderr
	stdout, err := watch.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := watch.Start(); err != nil {
		t.Fatal(err)
	}
	lines := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		if scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()
	var eventLine string
poll:
	for i := 0; i < 100; i++ {
		run("posts", "create", "--title", "sse")
		select {
		case l, ok := <-lines:
			if ok {
				eventLine = l
				break poll
			}
			// Closed with no line: the watch process ended (EOF). No
			// more events can arrive, stop creating and report.
			break poll
		case <-time.After(500 * time.Millisecond):
		case <-wctx.Done():
			break poll
		}
	}
	wcancel()
	watchErr := watch.Wait()
	if !strings.Contains(eventLine, "entity.created") {
		t.Fatalf("watch produced no created event: %q (watch exit: %v, stderr: %s)",
			eventLine, watchErr, watchStderr.String())
	}

	// delete
	if out, code = run("posts", "delete", id); code != 0 {
		t.Fatalf("delete exited %d: %s", code, out)
	}

	// login stores the token; a run without env token uses the config file
	login := exec.Command(bin, "login", "--url", srv.URL, "--with-token")
	login.Env = env
	login.Stdin = bytes.NewBufferString(plaintext + "\n")
	if out, err := login.CombinedOutput(); err != nil {
		t.Fatalf("login: %v\n%s", err, out)
	}
	noTokenEnv := append(os.Environ(),
		"HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"MYAPP_URL="+srv.URL, "MYAPP_TOKEN=")
	stored := exec.Command(bin, "posts", "list")
	stored.Env = noTokenEnv
	if out, err := stored.CombinedOutput(); err != nil {
		t.Fatalf("list with stored token: %v\n%s", err, out)
	}

	// revoked token → auth exit code 4
	if err := tokens.Revoke(ctx, issued.ID, "service", sa.ID); err != nil {
		t.Fatal(err)
	}
	if out, code = run("posts", "list"); code != 4 {
		t.Fatalf("revoked token should exit 4, got %d: %s", code, out)
	}
}
