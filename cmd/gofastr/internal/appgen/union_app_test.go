package appgen

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// unionCatalog is the union app's Spanish catalog: the phrase the owned
// join probe reads on the enrollment record page.
const unionCatalog = `{"union":{"screens":{"enrollments_crud":{"enrollment_record":"Ficha de matrícula"}}}}`

// TestUnionApp is the pull request's generated-app build. The app spec in
// testdata/union carries every generator feature that fits in one app, so
// one app, generated, vetted and run once, checks what the variant builds
// check one feature at a time (clitest.VariantBuild; they run in every CI
// run that is not a pull request). What cannot share an app stays a
// variant only: Postgres, output_dir, and apps without auth or without
// entities. It runs the generator the way `gofastr generate app` and
// then plain `gofastr generate` do, vets the module with go vet and with
// the repo's vettool (emitted code must pass the repo's own analyzers),
// and runs the app's own tests (the e2e suite and the catalog check)
// plus three probes: ownedJoinProbe (an owner-scoped record keyed on two
// foreign keys, read in Spanish, hidden from a second user),
// editSaveTest (an edit screen whose key values need escaping saves back
// to its record) and intIDProbe (consumer code passing an integer id
// between the repo, the id column, events, cascades and the client).
//
// It is the only build for several features whose own tests check the
// emitted source in-process and no longer compile it: per-verb access
// (TestAccessAppSpecRendersAccessLiteral), composite-key screens
// (TestCompositeKeyScreens), app spec hooks (the hooks security tests)
// and the per-screen file layout (generate_perscreen_test.go). Each was
// proved by breaking the emission and watching this test fail.
func TestUnionApp(t *testing.T) {
	if testing.Short() {
		t.Skip("generates, vets and boots the union app; skipped under -short")
	}
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := filepath.Abs(filepath.Join("testdata", "union"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	goVersion, err := clitest.RepoGoVersion(repoRoot)
	if err != nil {
		t.Fatalf("repoGoVersion: %v", err)
	}
	clitest.WriteTestFile(t, filepath.Join(dir, "go.mod"), "module example.com/union\n\ngo "+goVersion+"\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => "+repoRoot+"\n")
	if err := clitest.CopyGoSum(repoRoot, dir); err != nil {
		t.Fatalf("copy go.sum: %v", err)
	}
	if err := os.CopyFS(dir, os.DirFS(fixture)); err != nil {
		t.Fatalf("copy the union fixture: %v", err)
	}
	t.Chdir(dir)
	RunGenerateApp([]string{"gofastr.yml"})
	GenerateProject(nil)

	var src strings.Builder
	matches, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	for _, m := range matches {
		b, err := os.ReadFile(m)
		if err != nil {
			t.Fatal(err)
		}
		src.Write(b)
	}
	for _, want := range compositeScreenWants("bookings") {
		if !strings.Contains(src.String(), want) {
			t.Errorf("generated app lacks %s", want)
		}
	}
	for _, f := range []string{"cmd/union/main.gen.go", "gen/js/client.js"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("gofastr.settings.yml's codegen did not write %s: %v", f, err)
		}
	}
	// BlogPost declares no table, so the server mounts blog_post. The
	// generated CLI and JS SDK must route on that, not on the raw name.
	for f, want := range map[string]string{"cmd/union/blog-post.gen.go": `"/blog_post"`, "gen/js/client.js": `"blog_post"`} {
		b, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			t.Errorf("read %s: %v", f, err)
		} else if !strings.Contains(string(b), want) || strings.Contains(string(b), `"BlogPost"`) {
			t.Errorf("%s does not route BlogPost on %s", f, want)
		}
	}
	if t.Failed() {
		return
	}

	clitest.WriteTestFile(t, filepath.Join(dir, "locales", "es.json"), unionCatalog)
	clitest.WriteTestFile(t, filepath.Join(dir, "owned_join_test.go"), ownedJoinProbe)
	clitest.WriteTestFile(t, filepath.Join(dir, "edit_save_test.go"), editSaveTest("bookings"))
	clitest.WriteTestFile(t, filepath.Join(dir, "int_id_test.go"), intIDProbe)

	vettool, err := clitest.RepoVettool()
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"vet", "./..."}, {"vet", "-vettool=" + vettool, "./..."}} {
		cmd := exec.Command("go", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %s over the union app: %v\nevery finding is in the emitted templates (cmd/gofastr/internal/appgen), not the app spec:\n%s", strings.Join(args, " "), err, out)
		}
	}

	tests := []string{"TestE2E", "TestCatalogsMatchPhrases", "TestOwnedJoinRecord", "TestEditSavesToRecord", "TestIntIDRepo"}
	cmd := exec.Command("go", "test", "-run", "^("+strings.Join(tests, "|")+")$", "-count=1", "-v", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the union app's tests failed: %v\n%s", err, out)
	}
	for _, name := range tests {
		if !strings.Contains(string(out), "--- PASS: "+name+" ") {
			t.Fatalf("the union app's %s did not run:\n%s", name, out)
		}
	}
}
