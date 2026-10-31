package appgen

// ownedJoinProbe joins the generated app's e2e package; TestUnionApp
// writes it into the union app, whose enrollments join students and
// courses on a two-column key, each column a foreign key, and belong to
// the user who created them. Two users sign up; the first enrolls a
// student whose code holds a "/" and reads the record in Spanish at one
// segment per key column, the second cannot see it at all.
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
		if _, err := c.PostForm(base+"/account/register", creds); err != nil {
			t.Fatalf("register %s: %v", email, err)
		}
		if _, err := c.PostForm(base+"/account/login", creds); err != nil {
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
	// The app's auth mounts at base_path /account. The Sign out form the
	// page renders posts to its logout route and ends the session. The
	// page is Spanish, so the action carries /es, which the router strips.
	form := page[max(strings.Index(page, ` + "`" + `data-cui-comp="ui-sign-out"` + "`" + `), 0):]
	at := strings.Index(form, ` + "`" + `action="` + "`" + `)
	if !strings.Contains(page, ` + "`" + `data-cui-comp="ui-sign-out"` + "`" + `) || at < 0 {
		t.Fatal("the record page renders no Sign out form")
	}
	action := form[at+len(` + "`" + `action="` + "`" + `):]
	action = action[:strings.IndexByte(action, '"')]
	if action != "/es/account/logout" {
		t.Errorf("Sign out posts to %q, want /es/account/logout", action)
	}
	if code, _ := e2eDo(t, ann, "GET", base+"/account/me", ""); code != http.StatusOK {
		t.Fatalf("GET /account/me signed in = %d, want 200", code)
	}
	if _, err := ann.PostForm(base+action, url.Values{"next": {"/"}}); err != nil {
		t.Fatalf("sign out: %v", err)
	}
	if code, _ := e2eDo(t, ann, "GET", base+"/account/me", ""); code != http.StatusUnauthorized {
		t.Errorf("GET /account/me after Sign out = %d, want 401", code)
	}
}
`
