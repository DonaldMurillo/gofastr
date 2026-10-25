package appgen

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/framework"
)

// TestCompositeKeyScreens generates an app whose record screens show a
// composite-key entity and runs the app's own generated e2e suite: it
// creates a record, opens its detail screen at one path segment per key
// column, then updates and deletes it through the API.
func TestCompositeKeyScreens(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and boots a generated app; skipped under -short")
	}
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	goVersion, err := clitest.RepoGoVersion(repoRoot)
	if err != nil {
		t.Fatalf("repoGoVersion: %v", err)
	}
	clitest.WriteTestFile(t, filepath.Join(dir, "go.mod"), "module example.com/roster\n\ngo "+goVersion+"\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => "+repoRoot+"\n")
	if err := clitest.CopyGoSum(repoRoot, dir); err != nil {
		t.Fatalf("copy go.sum: %v", err)
	}
	clitest.WriteTestFile(t, filepath.Join(dir, "gofastr.yml"), clitest.CompositeScreensSpec)
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWD)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	RunGenerateApp([]string{"gofastr.yml"})

	var src strings.Builder
	matches, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	for _, m := range matches {
		b, err := os.ReadFile(m)
		if err != nil {
			t.Fatal(err)
		}
		src.Write(b)
	}
	for _, want := range []string{
		`app.NewScreen("/enrollments/:student_id/:course_id"`,
		`app.NewScreen("/:term/enrollments/:student_id/:course_id/edit"`,
		`s.id = framework.JoinKey([]string{p["student_id"], p["course_id"]})`,
		`s.base = "/" + url.PathEscape(p["term"]) + "/enrollments"`,
		`appUI.Record("enrollments", s.id).Base(s.base).RenderCtx(ctx)`,
		`appUI.Record("enrollments", s.id).Delete().Duplicate().RenderCtx(ctx)`,
		`var e2eKeyCols = []string{"studentId", "courseId"}`,
	} {
		if !strings.Contains(src.String(), want) {
			t.Errorf("generated app lacks %s", want)
		}
	}
	if t.Failed() {
		return
	}

	clitest.WriteTestFile(t, filepath.Join(dir, "edit_save_test.go"), editSaveTest)
	cmd := exec.Command("go", "test", "-run", "^(TestE2E|TestEditSavesToRecord)$", "-count=1", "-v", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated e2e failed: %v\n%s", err, out)
	}
	for _, name := range []string{"TestE2E", "TestEditSavesToRecord"} {
		if !strings.Contains(string(out), "--- PASS: "+name+" ") {
			t.Fatalf("generated %s did not run:\n%s", name, out)
		}
	}
}

// editSaveTest joins the generated app's own e2e package: it boots the
// app, opens the edit screen of a record whose key values need escaping
// and reads where Save goes. The edit route continues past the record,
// so the page's default base (the request path less one segment per key
// column) would send Save to /enrollments/ann%20lee/ann%20lee/c%201.
const editSaveTest = `package main

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEditSavesToRecord(t *testing.T) {
	t.Setenv("ADMIN_SEED_PASSWORD", "roster-pw-123")
	dir := t.TempDir()
	bin := filepath.Join(dir, "app")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	addr := e2eFreeAddr(t)
	srv := exec.Command(bin)
	srv.Dir = dir
	srv.Env = append(os.Environ(), "PORT="+addr, "DATABASE_URL=file:"+filepath.Join(dir, "e2e.db"))
	e2eCaptureServerLog(t, srv, dir)
	if err := srv.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Process.Kill(); _, _ = srv.Process.Wait() })
	base := "http://" + addr
	e2eWaitReady(t, base)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	if _, err := client.PostForm(base+"/auth/login", url.Values{"email": {"admin@example.com"}, "password": {"roster-pw-123"}}); err != nil {
		t.Fatalf("login: %v", err)
	}
	if code, body := e2eDo(t, client, "POST", base+"/api/enrollments", ` + "`" + `{"student_id": "ann lee", "course_id": "c 1", "grade": "B"}` + "`" + `); code != http.StatusCreated {
		t.Fatalf("create = %d: %s", code, body)
	}
	code, body := e2eDo(t, client, "GET", base+"/fall%3F24/enrollments/ann%20lee/c%201/edit", "")
	if code != http.StatusOK {
		t.Fatalf("edit screen = %d", code)
	}
	if want := ` + "`" + `data-cui-rpc-navigate="/fall%3F24/enrollments/ann%20lee/c%201"` + "`" + `; !strings.Contains(body, want) {
		i := strings.Index(body, "data-cui-rpc-navigate")
		t.Fatalf("Save does not land on the record (%s): %.120s", want, body[max(i, 0):])
	}
}
`

