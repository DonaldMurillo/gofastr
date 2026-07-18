package main

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

	_ "github.com/mattn/go-sqlite3"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
)

// cliFixtureDecls is the entity set the render tests run against: one plain
// entity, one with search + soft-delete (whose list verb must gain -q and
// --trashed), and a custom table name.
func cliFixtureDecls() []framework.EntityDeclaration {
	return []framework.EntityDeclaration{
		{
			Name:  "posts",
			Table: "posts",
			Fields: []framework.FieldDeclaration{
				{Name: "title", Type: "string", Required: true},
				{Name: "views", Type: "int"},
				{Name: "published", Type: "bool"},
				{Name: "meta", Type: "json"},
			},
		},
		{
			Name:         "documents",
			Table:        "documents",
			SoftDelete:   true,
			SearchFields: []string{"title", "body"},
			Fields: []framework.FieldDeclaration{
				{Name: "title", Type: "string", Required: true},
				{Name: "body", Type: "text"},
				{Name: "published_at", Type: "timestamp"},
			},
		},
	}
}

func cliFixtureSpec(t *testing.T, opts cliOptions) cliSpec {
	t.Helper()
	spec, err := buildCLISpec(cliFixtureDecls(), opts, "example.com/app/entities/client")
	if err != nil {
		t.Fatalf("buildCLISpec: %v", err)
	}
	return spec
}

func renderedCLI(t *testing.T, opts cliOptions) map[string]string {
	t.Helper()
	files := renderCLIFiles(cliFixtureSpec(t, opts))
	out := map[string]string{}
	for _, f := range files {
		out[f.name] = f.content
	}
	return out
}

func defaultCLIOptions() cliOptions {
	return cliOptions{outDir: "cli", binary: "myapp", apiPrefix: "api"}
}

// Every selected entity gets a command file wiring its verbs into the
// dispatch table, and main.go registers the entity command groups.
func TestRenderCLI_CommandTreePerEntity(t *testing.T) {
	files := renderedCLI(t, defaultCLIOptions())

	main := files["main.go"]
	for _, w := range []string{
		"type command struct",
		"func customCommands() []command", // referenced, defined in custom.go
		`binaryName = "myapp"`,
		`envPrefix  = "MYAPP"`,
		`apiPrefix  = "/api"`,
	} {
		if !strings.Contains(main, w) && !strings.Contains(files["custom.go"], w) {
			t.Errorf("main.go/custom.go missing %q", w)
		}
	}

	posts := files["posts.go"]
	for _, w := range []string{
		`{name: "posts list"`,
		`{name: "posts get"`,
		`{name: "posts create"`,
		`{name: "posts update"`,
		`{name: "posts patch"`,
		`{name: "posts delete"`,
		`{name: "posts batch-create"`,
		`{name: "posts batch-update"`,
		`{name: "posts batch-delete"`,
		`{name: "posts watch"`,
		"func runPostsList(",
		"func runPostsCreate(",
	} {
		if !strings.Contains(posts, w) {
			t.Errorf("posts.go missing %q", w)
		}
	}
	if _, ok := files["documents.go"]; !ok {
		t.Errorf("documents.go not rendered")
	}
	for _, name := range []string{"config.go", "auth.go", "output.go", "custom.go"} {
		if _, ok := files[name]; !ok {
			t.Errorf("%s not rendered", name)
		}
	}
}

// List flags derive from the schema: eq flag per field, range flags only on
// numeric/date/timestamp fields, -q only with SearchFields, --trashed only
// with SoftDelete.
func TestRenderCLI_FlagsFromSchema(t *testing.T) {
	files := renderedCLI(t, defaultCLIOptions())

	posts := files["posts.go"]
	for _, w := range []string{
		`fs.String("title"`,
		`fs.String("views"`,
		`fs.String("views-gt"`,
		`fs.String("views-lte"`,
		`fs.String("title-like"`,
		`fs.String("sort"`,
		`fs.String("page"`,
		`fs.String("cursor"`,
		`fs.String("include"`,
		`fs.String("fields"`,
	} {
		if !strings.Contains(posts, w) {
			t.Errorf("posts.go list flags missing %q", w)
		}
	}
	for _, absent := range []string{`fs.String("q"`, `fs.Bool("trashed"`, `fs.String("title-gt"`, `fs.String("published-gt"`} {
		if strings.Contains(posts, absent) {
			t.Errorf("posts.go should not have %q", absent)
		}
	}

	docs := files["documents.go"]
	for _, w := range []string{
		`fs.String("q"`,
		`fs.Bool("trashed"`,
		`fs.String("published-at-gt"`,
	} {
		if !strings.Contains(docs, w) {
			t.Errorf("documents.go missing %q", w)
		}
	}
}

