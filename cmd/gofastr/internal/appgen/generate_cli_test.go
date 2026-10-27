package appgen

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
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
		{Pagination: &framework.PaginationDeclaration{}, Exposure: &framework.ExposureDeclaration{}, Name: "documents",
			Table: "documents", Scope: &framework.ScopeDeclaration{SoftDelete: true}, SearchFields: []string{"title", "body"},
			Fields: []framework.FieldDeclaration{
				{Name: "title", Type: "string", Required: true},
				{Name: "body", Type: "text"},
				{Name: "published_at", Type: "timestamp"},
			},
		},
	}
}

func cliFixtureSpec(t *testing.T, opts CliOptions) cliSpec {
	t.Helper()
	spec, err := buildCLISpec(cliFixtureDecls(), opts, "example.com/app/entities/client")
	if err != nil {
		t.Fatalf("buildCLISpec: %v", err)
	}
	return spec
}

func renderedCLI(t *testing.T, opts CliOptions) map[string]string {
	t.Helper()
	files := RenderCLIFiles(cliFixtureSpec(t, opts))
	out := map[string]string{}
	for _, f := range files {
		out[f.Name] = f.Content
	}
	return out
}

func defaultCLIOptions() CliOptions {
	return CliOptions{OutDir: "cli", Binary: "myapp", ApiPrefix: "api"}
}

// Every selected entity gets a command file wiring its verbs into the
// dispatch table, and main.go registers the entity command groups.
func TestRenderCLI_CommandTreePerEntity(t *testing.T) {
	files := renderedCLI(t, defaultCLIOptions())

	main := files["main.gen.go"]
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

	if !strings.Contains(files["main.gen.go"], "func groupUsage(") {
		t.Errorf("main.go missing groupUsage helper")
	}

	posts := files["posts.gen.go"]
	for _, w := range []string{
		`{name: "posts",`, // bare entity command → group help
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
	if _, ok := files["documents.gen.go"]; !ok {
		t.Errorf("documents.go not rendered")
	}
	for _, name := range []string{"config.gen.go", "auth.gen.go", "output.gen.go", "custom.go"} {
		if _, ok := files[name]; !ok {
			t.Errorf("%s not rendered", name)
		}
	}
}

// List flags derive from the schema: an eq filter entry per field, range
// entries only on numeric/date/timestamp fields, -q only with
// SearchFields, --trashed only with SoftDelete. The per-entity filter
// table (postsListFilters) registers against the shared runListVerb body
// in verbs.go.
func TestRenderCLI_FlagsFromSchema(t *testing.T) {
	files := renderedCLI(t, defaultCLIOptions())

	posts := files["posts.gen.go"]
	for _, w := range []string{
		`{flag: "title", param: "title"`,
		`{flag: "views", param: "views"`,
		`{flag: "views-gt", param: "views_gt"`,
		`{flag: "views-lte", param: "views_lte"`,
		`{flag: "title-like", param: "title_like"`,
		`{flag: "title-ne", param: "title_ne"`,
		`{flag: "views-ne", param: "views_ne"`,
		// Text columns are ordered too: the server accepts ?title_gt=,
		// so the flag ships (the old hardcoded Comparable list hid an
		// operator the parser accepts).
		`{flag: "title-gt", param: "title_gt"`,
	} {
		if !strings.Contains(posts, w) {
			t.Errorf("posts.go filter table missing %q", w)
		}
	}
	// A Bool column refuses every ordered and LIKE operator: none of
	// those flags may ship for `published`.
	for _, absent := range []string{`{flag: "q"`, `{flag: "trashed"`, `{flag: "published-gt"`, `{flag: "published-like"`, `{flag: "published-lt"`} {
		if strings.Contains(posts, absent) {
			t.Errorf("posts.go should not have %q", absent)
		}
	}

	verbs := files["verbs.gen.go"]
	for _, w := range []string{
		`fs.String("sort"`,
		`fs.String("page"`,
		`fs.String("cursor"`,
		`fs.String("include"`,
		`fs.String("fields"`,
		`fs.String(f.flag, "", f.help)`,
		`fs.Bool(f.flag, false, f.help)`,
	} {
		if !strings.Contains(verbs, w) {
			t.Errorf("verbs.go shared list flags missing %q", w)
		}
	}

	// The mutation verbs are table-driven the same way: one row per
	// writable field in <entity>.go, one shared body per verb in verbs.go.
	for _, w := range []string{
		`var postsMutationFields = []mutationField{`,
		`{flag: "title", wire: "title", kind: fieldString, usage: "title (string)"}`,
		`{flag: "views", wire: "views", kind: fieldInt, usage: "views (int)"}`,
		`{flag: "published", wire: "published", kind: fieldBool, usage: "published (bool)"}`,
	} {
		if !strings.Contains(posts, w) {
			t.Errorf("posts.go mutation table missing %q", w)
		}
	}
	for _, w := range []string{
		`fs.Int(f.flag, 0, f.usage)`,
		`fs.Float64(f.flag, 0, f.usage)`,
		`fs.Bool(f.flag, false, f.usage)`,
		`fs.String(f.flag, "", f.usage)`,
		`body[f.wire] = *strVals[i]`,
		`runCreateVerb("posts create", "/posts", postsMutationFields, args)`,
		`runUpdateVerb("posts update", "/posts", []string{"id"}, postsMutationFields, args)`,
		`runPatchVerb("posts patch", "/posts", []string{"id"}, postsMutationFields, args)`,
	} {
		if !strings.Contains(posts, w) && !strings.Contains(verbs, w) {
			t.Errorf("mutation table wiring missing %q", w)
		}
	}

	docs := files["documents.gen.go"]
	for _, w := range []string{
		`{flag: "q", param: "q"`,
		`{flag: "trashed", param: "trashed", help: "include soft-deleted rows", isBool: true}`,
		`{flag: "published-at-gt"`,
	} {
		if !strings.Contains(docs, w) {
			t.Errorf("documents.go missing %q", w)
		}
	}
}

// A field whose flag name collides with a reserved flag (sort, json, page,
// url, …) must fail at generate time with the entity and field named, and
// never emit a broken flag set.
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
	if !strings.Contains(files["main.gen.go"], `apiPrefix  = "/api"`) {
		t.Errorf("main.go should bake apiPrefix constant")
	}

	bare := renderedCLI(t, CliOptions{OutDir: "cli", Binary: "myapp", ApiPrefix: ""})
	if !strings.Contains(bare["main.gen.go"], `apiPrefix  = ""`) {
		t.Errorf("empty --api-prefix should bake an empty prefix")
	}
}

