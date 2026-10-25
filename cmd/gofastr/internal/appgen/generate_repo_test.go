package appgen

import (
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/framework"
)

// An enum value with no identifier form, or two values that camel-case to
// one name, gets no constant: the value is named in a comment, quoted, and
// stays reachable through the value type. A control byte never reaches an
// identifier or ends the comment.
func TestEnumConstsSkipUnnamedValues(t *testing.T) {
	out := renderEntityEnums(framework.EntityDeclaration{
		Name: "posts",
		Fields: []framework.FieldDeclaration{{
			Name: "status", Type: "enum",
			Values: []string{"draft", "past_due", "past-due", "N/A", "a\nb"},
		}},
	})
	for _, want := range []string{
		"type PostsStatusValue string",
		`PostsStatusDraft PostsStatusValue = "draft"`,
		`// No constant for "past_due", "past-due", "N/A", "a\nb": the value has no identifier form. Write PostsStatusValue(<value>).`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("enum block missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "PostsStatusPastDue") {
		t.Errorf("colliding values got a constant:\n%s", out)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "x.go", "package x\n"+out, 0); err != nil {
		t.Fatalf("enum block does not parse: %v\n%s", err, out)
	}
}

// An enum value whose constant would repeat another name in the entity
// file gets no constant: "value" on status meets the value type
// PostsStatusValue, and "title" meets the column var of field
// status_title. The whole file must still declare each name once.
func TestEnumConstsAvoidFileNames(t *testing.T) {
	decl := framework.EntityDeclaration{
		Name: "posts",
		Fields: []framework.FieldDeclaration{
			{Name: "status", Type: "enum", Values: []string{"draft", "value", "title"}},
			{Name: "status_title", Type: "string"},
		},
	}
	out, err := renderEntityFile(decl, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := refuseDuplicateDecls([]GeneratedFile{{Name: "entities/posts.go", Content: out}}); err != nil {
		t.Fatalf("entity file: %v", err)
	}
	for _, want := range []string{
		`PostsStatusDraft PostsStatusValue = "draft"`,
		`// No constant for "value", "title"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("entity file missing %q:\n%s", want, out)
		}
	}
}

// A field whose column var is one of the new type names fails generation
// with that name, not with advice about entities only.
func TestFieldNamedPatchFailsGenerate(t *testing.T) {
	out, err := renderEntityFile(framework.EntityDeclaration{
		Name:   "releases",
		Fields: []framework.FieldDeclaration{{Name: "major", Type: "int"}, {Name: "patch", Type: "int"}},
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	err = refuseDuplicateDecls([]GeneratedFile{{Name: "entities/releases.go", Content: out}})
	if err == nil || !strings.Contains(err.Error(), "ReleasesPatch") || !strings.Contains(err.Error(), "fields") {
		t.Fatalf("err = %v, want the ReleasesPatch collision naming fields", err)
	}
}

// A NoQuery field has no column var, so it reserves no name: the enum
// value "title" keeps its constant beside a no_query status_title.
func TestNoQueryFieldReservesNoName(t *testing.T) {
	out := renderEntityEnums(framework.EntityDeclaration{
		Name: "posts",
		Fields: []framework.FieldDeclaration{
			{Name: "status", Type: "enum", Values: []string{"title"}},
			{Name: "status_title", Type: "string", NoQuery: true},
		},
	})
	if !strings.Contains(out, `PostsStatusTitle PostsStatusValue = "title"`) {
		t.Fatalf("constant missing:\n%s", out)
	}
}

// The write payloads leave out what the CRUD path never takes from them:
// an auto-generated column (create overwrites it, update skips it), and
// on a patch the owner field and the tenant column (update skips both).
// A required field with a default is a pointer on create, so leaving it
// out takes the default instead of sending "".
func TestWriteShapesDropServerFields(t *testing.T) {
	decl := framework.EntityDeclaration{
		Name:  "notes",
		Scope: &framework.ScopeDeclaration{OwnerField: "user_id", MultiTenant: true},
		Fields: []framework.FieldDeclaration{
			{Name: "body", Type: "string", Required: true},
			{Name: "kind", Type: "enum", Required: true, Values: []string{"note", "page"}, Default: "note"},
			{Name: "ref", Type: "string", AutoGenerate: "uuid"},
			{Name: "user_id", Type: "string"},
			{Name: "tenant_id", Type: "string"},
		},
	}
	shapes := renderEntityWriteShapes(decl)
	client := renderClientEntity(decl)
	input := between(t, shapes, "type NotesInput struct {", "}")
	patch := between(t, shapes, "type NotesPatch struct {", "}")
	clientInput := between(t, client, "type NotesInput struct {", "}")
	clientPatch := between(t, client, "type NotesPatch struct {", "}")
	clientBatch := between(t, client, "type NotesBatchPatch struct {", "}")
	for name, body := range map[string]string{"repo input": input, "client input": clientInput} {
		if !strings.Contains(body, "Body string `json:\"body\"`") {
			t.Errorf("%s: required field without default is not a value:\n%s", name, body)
		}
		if !strings.Contains(body, "Kind *") || !strings.Contains(body, `json:"kind,omitempty"`) {
			t.Errorf("%s: required field with default is not a pointer:\n%s", name, body)
		}
		if strings.Contains(body, "Ref ") {
			t.Errorf("%s carries the auto-generated column:\n%s", name, body)
		}
	}
	for name, body := range map[string]string{"repo patch": patch, "client patch": clientPatch, "client batch patch": clientBatch} {
		for _, field := range []string{"Ref ", "UserId ", "TenantId "} {
			if strings.Contains(body, field) {
				t.Errorf("%s carries %q, which update skips:\n%s", name, field, body)
			}
		}
		if !strings.Contains(body, "Body *string") {
			t.Errorf("%s lost an ordinary field:\n%s", name, body)
		}
	}

	// The JS SDK's d.ts follows the same rules.
	spec, err := buildSDKSpec([]framework.EntityDeclaration{decl}, &SdkOptions{name: "myapp", sdkVersion: "1.0.0"})
	if err != nil {
		t.Fatalf("buildSDKSpec: %v", err)
	}
	var dts string
	for _, f := range renderSDKJSFiles(spec) {
		if f.Name == "client.d.ts" {
			dts = f.Content
		}
	}
	tsInput := shapeBody(t, dts, "export interface NotesInput {")
	if !strings.Contains(tsInput, "body: string") || !strings.Contains(tsInput, "kind?:") || strings.Contains(tsInput, "ref") {
		t.Errorf("TS NotesInput: want body required, kind optional, no ref:\n%s", tsInput)
	}
	if want := `export type NotesPatch = Partial<Omit<NotesInput, "userId" | "tenantId">>;`; !strings.Contains(dts, want) {
		t.Errorf("client.d.ts is missing %q:\n%s", want, shapeBody(t, dts, "export type NotesPatch"))
	}
}

func between(t *testing.T, s, start, end string) string {
	t.Helper()
	i := strings.Index(s, start)
	if i < 0 {
		t.Fatalf("missing %q in:\n%s", start, s)
	}
	rest := s[i+len(start):]
	j := strings.Index(rest, "\n"+end)
	if j < 0 {
		t.Fatalf("no %q after %q", end, start)
	}
	return rest[:j]
}

// repoIntegrationTestSource drives the generated typed repository in
// process: the write shapes must carry zero values (a create overriding a
// non-zero default, a patch setting a field back to false or 0), the enum
// constants must reach both the struct fields and the typed column, and
// every move, the System one included, must run through Transition.
const repoIntegrationTestSource = `package integration_test

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"

	"github.com/DonaldMurillo/gofastr/framework"

	"example.com/repo/gen/entities"
)

func TestGeneratedRepo(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	app := framework.NewApp(framework.WithDB(db), framework.WithoutDefaultMiddleware())
	entities.RegisterAll(app)
	if err := framework.AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	ctx := context.Background()
	posts := entities.NewPostsRepo(app)

	// Create: published defaults to true and views to 5. An Input that
	// names false and 0 must store false and 0.
	p, err := posts.Create(ctx, entities.PostsInput{Title: "a", Published: new(false), Views: new(0)})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if p.ID == "" || p.Published || p.Views != 0 {
		t.Fatalf("create dropped zero values: %+v", p)
	}
	if p.Status != entities.PostsStatusDraft {
		t.Fatalf("status = %q, want the default %q", p.Status, entities.PostsStatusDraft)
	}
	// Omitted optional fields take their defaults.
	d, err := posts.Create(ctx, entities.PostsInput{Title: "defaults"})
	if err != nil {
		t.Fatalf("create defaults: %v", err)
	}
	if !d.Published || d.Views != 5 || d.Kind != entities.PostsKindNote {
		t.Fatalf("omitted fields lost their defaults: %+v", d)
	}

	// Update: a nil pointer leaves the field alone, a pointer to a zero
	// value writes it.
	u, err := posts.Update(ctx, d.ID, entities.PostsPatch{Published: new(false), Views: new(0)})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if u.Published || u.Views != 0 || u.Title != "defaults" {
		t.Fatalf("patch did not write zero values or touched title: %+v", u)
	}
	if got, err := posts.Get(ctx, d.ID); err != nil || got.Published || got.Views != 0 {
		t.Fatalf("stored row after patch = %+v, %v", got, err)
	}

	// Enum constants are typed values: the column takes them directly.
	if _, err := posts.Update(ctx, p.ID, entities.PostsPatch{Status: new(entities.PostsStatusLive)}); err != nil {
		t.Fatalf("update status: %v", err)
	}
	live, err := posts.Query().Where(entities.PostsStatus.Eq(entities.PostsStatusLive)).Find(ctx)
	if err != nil || len(live) != 1 || live[0].ID != p.ID {
		t.Fatalf("enum query = %+v, %v", live, err)
	}
	if n, err := posts.Query().Where(entities.PostsStatus.In(entities.PostsStatusDraft, entities.PostsStatusLive)).Count(ctx); err != nil || n != 2 {
		t.Fatalf("enum In count = %d, %v", n, err)
	}

	// Batch shapes and FirstOrCreate take Input and Patch too.
	made, err := posts.BatchCreate(ctx, []entities.PostsInput{{Title: "b1", Published: new(false)}, {Title: "b2"}})
	if err != nil || len(made) != 2 || made[0].Published || !made[1].Published {
		t.Fatalf("batch create = %+v, %v", made, err)
	}
	fixed, err := posts.BatchUpdate(ctx, []string{made[1].ID}, []entities.PostsPatch{{Published: new(false)}})
	if err != nil || len(fixed) != 1 || fixed[0].Published || fixed[0].Title != "b2" {
		t.Fatalf("batch update = %+v, %v", fixed, err)
	}
	again, err := posts.FirstOrCreate(ctx, entities.PostsInput{Title: "a"}, entities.PostsTitle.Eq("a"))
	if err != nil || again.ID != p.ID {
		t.Fatalf("first or create = %+v, %v", again, err)
	}
	fresh, err := posts.FirstOrCreate(ctx, entities.PostsInput{Title: "new", Published: new(false)}, entities.PostsTitle.Eq("new"))
	if err != nil || fresh.ID == "" || fresh.ID == p.ID || fresh.Published {
		t.Fatalf("first or create on a miss = %+v, %v", fresh, err)
	}

	// Cascade writes: children ride the payloads. A nested child with an
	// ID updates that child; one without creates it.
	parent, err := posts.Create(ctx, entities.PostsInput{Title: "parent",
		Comments: []entities.CommentsInput{{Body: "c1"}, {Body: "c2"}}})
	if err != nil {
		t.Fatalf("cascade create: %v", err)
	}
	comments := entities.NewCommentsRepo(app)
	kids, err := comments.Query().Where(entities.CommentsPostId.Eq(parent.ID)).Order(entities.CommentsBody.Asc()).Find(ctx)
	if err != nil || len(kids) != 2 || kids[0].Body != "c1" {
		t.Fatalf("cascaded children = %+v, %v", kids, err)
	}
	if _, err := posts.Update(ctx, parent.ID, entities.PostsPatch{Comments: []entities.PostsCommentsNested{
		{ID: kids[0].ID, CommentsPatch: entities.CommentsPatch{Body: new("c1 edited")}},
		{CommentsPatch: entities.CommentsPatch{Body: new("c3")}},
	}}); err != nil {
		t.Fatalf("cascade update: %v", err)
	}
	kids, err = comments.Query().Where(entities.CommentsPostId.Eq(parent.ID)).Order(entities.CommentsBody.Asc()).Find(ctx)
	if err != nil || len(kids) != 3 || kids[0].Body != "c1 edited" || kids[2].Body != "c3" {
		t.Fatalf("children after cascade update = %+v, %v", kids, err)
	}

	// Moves: Transition runs every move, the System one included.
	invoices := entities.NewInvoicesRepo(app)
	inv, err := invoices.Create(ctx, entities.InvoicesInput{Number: "INV-1"})
	if err != nil {
		t.Fatalf("create invoice: %v", err)
	}
	moved, err := invoices.Transition(ctx, inv.ID, entities.InvoicesMoveIssue)
	if err != nil || moved.Status != entities.InvoicesStatusOpen {
		t.Fatalf("issue = %+v, %v", moved, err)
	}
	if _, err := invoices.Transition(ctx, inv.ID, entities.InvoicesMoveMarkOverdue); err != nil {
		t.Fatalf("system move: %v", err)
	}
	paid, err := invoices.Transition(ctx, inv.ID, entities.InvoicesMovePay)
	if err != nil || paid.Status != entities.InvoicesStatusPaid || paid.PaidOn == "" {
		t.Fatalf("pay = %+v, %v", paid, err)
	}
	if _, err := invoices.Transition(ctx, inv.ID, entities.InvoicesMoveIssue); err == nil {
		t.Fatal("issue from paid succeeded; want a conflict")
	}
}
`

// TestGeneratedRepoRoundTrip generates a project into a temp module and runs
// repoIntegrationTestSource against it, so the emitted repo, not a hand copy,
// is what gets exercised.
func TestGeneratedRepoRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a generated module; skipped under -short")
	}
	clitest.VariantBuild(t)
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	goVersion, err := clitest.RepoGoVersion(repoRoot)
	if err != nil {
		t.Fatalf("repoGoVersion: %v", err)
	}
	goMod := "module example.com/repo\n\ngo " + goVersion + "\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => " + repoRoot + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := clitest.CopyGoSum(repoRoot, dir); err != nil {
		t.Fatalf("copy go.sum: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gofastr.yml"), []byte(`app:
  name: repo
entities:
  - name: posts
    crud: true
    public: true
    timestamps: false
    fields:
      - name: title
        type: string
        required: true
      - name: views
        type: int
        default: 5
      - name: published
        type: bool
        default: true
      - name: status
        type: enum
        values: [draft, live]
        default: draft
      - name: kind
        type: enum
        values: [note, page]
        required: true
        default: note
    relations:
      - type: has_many
        name: comments
        entity: comments
        foreign_key: post_id
        cascade_write: true
  - name: comments
    crud: true
    public: true
    timestamps: false
    fields:
      - name: body
        type: string
        required: true
      - name: post_id
        type: uuid
  - name: invoices
    crud: true
    public: true
    timestamps: false
    fields:
      - name: number
        type: string
        required: true
      - name: status
        type: enum
        values: [draft, open, paid]
        default: draft
      - name: paid_on
        type: date
    states:
      field: status
      initial: [draft]
      transitions:
        - key: issue
          from: [draft]
          to: open
        - key: pay
          from: [open]
          to: paid
          stamp: paid_on
        - key: mark_overdue
          from: [open]
          to: open
          system: true
`), 0o644); err != nil {
		t.Fatal(err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWD)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	RunGenerateApp([]string{"gofastr.yml", "--out=gen"})

	itDir := filepath.Join(dir, "integration")
	if err := os.MkdirAll(itDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(itDir, "repo_integration_test.go"), []byte(repoIntegrationTestSource), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-mod=mod", "-count=1", "./integration/...")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated repo integration failed: %v\n%s", err, out)
	}
}