// A field whose flag name collides with a reserved flag (sort, json, page,
// url, …) must fail at generate time with the entity and field named — never
// emit a broken flag set.
func TestRenderCLI_ReservedFlagCollision(t *testing.T) {
	decls := []framework.EntityDeclaration{{
		Name:   "events",
		Table:  "events",
		Fields: []framework.FieldDeclaration{{Name: "sort", Type: "string"}},
	}}
	_, err := buildCLISpec(decls, defaultCLIOptions(), "example.com/app/entities/client")
	if err == nil {
		t.Fatal("expected reserved-flag collision error, got nil")
	}
	if !strings.Contains(err.Error(), "events") || !strings.Contains(err.Error(), "sort") {
		t.Errorf("error should name entity and field: %v", err)
	}
}

// The API prefix is baked into the client construction so customers pass a
// bare server URL.
func TestRenderCLI_BakesAPIPrefix(t *testing.T) {
	files := renderedCLI(t, defaultCLIOptions())
	if !strings.Contains(files["main.go"], `apiPrefix  = "/api"`) {
		t.Errorf("main.go should bake apiPrefix constant")
	}

	bare := renderedCLI(t, cliOptions{outDir: "cli", binary: "myapp", apiPrefix: ""})
	if !strings.Contains(bare["main.go"], `apiPrefix  = ""`) {
		t.Errorf("empty --api-prefix should bake an empty prefix")
	}
}

// --only / --exclude select entities; excluded entities render nothing.
func TestRenderCLI_EntityOnlyExclude(t *testing.T) {
	opts := defaultCLIOptions()
	opts.only = []string{"posts"}
	files := renderedCLI(t, opts)
	if _, ok := files["documents.go"]; ok {
		t.Errorf("--only=posts must not render documents.go")
	}

	opts = defaultCLIOptions()
	opts.exclude = []string{"posts"}
	files = renderedCLI(t, opts)
	if _, ok := files["posts.go"]; ok {
		t.Errorf("--exclude=posts must not render posts.go")
	}
	if _, ok := files["documents.go"]; !ok {
		t.Errorf("--exclude=posts must keep documents.go")
	}

	opts = defaultCLIOptions()
	opts.only = []string{"nope"}
	if _, err := buildCLISpec(cliFixtureDecls(), opts, "x"); err == nil {
		t.Errorf("--only naming an unknown entity must error")
	}
}

// --verbs allow-lists verbs, globally or per entity
// ("posts=list,get;documents=*").
func TestRenderCLI_VerbAllowList(t *testing.T) {
	opts := defaultCLIOptions()
	opts.verbs = "list,get"
	files := renderedCLI(t, opts)
	posts := files["posts.go"]
	if !strings.Contains(posts, `{name: "posts list"`) || !strings.Contains(posts, `{name: "posts get"`) {
		t.Errorf("global --verbs should keep list/get")
	}
	for _, absent := range []string{`{name: "posts create"`, `{name: "posts watch"`, "func runPostsCreate("} {
		if strings.Contains(posts, absent) {
			t.Errorf("global --verbs=list,get should drop %q", absent)
		}
	}

	opts = defaultCLIOptions()
	opts.verbs = "posts=list;documents=*"
	files = renderedCLI(t, opts)
	if strings.Contains(files["posts.go"], `{name: "posts get"`) {
		t.Errorf("per-entity verbs: posts should only have list")
	}
	if !strings.Contains(files["documents.go"], `{name: "documents watch"`) {
		t.Errorf("per-entity verbs: documents=* should keep watch")
	}

	opts = defaultCLIOptions()
	opts.verbs = "posts=frobnicate"
	if _, err := buildCLISpec(cliFixtureDecls(), opts, "x"); err == nil {
		t.Errorf("unknown verb must error at generate time")
	}
}

// A delete-only (or any narrow) verb selection must still emit the imports
// its code uses — needsHTTP/needsURLValues are per-verb, and format.Source
// cannot add missing imports.
func TestRenderCLI_NarrowVerbImports(t *testing.T) {
	opts := defaultCLIOptions()
	opts.verbs = "delete"
	files := renderedCLI(t, opts)
	posts := files["posts.go"]
	for _, w := range []string{`"net/http"`, `"net/url"`, "url.PathEscape(id)"} {
		if !strings.Contains(posts, w) {
			t.Errorf("delete-only posts.go missing %q\n%s", w, posts)
		}
	}
}

