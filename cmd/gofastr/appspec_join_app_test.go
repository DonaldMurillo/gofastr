package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ownedJoinSpec is a localized app whose enrollments join students and
// courses on a two-column key, each column a foreign key, and belong to
// the user who created them.
const ownedJoinSpec = `app:
  name: Roster
  module: example.com/roster
  locale_prefixes: true
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
  - name: students
    crud: true
    primary_key: [code]
    fields:
      - name: code
        type: string
        required: true
      - name: name
        type: string
        required: true
  - name: courses
    crud: true
    primary_key: [code]
    fields:
      - name: code
        type: string
        required: true
      - name: title
        type: string
        required: true
  - name: enrollments
    crud: true
    owner_field: user_id
    primary_key: [student_id, course_id]
    fields:
      - name: student_id
        type: string
        required: true
      - name: course_id
        type: string
        required: true
      - name: grade
        type: string
        required: true
      - name: user_id
        type: string
    relations:
      - type: belongs_to
        name: student
        entity: students
        foreign_key: student_id
      - type: belongs_to
        name: course
        entity: courses
        foreign_key: course_id
screens:
  - name: home
    route: /
    title: Roster
    body:
      - type: heading
        level: 1
        text: Roster
  - name: enrollment
    route: /enrollments/{id}
    access:
      auth: true
    body:
      - type: heading
        level: 1
        text: Enrollment record
      - type: entity_detail
        entity: enrollments
`

// ownedJoinProbe joins the generated app's e2e package. Two users sign
// up; the first enrolls a student whose code holds a "/" and reads the
// record in Spanish at one segment per key column, the second cannot
// see it at all.
const ownedJoinProbe = `package main

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

func TestOwnedJoinRecord(t *testing.T) {
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
	user := func(email string) *http.Client {
		jar, _ := cookiejar.New(nil)
		c := &http.Client{Jar: jar, Timeout: 10 * time.Second}
		creds := url.Values{"email": {email}, "password": {"roster-pw-123"}}
		if _, err := c.PostForm(base+"/auth/register", creds); err != nil {
			t.Fatalf("register %s: %v", email, err)
		}
		if _, err := c.PostForm(base+"/auth/login", creds); err != nil {
			t.Fatalf("login %s: %v", email, err)
		}
		return c
	}
	ann, bob := user("ann@example.com"), user("bob@example.com")
	for _, c := range [][2]string{
		{"/api/students", ` + "`" + `{"code": "ann/lee", "name": "Ann Lee"}` + "`" + `},
		{"/api/courses", ` + "`" + `{"code": "c 1", "title": "Chemistry"}` + "`" + `},
		{"/api/enrollments", ` + "`" + `{"student_id": "ann/lee", "course_id": "c 1", "grade": "B-probe"}` + "`" + `},
	} {
		if code, out := e2eDo(t, ann, "POST", base+c[0], c[1]); code != http.StatusCreated {
			t.Fatalf("POST %s = %d: %s", c[0], code, out)
		}
	}
	const record = "/enrollments/ann%2Flee/c%201"
	code, page := e2eDo(t, ann, "GET", base+"/es"+record, "")
	if code != http.StatusOK {
		t.Fatalf("GET /es%s = %d", record, code)
	}
	for _, want := range []string{` + "`" + `lang="es"` + "`" + `, "Ficha de matrícula", "B-probe"} {
		if !strings.Contains(page, want) {
			t.Errorf("the Spanish record page lacks %q", want)
		}
	}
	if code, out := e2eDo(t, ann, "GET", base+"/api"+record+"?include=student,course", ""); code != http.StatusOK ||
		!strings.Contains(out, "Ann Lee") || !strings.Contains(out, "Chemistry") {
		t.Errorf("GET the record with its parents = %d: %s", code, out)
	}
	if code, _ := e2eDo(t, bob, "GET", base+"/api"+record, ""); code != http.StatusNotFound {
		t.Errorf("another user reads the record: %d, want 404", code)
	}
	if _, out := e2eDo(t, bob, "GET", base+"/api/enrollments", ""); strings.Contains(out, "B-probe") {
		t.Errorf("another user lists the record: %s", out)
	}
	// The record screen answers 404 with its not-found state for a row
	// the reader cannot see.
	if code, out := e2eDo(t, bob, "GET", base+"/es"+record, ""); code != http.StatusNotFound || strings.Contains(out, "B-probe") || !strings.Contains(out, ` + "`" + `aria-label="Not found"` + "`" + `) {
		i := strings.Index(out, "<main")
		t.Errorf("another user opens the record page: %d, want 404: %.1500s", code, out[max(i, 0):])
	}
}
`

// TestOwnedJoinApp generates a localized app with an owner-scoped join
// entity keyed on two foreign keys and runs its own tests: the catalog
// check, the generated e2e suite, and a probe that reads one user's
// record in Spanish and keeps it from a second user.
func TestOwnedJoinApp(t *testing.T) {
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
	writeTestFile(t, filepath.Join(dir, "gofastr.yml"), ownedJoinSpec)
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWD)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	runGenerateApp([]string{"gofastr.yml"})

	writeTestFile(t, filepath.Join(dir, "locales", "es.json"),
		`{"roster":{"screens":{"home":{"roster":"Lista"},"enrollments_crud":{"enrollment_record":"Ficha de matrícula"}}}}`)
	writeTestFile(t, filepath.Join(dir, "owned_join_test.go"), ownedJoinProbe)
	cmd := exec.Command("go", "test", "-run", "^(TestE2E|TestCatalogsMatchPhrases|TestOwnedJoinRecord)$", "-count=1", "-v", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated app tests failed: %v\n%s", err, out)
	}
	for _, name := range []string{"TestE2E", "TestCatalogsMatchPhrases", "TestOwnedJoinRecord"} {
		if !strings.Contains(string(out), "--- PASS: "+name+" ") {
			t.Fatalf("generated %s did not run:\n%s", name, out)
		}
	}
}
