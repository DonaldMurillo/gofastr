package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// compositeScreensSpec gives a composite-key entity the screens an app
// spec hangs off a record: a list with a create form, a detail screen
// and an edit form, both at {id}.
const compositeScreensSpec = `app:
  name: Roster
  module: example.com/roster
  db:
    driver: sqlite
    url: file:roster.db
  auth:
    enabled: true
  admin:
    enabled: true
    seed_email: admin@example.com
    seed_password: roster-pw-123
entities:
  - name: enrollments
    crud: true
    primary_key: [student_id, course_id]
    fields:
      - name: student_id
        type: string
        required: true
      # The generated e2e test creates with the first value: a comma in a
      # key value must survive reading the record's id back.
      - name: course_id
        type: enum
        values: ["c,1", "c 1"]
        required: true
      - name: grade
        type: string
        required: true
screens:
  - name: home
    route: /
    title: Roster
    body:
      - type: heading
        level: 1
        text: Roster
  - name: enrollments
    route: /enrollments
    access:
      auth: true
    body:
      - type: entity_list
        entity: enrollments
        fields: [student_id, course_id, grade]
        create: true
  - name: enrollment
    route: /enrollments/{id}
    access:
      auth: true
    body:
      - type: entity_detail
        entity: enrollments
  # A param before the record: the base Save returns to escapes it.
  - name: enrollment_edit
    route: /{term}/enrollments/{id}/edit
    access:
      auth: true
    body:
      - type: entity_form
        entity: enrollments
        mode: edit
`

// TestCompositeKeyScreens generates an app whose record screens show a
// composite-key entity and runs the app's own generated e2e suite: it
// creates a record, opens its detail screen at one path segment per key
// column, then updates and deletes it through the API.
func TestCompositeKeyScreens(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and boots a generated app; skipped under -short")
	}
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	goVersion, err := repoGoVersion(repoRoot)
	if err != nil {
		t.Fatalf("repoGoVersion: %v", err)
	}
	writeTestFile(t, filepath.Join(dir, "go.mod"), "module example.com/roster\n\ngo "+goVersion+"\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => "+repoRoot+"\n")
	if err := copyGoSum(repoRoot, dir); err != nil {
		t.Fatalf("copy go.sum: %v", err)
	}
	writeTestFile(t, filepath.Join(dir, "gofastr.yml"), compositeScreensSpec)
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWD)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	runGenerateApp([]string{"gofastr.yml"})

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

	writeTestFile(t, filepath.Join(dir, "edit_save_test.go"), editSaveTest)
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

// An edit form is the record page with the unlisted fields omitted, and
// pack reads it back as the edit form with its fields allowlist, so the
// packed spec generates the same app.
func TestEditFormPacksBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gofastr.yml")
	writeTestFile(t, path, strings.Replace(compositeScreensSpec, "        mode: edit\n", "        mode: edit\n        fields: [grade]\n", 1))
	bp, err := loadAppSpec(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	dir := materializeAppSpec(t, bp)
	packed, err := packAppSpec(dir)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	var edit *AppSpecBlock
	for _, s := range packed.Screens {
		if s.Name == "enrollment_edit" && len(s.Body) == 1 {
			edit = &s.Body[0]
		}
	}
	if edit == nil || edit.Kind != "entity_form" || edit.Mode != "edit" || !slices.Equal(edit.Fields, []string{"grade"}) || edit.Props != nil {
		t.Fatalf("packed edit form = %+v, want entity_form mode edit fields [grade]", edit)
	}
	if c := packAndCheck(t, dir); len(c.HandWritten) != 0 || len(c.Unrecovered) != 0 {
		t.Fatalf("packed spec does not regenerate the app: %+v", c)
	}
}

// An entity keyed on one declared column has no "id" in its create
// response: the generated e2e test reads the record's path from the key
// column instead.
func TestE2EReadsDeclaredKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gofastr.yml")
	writeTestFile(t, path, strings.Replace(compositeScreensSpec, "primary_key: [student_id, course_id]", "primary_key: [student_id]", 1))
	bp, err := loadAppSpec(path)
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
	rows := appSpecGenerateSeedRows(decl, 1, nil)
	if len(rows) != 1 || rows[0]["student_id"] == nil {
		t.Fatalf("seed rows %v: auto_generate none dropped student_id", rows)
	}
	// The app seeds before the e2e test creates: the two keys differ.
	if create, _, _ := appSpecE2ECreateBody(decl); strings.Contains(create, `"`+rows[0]["student_id"].(string)+`"`) {
		t.Errorf("seed key %v collides with the e2e create %s", rows[0]["student_id"], create)
	}
}

// An edit screen whose record param opens the route bases at the root,
// or the record page would take /abc as its base and save to /abc/abc.
func TestRecordBaseAtRoot(t *testing.T) {
	edit := []AppSpecBlock{{Kind: "entity_form", Entity: "posts", Mode: "edit"}}
	for route, want := range map[string]string{
		"/{id}/edit":       `"/"`,
		"/{org}/{id}/edit": `"/" + url.PathEscape(p["org"])`,
		"/posts/{id}":      ``,
	} {
		if got := screenRecordBase(AppSpec{}, AppSpecScreen{Route: route, Body: edit}); got != want {
			t.Errorf("%s: base %s, want %s", route, got, want)
		}
	}
}
