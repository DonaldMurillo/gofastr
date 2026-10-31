package appgen

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// The generated e2e signed in and then accepted any 200 (2026-10-27
// G14): with no session, each gated screen bounced to the login page,
// the client followed, and the login page passed for the screen. The
// suite runs here against a stand-in app whose login sets a working
// cookie or none. (A Secure cookie is not a way in: Go's cookie jar
// sends it to the loopback host the suite dials.)
func TestGeneratedE2ERequiresASession(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs a generated suite")
	}
	root := clitest.RepoRootDir(t)
	dir := t.TempDir()
	version, err := clitest.RepoGoVersion(root)
	if err != nil {
		t.Fatal(err)
	}
	clitest.WriteTestFile(t, filepath.Join(dir, "go.mod"), "module local/sessionprobe\n\ngo "+version+"\nrequire github.com/DonaldMurillo/gofastr v0.0.0\nreplace github.com/DonaldMurillo/gofastr => "+root+"\n")
	if err := clitest.CopyGoSum(root, dir); err != nil {
		t.Fatal(err)
	}
	clitest.WriteTestFile(t, filepath.Join(dir, "e2e_test.go"), renderAppSpecE2ETest(authBaseSpec("")))
	clitest.WriteTestFile(t, filepath.Join(dir, "main.go"), `package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"
)

func page(w http.ResponseWriter, text string) {
	fmt.Fprint(w, "<html><body>acct "+text+" ", strings.Repeat("padding ", 30), "</body></html>")
}

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { page(w, "home") })
	http.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) { page(w, "sign in") })
	http.HandleFunc("/signup", func(w http.ResponseWriter, r *http.Request) { page(w, "sign up") })
	http.HandleFunc("/dashboard", func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("sess"); err != nil || c.Value != "ok" {
			http.Redirect(w, r, "/login?next=/dashboard", http.StatusSeeOther)
			return
		}
		page(w, "dashboard")
	})
	http.HandleFunc("/auth/login", func(w http.ResponseWriter, r *http.Request) {
		switch os.Getenv("FAKE_LOGIN") {
		case "ok":
			http.SetCookie(w, &http.Cookie{Name: "sess", Value: "ok", Path: "/"})
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})
	if err := http.ListenAndServe(os.Getenv("PORT"), nil); err != nil {
		panic(err)
	}
}
`)
	for _, mode := range []string{"ok", "none"} {
		cmd := exec.Command("go", "test", "-run", "^TestE2E$", "-count=1")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "FAKE_LOGIN="+mode, "GOFLAGS=-mod=mod")
		out, err := cmd.CombinedOutput()
		if mode == "ok" {
			if err != nil {
				t.Fatalf("a working session failed the generated suite: %v\n%s", err, out)
			}
			continue
		}
		if err == nil || !strings.Contains(string(out), "authed screen /dashboard = 303") {
			t.Errorf("login %s: the generated suite passed with no session, or failed for another reason: %v\n%s", mode, err, out)
		}
	}
}