// Positional ids are path-escaped in every id-addressed verb, matching the
// typed client — a '/' or '?' in an id must not rewrite the route.
func TestRenderCLI_PathEscapesIDs(t *testing.T) {
	files := renderedCLI(t, defaultCLIOptions())
	posts := files["posts.go"]
	if got := strings.Count(posts, "url.PathEscape(id)"); got != 4 { // get/update/patch/delete
		t.Errorf("want 4 url.PathEscape(id) uses, got %d", got)
	}
}

// An entity whose command form collides with a CLI built-in (config, login,
// custom, …) must fail generation — it would shadow a command or emit a
// duplicate filename.
func TestRenderCLI_ReservedCommandName(t *testing.T) {
	for _, table := range []string{"config", "login", "custom"} {
		decls := []framework.EntityDeclaration{{
			Name:   table,
			Table:  table,
			Fields: []framework.FieldDeclaration{{Name: "name", Type: "string"}},
		}}
		_, err := buildCLISpec(decls, defaultCLIOptions(), "x")
		if err == nil || !strings.Contains(err.Error(), table) {
			t.Errorf("entity table %q should fail generation, got %v", table, err)
		}
	}
}

// cliTempModule scaffolds a temp Go module with a two-entity project
// (generateProject at the module root) and returns its dir. The module
// replaces gofastr with the repo so generated code builds offline.
func cliTempModule(t *testing.T) string {
	t.Helper()
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	goVersion, err := repoGoVersion(repoRoot)
	if err != nil {
		t.Fatalf("repoGoVersion: %v", err)
	}
	goMod := "module example.com/myapp\n\ngo " + goVersion + "\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => " + repoRoot + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyGoSum(repoRoot, dir); err != nil {
		t.Fatalf("copy go.sum: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gofastr.yml"), []byte(`app:
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
`), 0o644); err != nil {
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
	generateProject([]string{"--from=gofastr.yml"})
	return dir
}

// End-to-end compile: generate the project, then the CLI, and `go build` it.
func TestGenerateCLI_BuildsCleanly(t *testing.T) {
	dir := cliTempModule(t)
	runGenerateCLI([]string{"--binary=myapp"})

	for _, f := range []string{"main.go", "config.go", "auth.go", "output.go", "custom.go", "posts.go", "documents.go"} {
		if _, err := os.Stat(filepath.Join(dir, "cli", f)); err != nil {
			t.Fatalf("cli/%s not written: %v", f, err)
		}
	}
	cmd := exec.Command("go", "build", "-o", filepath.Join(dir, "myapp-bin"), "./cli")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated CLI did not build: %v\n%s", err, out)
	}
}

// custom.go is the dev-owned seam: --force regen must not touch it, and its
// commands must merge over the generated set.
func TestGenerateCLI_CustomSeamSurvives(t *testing.T) {
	dir := cliTempModule(t)
	runGenerateCLI([]string{"--binary=myapp"})

	customPath := filepath.Join(dir, "cli", "custom.go")
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
	}
}

func configureClient(c *client.Client) {}
`
	if err := os.WriteFile(customPath, []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}

	runGenerateCLI([]string{"--binary=myapp", "--force"})

	after, err := os.ReadFile(customPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != custom {
		t.Fatalf("--force overwrote custom.go")
	}

	bin := filepath.Join(dir, "myapp-cli")
	cmd := exec.Command("go", "build", "-o", bin, "./cli")
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
}

// The full loop: token-authenticated server in-process, generated CLI binary
// driven via os/exec. Covers login/config, every CRUD verb, batch, watch,
// table output, and the auth exit code on a revoked token.
func TestGenerateCLI_RoundTripLiveServer(t *testing.T) {
	dir := cliTempModule(t)
	runGenerateCLI([]string{"--binary=myapp"})
	bin := filepath.Join(dir, "myapp")
	build := exec.Command("go", "build", "-o", bin, "./cli")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, out)
	}

	// Server: posts entity (session-required by default) + token middleware.
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
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

	// create via field flags
	out, code := run("posts", "create", "--title", "hello", "--views", "3")
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
	if out, code = run("posts", "batch-delete", bid1, "--token", plaintext, bid2); code != 0 {
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

	// watch: subscribe, create until an event line arrives
	wctx, wcancel := context.WithTimeout(ctx, 20*time.Second)
	defer wcancel()
	watch := exec.CommandContext(wctx, bin, "posts", "watch")
	watch.Env = env
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
	for i := 0; i < 50; i++ {
		run("posts", "create", "--title", "sse")
		select {
		case l, ok := <-lines:
			if ok {
				eventLine = l
			}
			break poll
		case <-time.After(200 * time.Millisecond):
		case <-wctx.Done():
			break poll
		}
	}
	wcancel()
	watch.Wait()
	if !strings.Contains(eventLine, "entity.created") {
		t.Fatalf("watch produced no created event: %q", eventLine)
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