// --only / --exclude select entities; excluded entities render nothing.
func TestRenderCLI_EntityOnlyExclude(t *testing.T) {
	opts := defaultCLIOptions()
	opts.only = []string{"posts"}
	files := renderedCLI(t, opts)
	if _, ok := files["documents.gen.go"]; ok {
		t.Errorf("--only=posts must not render documents.gen.go")
	}

	opts = defaultCLIOptions()
	opts.exclude = []string{"posts"}
	files = renderedCLI(t, opts)
	if _, ok := files["posts.gen.go"]; ok {
		t.Errorf("--exclude=posts must not render posts.gen.go")
	}
	if _, ok := files["documents.gen.go"]; !ok {
		t.Errorf("--exclude=posts must keep documents.gen.go")
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
	posts := files["posts.gen.go"]
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
	if strings.Contains(files["posts.gen.go"], `{name: "posts get"`) {
		t.Errorf("per-entity verbs: posts should only have list")
	}
	if !strings.Contains(files["documents.gen.go"], `{name: "documents watch"`) {
		t.Errorf("per-entity verbs: documents=* should keep watch")
	}

	opts = defaultCLIOptions()
	opts.verbs = "posts=frobnicate"
	if _, err := buildCLISpec(cliFixtureDecls(), opts, "x"); err == nil {
		t.Errorf("unknown verb must error at generate time")
	}
}

// A delete-only (or any narrow) verb selection must still emit code that
// compiles: the shared delete body lands in verbs.go with its net/http
// import (takeKey, in output.go, escapes the key), and the entity file's
// wrapper needs no imports at all (format.Source cannot add missing imports, so a stray reference
// would be a broken file).
func TestRenderCLI_NarrowVerbImports(t *testing.T) {
	opts := defaultCLIOptions()
	opts.verbs = "delete"
	files := renderedCLI(t, opts)
	posts := files["posts.gen.go"]
	for _, w := range []string{"runPostsDelete(", `runDeleteVerb("posts delete", "/posts", []string{"id"}, args)`} {
		if !strings.Contains(posts, w) {
			t.Errorf("delete-only posts.go missing %q\n%s", w, posts)
		}
	}
	if strings.Contains(posts, "import (") {
		t.Errorf("delete-only posts.go should need no imports:\n%s", posts)
	}
	verbs := files["verbs.gen.go"]
	for _, w := range []string{`"fmt"`, `"net/http"`, "takeKey(cmd, keys, args)"} {
		if !strings.Contains(verbs, w) {
			t.Errorf("delete-only verbs.go missing %q\n%s", w, verbs)
		}
	}
	if strings.Contains(verbs, `"context"`) {
		t.Errorf("delete-only verbs.go should not import context (no watch verb)")
	}
}

// A create/update/patch-only selection is the import-shrink contract for
// the table-driven mutation verbs: the entity file carries just the field
// table and one-line wrappers (no imports at all: no flag plumbing, no
// http method identifiers, no path building), while verbs.go gains the
// encoding/json, fmt and net/http its shared body needs.
func TestRenderCLI_MutationOnlyImports(t *testing.T) {
	opts := defaultCLIOptions()
	opts.verbs = "create,update,patch"
	files := renderedCLI(t, opts)
	posts := files["posts.gen.go"]
	for _, w := range []string{
		"var postsMutationFields = []mutationField{",
		`runCreateVerb("posts create", "/posts", postsMutationFields, args)`,
		`runUpdateVerb("posts update", "/posts", []string{"id"}, postsMutationFields, args)`,
		`runPatchVerb("posts patch", "/posts", []string{"id"}, postsMutationFields, args)`,
	} {
		if !strings.Contains(posts, w) {
			t.Errorf("mutation-only posts.go missing %q\n%s", w, posts)
		}
	}
	if strings.Contains(posts, "import (") {
		t.Errorf("mutation-only posts.go should need no imports:\n%s", posts)
	}
	verbs := files["verbs.gen.go"]
	for _, w := range []string{`"fmt"`, `"net/http"`, "func runMutationVerb("} {
		if !strings.Contains(verbs, w) {
			t.Errorf("mutation-only verbs.go missing %q\n%s", w, verbs)
		}
	}
	for _, absent := range []string{`"context"`, `"encoding/json"`, "func runListVerb(", "func runWatchVerb("} {
		if strings.Contains(verbs, absent) {
			t.Errorf("mutation-only verbs.go should not carry %q", absent)
		}
	}
}

// Positional ids are path-escaped in every id-addressed verb, matching the
// typed client: a '/' or '?' in an id must not rewrite the route. The
// shared bodies in verbs.go (get, delete, and the mutation with-id form
// behind update/patch) each take the key through takeKey, which escapes
// every value as its own segment;
// the entity files bind only the pre-escaped base path.
func TestRenderCLI_PathEscapesIDs(t *testing.T) {
	files := renderedCLI(t, defaultCLIOptions())
	if got := strings.Count(files["posts.gen.go"], "url.PathEscape(id)"); got != 0 {
		t.Errorf("entity file should bind only the base path, got %d url.PathEscape(id) uses in posts.gen.go", got)
	}
	if got := strings.Count(files["verbs.gen.go"], "takeKey("); got != 3 { // get + delete + mutation
		t.Errorf("want 3 takeKey uses in verbs.go (get/delete/mutation), got %d", got)
	}
	if !strings.Contains(files["output.gen.go"], "segs[i] = url.PathEscape(args[i])") {
		t.Error("takeKey does not escape each key value as its own segment")
	}
}

// An entity whose command form collides with a CLI built-in (config,
// login, custom, verbs, …) must fail generation: it would shadow a
// command or emit a duplicate filename (verbs.go is the shared verb
// bodies).
func TestRenderCLI_ReservedCommandName(t *testing.T) {
	for _, table := range []string{"config", "login", "custom", "verbs"} {
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
