package appgen

import (
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	coreoa "github.com/DonaldMurillo/gofastr/core/openapi"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/openapi"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// The OpenAPI CLI reads a composite-key app's own document: the record
// operations take a flag per key column, and each value reaches the app
// as its own escaped path segment.
func TestOpenAPICLICompositeKeys(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a generated CLI; skipped under -short")
	}
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	app := framework.NewApp(framework.WithDB(db), framework.WithoutDefaultMiddleware(), framework.WithAPIPrefix("/api"))
	app.Entity("enrollments", framework.EntityConfig{
		Exposure:   &framework.ExposureConfig{Public: true},
		PrimaryKey: []string{"student_id", "course_id"},
		Fields: []schema.Field{
			{Name: "student_id", Type: schema.String, Required: true},
			{Name: "course_id", Type: schema.String, Required: true},
			{Name: "grade", Type: schema.String},
		},
	}.WithTimestamps(false))
	if err := framework.AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	all := func(*entity.Entity) bool { return true }
	served := httptest.NewRecorder()
	coreoa.PublicHandler(openapi.EntityOpenAPIWithBulk(app.Registry, "keys", "1.0.0", all, all, "/api")).
		ServeHTTP(served, httptest.NewRequest("GET", "/openapi.json", nil))
	doc := served.Body.Bytes()
	if !json.Valid(doc) {
		t.Fatalf("openapi.json = %d: %s", served.Code, doc)
	}
	srv := httptest.NewServer(app.Router())
	defer srv.Close()

	dir := oaTempModule(t)
	if err := os.WriteFile(filepath.Join(dir, "openapi.json"), doc, 0o644); err != nil {
		t.Fatal(err)
	}
	RunGenerateCLI([]string{"--from-openapi=openapi.json", "--binary=oapp", "--force"})
	bin := clitest.TestExecutablePath(filepath.Join(dir, "oapp-bin"))
	build := exec.Command("go", "build", "-o", bin, "./cmd/oapp")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	run := func(args ...string) string {
		t.Helper()
		out, err := exec.Command(bin, append(args, "--url", srv.URL)...).CombinedOutput()
		if err != nil {
			t.Fatalf("oapp %q: %v\n%s", args, err, out)
		}
		return string(out)
	}

	s, c := "ann lee/1", "x@y!z"
	run("create-enrollments", "--json", `{"student_id":"ann lee/1","course_id":"x@y!z","grade":"A"}`)
	if out := run("get-enrollments", "--student-id", s, "--course-id", c); !strings.Contains(out, `"grade": "A"`) {
		t.Fatalf("get = %s", out)
	}
	// The PUT body repeats the key columns; the path flags carry them.
	if out := run("update-enrollments", "--student-id", s, "--course-id", c, "--grade", "B"); !strings.Contains(out, `"grade": "B"`) {
		t.Fatalf("update = %s", out)
	}
	run("delete-enrollments", "--student-id", s, "--course-id", c)
	if out, err := exec.Command(bin, "get-enrollments", "--student-id", s, "--course-id", c, "--url", srv.URL).CombinedOutput(); err == nil {
		t.Fatalf("get after delete succeeded: %s", out)
	}
}
