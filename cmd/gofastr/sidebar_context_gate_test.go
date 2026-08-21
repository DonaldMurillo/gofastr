package main

import (
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The generated app shell's sidebar is built per request so its footer and its
// role-gated entries reflect the live session. That fix has been reverted once
// already in review, and a mutation audit showed the whole cmd/gofastr suite
// stayed green when the emission was reverted to a static
// `ui.Sidebar(cfg)`. The regression it repairs (admin-only nav entries
// rendered to anonymous visitors, and "Sign out" offered to someone with no
// session) shipped with no guard at all.
//
// examples/meridian's blueprint does declare a role-gated nav item, but its app
// is hand-written (examples/meridian/app.go builds its own sidebar), so nothing
// in the tree exercises the GENERATED emission with one. This test therefore
// writes its own minimal blueprint, generates it, builds it, boots it, and
// reads what an anonymous caller and a signed-in one actually receive.
const sidebarGateBlueprint = `app:
  name: SidebarGate
  module: local/sidebargate
  db:
    driver: sqlite
    url: file:sidebargate.db
  auth:
    enabled: true
    dev_mode: true
  theme:
    primary: "#1E293B"
    dark:
      primary: "#93C5FD"

entities:
  - name: notes
    crud: true
    access:
      read:
      create: notes:write
      update: notes:write
      delete: notes:admin
    fields:
      - name: title
        type: string
        required: true

nav:
  - label: Notes
    href: /notes
  - label: Admin Console
    href: /admin-console
    role: admin

screens:
  - name: notes
    route: /notes
    title: Notes
    body:
      - type: entity_list
        entity: notes
        fields: [title]
`

func TestGeneratedSidebarIsResolvedPerRequest(t *testing.T) {
	if testing.Short() {
		t.Skip("generates, builds, and boots an app")
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
	writeTestFile(t, filepath.Join(work, "gofastr.yml"), sidebarGateBlueprint)
	gen := exec.Command(binPath, "generate", "--from=gofastr.yml")
	gen.Dir = work
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("generate: %v\n%s", err, out)
	}

	goVersion, err := repoGoVersion(repoRoot)
	if err != nil {
		t.Fatalf("repoGoVersion: %v", err)
	}
	writeTestFile(t, filepath.Join(work, "go.mod"),
		"module local/sidebargate\n\ngo "+goVersion+
			"\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => "+repoRoot+"\n")
	if err := copyGoSum(repoRoot, work); err != nil {
		t.Fatalf("copy go.sum: %v", err)
	}

	appBin := testExecutablePath(filepath.Join(work, "sidebargate-app"))
	appBuild := exec.Command("go", "build", "-mod=mod", "-o", appBin, ".")
	appBuild.Dir = work
	if out, err := appBuild.CombinedOutput(); err != nil {
		t.Fatalf("generated app did not build: %v\n%s", err, out)
	}

	addr := freeAddr(t)
	cmd := exec.Command(appBin)
	cmd.Dir = work
	cmd.Env = append(os.Environ(), "PORT="+addr, "DATABASE_URL=file:"+filepath.Join(work, "gate.db"))
	// syncBuffer, not bytes.Buffer: os/exec copies the child's output from its
	// own goroutines until Wait returns, and Wait runs in t.Cleanup, so every
	// read of this below races the copier. The package already has the
	// mutex-guarded writer for exactly this.
	var output syncBuffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatalf("start app: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	baseURL := "http://" + addr
	waitForHTTP(t, baseURL+"/notes", &output)

	resp, err := http.Get(baseURL + "/notes")
	if err != nil {
		t.Fatalf("GET /notes: %v", err)
	}
	body := make([]byte, 0, 64*1024)
	buf := make([]byte, 8192)
	for {
		n, readErr := resp.Body.Read(buf)
		body = append(body, buf[:n]...)
		if readErr != nil {
			break
		}
	}
	resp.Body.Close()
	html := string(body)

	// Anonymous: no session, so no Sign out, and a role-gated entry must not
	// appear. (Role filtering also survives a static sidebar, because the
	// layout renders the slot through SafeRenderCtx, so this pair documents
	// the contract but does not, on its own, distinguish the two shapes.)
	if strings.Contains(html, "Sign out") {
		t.Errorf("anonymous visitor is offered Sign out:\n%s", snippetAround(html, "Sign out"))
	}
	if strings.Contains(html, "Admin Console") {
		t.Errorf("anonymous visitor sees the role-gated nav entry:\n%s", snippetAround(html, "Admin Console"))
	}
	if !strings.Contains(html, "Notes") {
		t.Fatalf("the sidebar rendered no ordinary nav entry:\n%s", html)
	}

	// This blueprint enables auth but routes no login screen, the shape six
	// of the seven shipped blueprints have. The sidebar is the app shell's
	// only auth affordance, so offering "Sign in" here would send a
	// first-time visitor to a route nothing serves. Nothing else in this file
	// pins that branch: without this check the guard around the Sign-in
	// emission can be deleted and every assertion still passes.
	if strings.Contains(html, "Sign in") {
		t.Errorf("anonymous visitor is offered Sign in, but no screen routes a login form — the link goes nowhere:\n%s",
			snippetAround(html, "Sign in"))
	}

	// THE DISCRIMINATING ASSERTION. A sidebar built once at startup resolves
	// its footer from a background context. Anonymous, forever. Only a
	// per-request sidebar can offer Sign out to someone who is signed in, so
	// this is the assertion that fails if the emission reverts to
	// `ui.Sidebar(sidebarConfig(context.Background()))`.
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	creds := strings.NewReader(`{"email":"gate@example.com","password":"str0ng-passphrase"}`)
	regResp, err := client.Post(baseURL+"/auth/register", "application/json", creds)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	regResp.Body.Close()
	loginResp, err := client.Post(baseURL+"/auth/login", "application/json",
		strings.NewReader(`{"email":"gate@example.com","password":"str0ng-passphrase"}`))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("login = %d, want 200 — the signed-in half of this gate cannot run", loginResp.StatusCode)
	}

	authedResp, err := client.Get(baseURL + "/notes")
	if err != nil {
		t.Fatalf("authenticated GET /notes: %v", err)
	}
	authedBody := readAllBody(authedResp)
	authedResp.Body.Close()

	if !strings.Contains(authedBody, "Sign out") {
		t.Errorf("a signed-in user is not offered Sign out — the sidebar footer was resolved once at startup instead of per request:\n%s", snippetAround(authedBody, "Notes"))
	}
	if strings.Contains(authedBody, "Sign in") {
		t.Errorf("a signed-in user is still offered Sign in — the sidebar is not seeing the session:\n%s", snippetAround(authedBody, "Sign in"))
	}
}

func readAllBody(resp *http.Response) string {
	body := make([]byte, 0, 64*1024)
	buf := make([]byte, 8192)
	for {
		n, err := resp.Body.Read(buf)
		body = append(body, buf[:n]...)
		if err != nil {
			break
		}
	}
	return string(body)
}

func snippetAround(s, needle string) string {
	i := strings.Index(s, needle)
	if i < 0 {
		return ""
	}
	start := max(0, i-200)
	end := min(len(s), i+200)
	return s[start:end]
}
