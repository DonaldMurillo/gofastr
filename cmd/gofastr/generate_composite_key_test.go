package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// compositeKeySpec declares an entity keyed on two columns, with a state
// move so the transition methods are generated too.
const compositeKeySpec = `app:
  name: keys
entities:
  - name: enrollments
    crud: true
    public: true
    timestamps: false
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
      - name: status
        type: enum
        values: [enrolled, done]
        default: enrolled
    states:
      field: status
      initial: [enrolled]
      transitions:
        - key: finish
          from: [enrolled]
          to: done
`

// compositeKeyTestSource drives the generated typed repo, event helper
// and HTTP client for compositeKeySpec against a live app. The key values
// carry a space, a slash and the characters url.PathEscape and
// encodeURIComponent disagree on, so every path and record id is spelled
// the one way the server accepts.
const compositeKeyTestSource = `package integration_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"

	"github.com/DonaldMurillo/gofastr/framework"

	"example.com/keys/gen/entities"
	"example.com/keys/gen/entities/client"
)

func TestCompositeKeys(t *testing.T) {
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

	var mu sync.Mutex
	var deleted []string
	defer entities.OnEnrollmentsDeleted(app, func(_ context.Context, id string) error {
		mu.Lock()
		defer mu.Unlock()
		deleted = append(deleted, id)
		return nil
	})()

	s, c := "ann lee/1", "x@y!z"
	repo := entities.NewEnrollmentsRepo(app)
	if _, err := repo.Create(ctx, entities.EnrollmentsInput{StudentId: s, CourseId: c, Grade: new("A")}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if got, err := repo.Get(ctx, s, c); err != nil || got.StudentId != s || got.CourseId != c || got.Grade != "A" {
		t.Fatalf("get = %+v, %v", got, err)
	}
	if got, err := repo.Update(ctx, s, c, entities.EnrollmentsPatch{Grade: new("B")}); err != nil || got.Grade != "B" {
		t.Fatalf("update = %+v, %v", got, err)
	}
	if ok, err := repo.Exists(ctx, s, c); !ok || err != nil {
		t.Fatalf("exists = %v, %v", ok, err)
	}
	if ok, err := repo.Exists(ctx, s, "other"); ok || err != nil {
		t.Fatalf("exists on a missing key = %v, %v", ok, err)
	}
	if got, err := repo.Transition(ctx, s, c, entities.EnrollmentsMoveFinish); err != nil || got.Status != entities.EnrollmentsStatusDone {
		t.Fatalf("transition = %+v, %v", got, err)
	}
	if _, err := repo.Create(ctx, entities.EnrollmentsInput{StudentId: "r2", CourseId: c}); err != nil {
		t.Fatalf("create r2: %v", err)
	}
	if got, err := repo.BatchUpdate(ctx, []string{entities.EnrollmentsID("r2", c)}, []entities.EnrollmentsPatch{{Grade: new("R")}}); err != nil || len(got) != 1 || got[0].Grade != "R" {
		t.Fatalf("repo batch update = %+v, %v", got, err)
	}
	if err := repo.BatchDelete(ctx, []string{entities.EnrollmentsID("r2", c)}); err != nil {
		t.Fatalf("repo batch delete: %v", err)
	}

	srv := httptest.NewServer(app.Router())
	defer srv.Close()
	api := client.NewClient(srv.URL, nil)
	if got, err := api.GetEnrollments(ctx, s, c); err != nil || got.StudentId != s || got.Grade != "B" {
		t.Fatalf("client get = %+v, %v", got, err)
	}
	if _, err := api.CreateEnrollments(ctx, client.EnrollmentsInput{StudentId: "s2", CourseId: c}); err != nil {
		t.Fatalf("client create: %v", err)
	}
	if got, err := api.PatchEnrollments(ctx, "s2", c, client.EnrollmentsPatch{Grade: new("C")}); err != nil || got.Grade != "C" {
		t.Fatalf("client patch = %+v, %v", got, err)
	}
	if got, err := api.UpdateEnrollments(ctx, "s2", c, client.EnrollmentsInput{StudentId: "s2", CourseId: c, Grade: new("D")}); err != nil || got.Grade != "D" {
		t.Fatalf("client update = %+v, %v", got, err)
	}
	if got, err := api.FinishEnrollments(ctx, "s2", c); err != nil || got.Status != "done" {
		t.Fatalf("client transition = %+v, %v", got, err)
	}
	res, err := api.BatchUpdateEnrollments(ctx, []client.EnrollmentsBatchPatch{{StudentId: "s2", CourseId: c, Grade: new("E")}})
	if err != nil || !res.Committed {
		t.Fatalf("client batch update = %+v, %v", res, err)
	}
	if got, err := api.GetEnrollments(ctx, "s2", c); err != nil || got.Grade != "E" {
		t.Fatalf("after batch update = %+v, %v", got, err)
	}
	res, err = api.BatchDeleteEnrollments(ctx, []string{client.EnrollmentsID("s2", c)})
	if err != nil || !res.Committed {
		t.Fatalf("client batch delete = %+v, %v", res, err)
	}
	var apiErr *client.APIError
	if _, err := api.GetEnrollments(ctx, "s2", c); !errors.As(err, &apiErr) || apiErr.Status != 404 {
		t.Fatalf("get after batch delete: %v", err)
	}
	if err := api.DeleteEnrollments(ctx, s, c); err != nil {
		t.Fatalf("client delete: %v", err)
	}

	want := []string{entities.EnrollmentsID("r2", c), entities.EnrollmentsID("s2", c), entities.EnrollmentsID(s, c)}
	if want[2] != client.EnrollmentsID(s, c) || want[2] != framework.JoinKey([]string{s, c}) {
		t.Fatalf("record ids disagree: typed %q, client %q", want[2], client.EnrollmentsID(s, c))
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		got := slices.Clone(deleted)
		mu.Unlock()
		slices.Sort(got)
		slices.Sort(want)
		if slices.Equal(got, want) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("OnEnrollmentsDeleted ids = %q, want %q", got, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestCompositeKeyCLI drives the generated CLI's record verbs, which take
// one positional argument per key column, against a live app.
func TestCompositeKeyCLI(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	app := framework.NewApp(framework.WithDB(db), framework.WithoutDefaultMiddleware(), framework.WithAPIPrefix("/api"))
	entities.RegisterAll(app)
	if err := framework.AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	srv := httptest.NewServer(app.Router())
	defer srv.Close()

	bin := filepath.Join(t.TempDir(), "keys")
	if out, err := exec.Command("go", "build", "-o", bin, "example.com/keys/gen/cmd/keys").CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, out)
	}
	home := t.TempDir()
	run := func(args ...string) (map[string]any, error) {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Env = append(os.Environ(), "HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, ".config"), "KEYS_URL="+srv.URL)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return nil, errors.New(err.Error() + ": " + string(out))
		}
		var row map[string]any
		_ = json.Unmarshal(out, &row)
		return row, nil
	}
	must := func(args ...string) map[string]any {
		t.Helper()
		row, err := run(args...)
		if err != nil {
			t.Fatalf("keys %q: %v", args, err)
		}
		return row
	}

	s, c := "ann lee/1", "x@y!z"
	body, _ := json.Marshal(map[string]string{"student_id": s, "course_id": c, "grade": "A"})
	must("enrollments", "create", "--json", string(body))
	if row := must("enrollments", "get", s, c); row["studentId"] != s || row["grade"] != "A" {
		t.Fatalf("get = %v", row)
	}
	if row := must("enrollments", "patch", s, c, "--grade", "B"); row["grade"] != "B" {
		t.Fatalf("patch = %v", row)
	}
	// The path names the record: patch offers no flag for a key column.
	if _, err := run("enrollments", "patch", s, c, "--student-id", "x"); err == nil || !strings.Contains(err.Error(), "student-id") {
		t.Fatalf("patch --student-id = %v, want an unknown-flag refusal", err)
	}
	body, _ = json.Marshal(map[string]string{"student_id": s, "course_id": c, "grade": "C"})
	if row := must("enrollments", "update", s, c, "--json", string(body)); row["grade"] != "C" {
		t.Fatalf("update = %v", row)
	}
	if row := must("enrollments", "finish", s, c); row["status"] != "done" {
		t.Fatalf("finish = %v", row)
	}
	must("enrollments", "batch-delete", entities.EnrollmentsID(s, c))
	if _, err := run("enrollments", "get", s, c); err == nil {
		t.Fatalf("get after batch-delete succeeded")
	}
	must("enrollments", "create", "--student-id", "bo", "--course-id", "c/2")
	must("enrollments", "delete", "bo", "c/2")
	if _, err := run("enrollments", "get", "bo", "c/2"); err == nil {
		t.Fatalf("get after delete succeeded")
	}
	cmd := exec.Command(bin, "enrollments", "get", s)
	cmd.Env = append(os.Environ(), "HOME="+home, "KEYS_URL="+srv.URL)
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 2 || !strings.Contains(string(out), "usage: keys enrollments get <student_id> <course_id>") {
		t.Fatalf("get with one of two key values = %v: %s", err, out)
	}
}
`

// TestCompositeKeyRoundTrip generates an app whose entity has a composite
// primary key and runs compositeKeyTestSource against the emitted repo,
// event helper and HTTP client.
func TestCompositeKeyRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a generated module; skipped under -short")
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
	goMod := "module example.com/keys\n\ngo " + goVersion + "\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => " + repoRoot + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyGoSum(repoRoot, dir); err != nil {
		t.Fatalf("copy go.sum: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gofastr.yml"), []byte(compositeKeySpec), 0o644); err != nil {
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
	runGenerateApp([]string{"gofastr.yml", "--out=gen"})
	if err := os.Chdir(filepath.Join(dir, "gen")); err != nil {
		t.Fatal(err)
	}
	runGenerateCLI([]string{"--binary=keys"})
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	itDir := filepath.Join(dir, "integration")
	if err := os.MkdirAll(itDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(itDir, "keys_integration_test.go"), []byte(compositeKeyTestSource), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-mod=mod", "-count=1", "./integration/...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated composite-key integration failed: %v\n%s", err, out)
	}
}