// An entity keyed on one declared column has no "id" in its create
// response: the generated e2e test reads the record's path from the key
// column instead.
// The e2e create sends no relation, so a relation in the declared key
// leaves the record with no id: the entity is no create target, whether
// the relation is required or not.
func TestE2ESkipsRelationKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gofastr.yml")
	clitest.WriteTestFile(t, path, clitest.CompositeScreensSpec)
	bp, err := appspec.LoadAppSpec(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	decl := bp.Entities[0]
	if appSpecUnfillableRelation(decl) {
		t.Fatal("control: a key of plain columns must stay a create target")
	}
	for i := range decl.Fields {
		decl.Fields[i].Required = false
		if decl.Fields[i].Name == "student_id" {
			decl.Fields[i].Type = "relation"
		}
	}
	if !appSpecUnfillableRelation(decl) {
		t.Error("an optional relation in the key must disqualify the create target")
	}
	// primary_key: [id] with id a relation is the same shape.
	decl.PrimaryKey = []string{"id"}
	decl.Fields = append(decl.Fields, framework.FieldDeclaration{Name: "id", Type: "relation", To: "people"})
	if !appSpecUnfillableRelation(decl) {
		t.Error("an optional relation id key must disqualify the create target")
	}
}

func TestE2EReadsDeclaredKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gofastr.yml")
	clitest.WriteTestFile(t, path, strings.Replace(clitest.CompositeScreensSpec, "primary_key: [student_id, course_id]", "primary_key: [student_id]", 1))
	bp, err := appspec.LoadAppSpec(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if src := renderAppSpecE2ETest(bp); !strings.Contains(src, `var e2eKeyCols = []string{"studentId"}`) {
		t.Errorf("e2e test does not read the student_id key:\n%s", src)
	}
	// The create sends the key column even when it is optional, and the
	// update leaves it alone.
	decl := bp.Entities[0]
	for i := range decl.Fields {
		decl.Fields[i].Required = decl.Fields[i].Name == "grade"
	}
	create, update, _ := appSpecE2ECreateBody(decl)
	if !strings.Contains(create, `"student_id"`) || strings.Contains(update, `"student_id"`) {
		t.Errorf("create %s / update %s: want the key in the create only", create, update)
	}
	// A uuid key the server does not generate is sent too.
	decl.Fields[0].Type = "uuid"
	if create, _, _ := appSpecE2ECreateBody(decl); !strings.Contains(create, `"student_id": "00000000-0000-4000-8000-`) {
		t.Errorf("create %s: want a uuid for the student_id key", create)
	}
	// auto_generate: none is no generation: the create still sends it, and
	// the form and seed rows still fill it.
	decl.Fields[0].AutoGenerate = " None"
	if create, _, _ := appSpecE2ECreateBody(decl); !strings.Contains(create, `"student_id": "00000000-0000-4000-8000-`) {
		t.Errorf("create %s: auto_generate none dropped the student_id key", create)
	}
	if appSpecFormFieldSkipped(decl.Fields[0], nil) {
		t.Error("the form skips a field with auto_generate none")
	}
	rows := appspec.AppSpecGenerateSeedRows(decl, 1, nil)
	if len(rows) != 1 || rows[0]["student_id"] == nil {
		t.Fatalf("seed rows %v: auto_generate none dropped student_id", rows)
	}
	// The app seeds before the e2e test creates: the two keys differ.
	if create, _, _ := appSpecE2ECreateBody(decl); strings.Contains(create, `"`+rows[0]["student_id"].(string)+`"`) {
		t.Errorf("seed key %v collides with the e2e create %s", rows[0]["student_id"], create)
	}
}

