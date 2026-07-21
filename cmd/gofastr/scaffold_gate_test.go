package main

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Executable-scaffold gate. The site's /get-started page walks a reader
// through `gofastr init` and makes runtime claims about the result: the
// home screen renders, the sample posts entity exists in
// entities/entities.go with the declaration the page shows, and
// anonymous CRUD answers 401 (secure by default). This test runs the
// real init, builds the scaffolded app against the working tree, boots
// it, and asserts those claims — so the scaffold can't drift away from
// the page that teaches it.
func TestInitScaffoldBootsSecureByDefault(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and boots a scaffolded app")
	}
	repoRoot := repoRootDir(t)

	binDir := t.TempDir()
	binPath := testExecutablePath(filepath.Join(binDir, "gofastr"))
	build := exec.Command("go", "build", "-o", binPath, "./cmd/gofastr")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build gofastr: %v\n%s", err, out)
	}

	work := t.TempDir()
	initCmd := exec.Command(binPath, "init", "blog")
	initCmd.Dir = work
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("gofastr init: %v\n%s", err, out)
	}
	appDir := filepath.Join(work, "blog")

	// The sample entity is what /get-started shows — pin its shape.
	entSrc, err := os.ReadFile(filepath.Join(appDir, "entities", "entities.go"))
	if err != nil {
		t.Fatalf("scaffold lost entities/entities.go: %v", err)
	}
	for _, want := range []string{`app.Entity("posts"`, "entity.EntityConfig{", "[]schema.Field{", `{Name: "title"`} {
		if !strings.Contains(string(entSrc), want) {
			t.Fatalf("scaffolded entities.go lost %q — /get-started teaches this exact declaration:\n%s", want, entSrc)
		}
	}

	// Point the scaffold's module at the working tree and build it.
	goVersion, err := repoGoVersion(repoRoot)
	if err != nil {
		t.Fatalf("repoGoVersion: %v", err)
	}
	goMod := "module local/blog\n\ngo " + goVersion + "\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => " + repoRoot + "\n"
	writeTestFile(t, filepath.Join(appDir, "go.mod"), goMod)
	if err := copyGoSum(repoRoot, appDir); err != nil {
		t.Fatalf("copy go.sum: %v", err)
	}

	appBin := testExecutablePath(filepath.Join(appDir, "scaffold-app"))
	appBuild := exec.Command("go", "build", "-mod=mod", "-o", appBin, ".")
	appBuild.Dir = appDir
	if out, err := appBuild.CombinedOutput(); err != nil {
		t.Fatalf("scaffolded app did not build: %v\n%s", err, out)
	}

	addr := freeAddr(t)
	cmd := exec.Command(appBin)
	cmd.Dir = appDir
	cmd.Env = append(os.Environ(),
		"PORT="+addr,
		"DATABASE_URL=file:"+filepath.Join(appDir, "gate.db"),
	)
	output := &bytes.Buffer{}
	cmd.Stdout = output
	cmd.Stderr = output
	if err := cmd.Start(); err != nil {
		t.Fatalf("start scaffolded app: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	baseURL := "http://" + addr
	waitForHTTP(t, baseURL+"/", output)

	// The scaffolded home screen renders (PageHeader carries the app name).
	homeResp, err := http.Get(baseURL + "/")
	if err != nil {
		t.Fatal(err)
	}
	homeBody, _ := io.ReadAll(homeResp.Body)
	homeResp.Body.Close()
	if homeResp.StatusCode != http.StatusOK || !strings.Contains(string(homeBody), "blog") {
		t.Fatalf("GET / = %d — want 200 with the scaffolded home screen\n%s", homeResp.StatusCode, output.String())
	}

	// Secure by default: the sample entity has no Public/Access/OwnerField,
	// so anonymous CRUD refuses — the 401 /get-started demonstrates.
	for _, probe := range []struct {
		method, path string
	}{{"GET", "/posts"}, {"POST", "/posts"}} {
		req, _ := http.NewRequest(probe.method, baseURL+probe.path, strings.NewReader(`{"title":"x"}`))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("anonymous %s %s = %d, want 401 — the scaffold must stay secure by default\n%s", probe.method, probe.path, resp.StatusCode, output.String())
		}
	}
}