// Count-seeded rows never repeat a key, nor take the one the e2e create
// sends; a count an enum-only key cannot cover is refused.
func TestSeedCountKeysDistinct(t *testing.T) {
	type F = framework.FieldDeclaration
	keyOf := func(decl framework.EntityDeclaration, row map[string]any) string {
		parts := []string{}
		for _, c := range decl.PrimaryKey {
			parts = append(parts, fmt.Sprint(row[c]))
		}
		return strings.Join(parts, "|")
	}
	refuses := func(decl framework.EntityDeclaration, count int) bool {
		bp := appspec.AppSpec{Seed: []appspec.AppSpecSeedEntity{{Entity: decl.Name, Count: count}}}
		return appspec.ValidateAppSpecSeedStructure(bp, map[string]framework.EntityDeclaration{decl.Name: decl}) != nil
	}
	ints := framework.EntityDeclaration{Name: "widgets", PrimaryKey: []string{"code"},
		Fields: []F{{Name: "code", Type: "int", Required: true}, {Name: "title", Type: "string"}}}
	enums := framework.EntityDeclaration{Name: "slots", PrimaryKey: []string{"day", "shift"},
		Fields: []F{{Name: "day", Type: "enum", Values: []string{"mon", "tue"}}, {Name: "shift", Type: "enum", Values: []string{"am", "pm", "eve"}}}}
	for _, c := range []struct {
		decl framework.EntityDeclaration
		n    int
	}{{ints, 100}, {enums, 5}} {
		create, _, _ := appSpecE2ECreateBody(c.decl)
		e2eKey := map[string]any{}
		_ = json.Unmarshal([]byte(create), &e2eKey)
		seen := map[string]bool{keyOf(c.decl, e2eKey): true}
		for _, row := range appspec.AppSpecGenerateSeedRows(c.decl, c.n, nil) {
			if k := keyOf(c.decl, row); seen[k] {
				t.Errorf("%s: seed key %s repeats (e2e create %s)", c.decl.Name, k, create)
			} else {
				seen[k] = true
			}
		}
	}
	if refuses(enums, 5) || !refuses(enums, 6) {
		t.Error("slots has 6 key combinations, one kept for e2e: want 5 accepted, 6 refused")
	}
	// Another key column makes each row distinct: no limit.
	mixed := enums
	mixed.PrimaryKey = []string{"day", "label"}
	mixed.Fields = append(slices.Clone(enums.Fields), F{Name: "label", Type: "string"})
	if refuses(mixed, 50) {
		t.Error("a string key column refused a count of 50")
	}
	// Count seeding never fills a relation, so a relation key is refused.
	rel := framework.EntityDeclaration{Name: "members", PrimaryKey: []string{"team", "role"},
		Fields: []F{{Name: "team", Type: "relation", To: "teams"}, {Name: "role", Type: "string"}}}
	if !refuses(rel, 1) {
		t.Error("count seeding accepted a relation key column")
	}
	shared := framework.EntityDeclaration{Name: "profiles", PrimaryKey: []string{"id"},
		Fields: []F{{Name: "id", Type: "relation", To: "users"}}}
	sharedSeed := appspec.AppSpec{Seed: []appspec.AppSpecSeedEntity{{Entity: "profiles", Count: 1}}}
	if err := appspec.ValidateAppSpecSeedStructure(sharedSeed, map[string]framework.EntityDeclaration{"profiles": shared}); err == nil || !strings.Contains(err.Error(), "cannot fabricate the required relation") {
		t.Errorf("relation key named id: %v, want the relation refusal", err)
	}
	if !refuses(enums, math.MaxInt) {
		t.Error("a count of MaxInt passed the enum key limit")
	}
	// Repeated enum values are one value: [A, B, B] has two, one for e2e.
	dup := framework.EntityDeclaration{Name: "codes", PrimaryKey: []string{"code"},
		Fields: []F{{Name: "code", Type: "enum", Values: []string{"A", "B", "B"}}}}
	if refuses(dup, 1) || !refuses(dup, 2) {
		t.Error("codes has two distinct values: want 1 accepted, 2 refused")
	}
	bp := appspec.AppSpec{Seed: []appspec.AppSpecSeedEntity{{Entity: "codes", Count: 2}}}
	if err := appspec.ValidateAppSpecSeedStructure(bp, map[string]framework.EntityDeclaration{"codes": dup}); err == nil || !strings.Contains(err.Error(), "Lower the count to 1") {
		t.Errorf("codes count 2: %v, want the limit of 1", err)
	}
	// A generated key an explicit row already holds is refused.
	abc := dup
	abc.Fields = []F{{Name: "code", Type: "enum", Values: []string{"A", "B", "C"}}}
	for _, c := range []struct {
		decl framework.EntityDeclaration
		row  map[string]any
	}{{abc, map[string]any{"code": "B"}}, {ints, map[string]any{"code": 1001, "title": "x"}}} {
		bp := appspec.AppSpec{Seed: []appspec.AppSpecSeedEntity{{Entity: c.decl.Name, Count: 1, Rows: []map[string]any{c.row}}}}
		if appspec.ValidateAppSpecSeedStructure(bp, map[string]framework.EntityDeclaration{c.decl.Name: c.decl}) == nil {
			t.Errorf("%s: a generated key repeating explicit row %v passed", c.decl.Name, c.row)
		}
	}
	// A bounded integer key stays inside its bounds, clear of e2e's 1.
	lo, hi := 1.0, 100.0
	bounded := ints
	bounded.Fields = []F{{Name: "code", Type: "int", Min: &lo, Max: &hi}, {Name: "title", Type: "string"}}
	if rows := appspec.AppSpecGenerateSeedRows(bounded, 1, nil); rows[0]["code"] != 2 {
		t.Errorf("bounded key seeded %v, want 2", rows[0]["code"])
	}
	if refuses(bounded, 99) || !refuses(bounded, 100) {
		t.Error("code in [1, 100] less e2e's 1: want 99 accepted, 100 refused")
	}
	// An untyped key column is a string and gets a value; a file key
	// gets none and is refused.
	untyped := framework.EntityDeclaration{Name: "tags", PrimaryKey: []string{"code"},
		Fields: []F{{Name: "code", Required: true}}}
	if refuses(untyped, 3) {
		t.Error("count seeding refused an untyped (string) key")
	}
	filed := untyped
	filed.Fields = []F{{Name: "code", Type: "file"}}
	if !refuses(filed, 1) {
		t.Error("count seeding accepted a key column it cannot fill")
	}
	// A key the server fills is not the seed's to fill.
	served := untyped
	served.Fields = []F{{Name: "code", Type: "uuid", AutoGenerate: "uuid"}, {Name: "title", Type: "string"}}
	if refuses(served, 3) {
		t.Error("count seeding refused a key the server generates")
	}
	// A declared id the server does not fill is a key the seed leaves empty.
	ownID := framework.EntityDeclaration{Name: "notes", Fields: []F{{Name: "id", Type: "uuid", Required: true}, {Name: "body", Type: "string"}}}
	if !refuses(ownID, 1) {
		t.Error("count seeding accepted a declared id it cannot fill")
	}
	// "01001" is the integer 1001.
	bp = appspec.AppSpec{Seed: []appspec.AppSpecSeedEntity{{Entity: "widgets", Count: 1, Rows: []map[string]any{{"code": "01001", "title": "x"}}}}}
	if appspec.ValidateAppSpecSeedStructure(bp, map[string]framework.EntityDeclaration{"widgets": ints}) == nil {
		t.Error(`a generated 1001 repeating explicit "01001" passed`)
	}
	// A large min is honoured exactly, and one past int64 is refused.
	big, huge := 4503599627370497.0, 1e19
	bigMin := ints
	bigMin.Fields = []F{{Name: "code", Type: "int", Min: &big}, {Name: "title", Type: "string"}}
	if rows := appspec.AppSpecGenerateSeedRows(bigMin, 1, nil); rows[0]["code"] != 4503599627370497 || refuses(bigMin, 1) {
		t.Errorf("min %v seeded %v", big, rows[0]["code"])
	}
	bigMin.Fields[0].Min = &huge
	if !refuses(bigMin, 1) {
		t.Error("a key min past int64 passed")
	}
	// Bounds compare as integers: 2^53+1 is past a max of 2^53.
	near, top := 9007199254740990.0, 9007199254740992.0
	bigMin.Fields[0].Min, bigMin.Fields[0].Max = &near, &top
	if refuses(bigMin, 3) || !refuses(bigMin, 4) {
		t.Error("key in [2^53-2, 2^53]: want 3 accepted, 4 refused")
	}
	// 1000000.0 is the integer 1000000.
	million := 1000000.0
	bigMin.Fields[0].Min, bigMin.Fields[0].Max = &million, nil
	bp = appspec.AppSpec{Seed: []appspec.AppSpecSeedEntity{{Entity: "widgets", Count: 1, Rows: []map[string]any{{"code": 1000000.0, "title": "x"}}}}}
	if appspec.ValidateAppSpecSeedStructure(bp, map[string]framework.EntityDeclaration{"widgets": bigMin}) == nil {
		t.Error("a generated 1000000 repeating explicit 1000000.0 passed")
	}
}

// An edit screen whose record param opens the route bases at the root,
// or the record page would take /abc as its base and save to /abc/abc.
func TestRecordBaseAtRoot(t *testing.T) {
	edit := []appspec.AppSpecBlock{{Kind: "entity_form", Entity: "posts", Mode: "edit"}}
	for route, want := range map[string]string{
		"/{id}/edit":       `"/"`,
		"/{org}/{id}/edit": `"/" + url.PathEscape(p["org"])`,
		"/posts/{id}":      ``,
	} {
		if got := screenRecordBase(appspec.AppSpec{}, appspec.AppSpecScreen{Route: route, Body: edit}); got != want {
			t.Errorf("%s: base %s, want %s", route, got, want)
		}
	}
}
