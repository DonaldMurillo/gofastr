package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// #503: the init scaffold shipped no sign-in surface and no API prefix.
// Every entity URL answered 401 with no login screen to get past it, and
// a screen at an entity's name panicked at Mount on the route collision.

func scaffoldApp(t *testing.T, args ...string) string {
	t.Helper()
	dir := t.TempDir()
	covT_chdir(t, dir)
	covT_capStdout(t, func() { runInit(args) })
	return filepath.Join(dir, args[0])
}

func readScaffold(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

var jwtSecretLine = regexp.MustCompile(`(?m)^JWT_SECRET=([0-9a-f]{64})$`)

func TestInitWiresAuthAndAPIPrefix(t *testing.T) {
	root := scaffoldApp(t, "authapp")
	mainGo := readScaffold(t, root, "main.go")
	for _, want := range []string{
		`framework.WithAPIPrefix("/api")`,
		`auth.NewEntityUserStore(db, "auth_users")`,
		`auth.NewEntitySessionStore(db, "auth_sessions")`,
		`auth.NewCorePlugin()`,
		`authMgr.Init(fwApp)`,
		`auth.SessionMiddleware(authMgr)`,
		`os.Getenv("JWT_SECRET")`,
		`DevMode:      dev.Enabled(),`,
		`site.Register("/login", &LoginScreen{}, nil)`,
		`site.Register("/register", &RegisterScreen{}, nil)`,
	} {
		if !strings.Contains(mainGo, want) {
			t.Errorf("main.go missing %s:\n%s", want, mainGo)
		}
	}

	env := readScaffold(t, root, ".env")
	m := jwtSecretLine.FindStringSubmatch(env)
	if m == nil {
		t.Fatalf(".env must carry a 32-byte hex JWT_SECRET:\n%s", env)
	}
	if strings.Contains(mainGo, m[1]) {
		t.Error("the JWT secret must live only in .env, never in main.go")
	}
	// Dev mode comes from the dev loop (GOFASTR_DEV), never from .env: a
	// .env copied next to a deployed binary must not relax auth.
	if strings.Contains(env, "APP_ENV") || strings.Contains(env, "GOFASTR_DEV") {
		t.Errorf(".env must not switch auth into dev mode:\n%s", env)
	}
	if fi, err := os.Stat(filepath.Join(root, ".env")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf(".env mode = %v (%v), want 0600", fi.Mode().Perm(), err)
	}
	other := jwtSecretLine.FindStringSubmatch(readScaffold(t, scaffoldApp(t, "authapp2"), ".env"))
	if other == nil || other[1] == m[1] {
		t.Error("each scaffold must mint its own JWT secret")
	}

	screens := readScaffold(t, root, "screens_auth.go")
	for _, want := range []string{
		"type LoginScreen struct",
		"type RegisterScreen struct",
		`Action: "/auth/login"`,
		`Action: "/auth/register"`,
		`Value: "/login?registered=1"`,
		`next := q.Get("next")`,
		"Notice:",
		"func authError(ctx context.Context) render.HTML",
	} {
		if !strings.Contains(screens, want) {
			t.Errorf("screens_auth.go missing %s:\n%s", want, screens)
		}
	}
	// The register redirect is the same for a new and a taken address
	// (battery/auth anti-enumeration), so no copy may claim either.
	if strings.Contains(screens, "already registered") {
		t.Errorf("screens_auth.go must not tell a caller an email is taken:\n%s", screens)
	}

	home := readScaffold(t, root, "screens.go")
	for _, want := range []string{`Href: "/login"`, `Href: "/register"`, "/api/posts"} {
		if !strings.Contains(home, want) {
			t.Errorf("screens.go missing %s:\n%s", want, home)
		}
	}
}

func TestInitNoEntitySkipsAuth(t *testing.T) {
	root := scaffoldApp(t, "uionly", "--no-entity")
	if _, err := os.Stat(filepath.Join(root, "screens_auth.go")); !os.IsNotExist(err) {
		t.Errorf("--no-entity has no database for auth: screens_auth.go must not exist (%v)", err)
	}
	mainGo := readScaffold(t, root, "main.go")
	for _, unwanted := range []string{"battery/auth", `framework.WithAPIPrefix(`, "LoginScreen"} {
		if strings.Contains(mainGo, unwanted) {
			t.Errorf("--no-entity main.go must not contain %s", unwanted)
		}
	}
	if strings.Contains(readScaffold(t, root, ".env"), "JWT_SECRET") {
		t.Error("--no-entity .env must not carry a JWT secret")
	}
}

// `gofastr init .` rewrites .env with a fresh JWT_SECRET. In a repo that
// already tracks .env, the .gitignore entry the scaffold adds does not
// untrack it, so the next commit would publish the signing key. Init
// must refuse before writing anything.
func TestInitRefusesTrackedEnv(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	covT_chdir(t, dir)
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", ".")
	const original = "DATABASE_URL=postgres://keep-me\n"
	if err := os.WriteFile(".env", []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", ".env")

	var code int
	out := covT_capStdout(t, func() { code = covT_capExit(t, func() { runInit([]string{"."}) }) })
	if code != 1 {
		t.Fatalf("init . over a tracked .env exited %d, want 1:\n%s", code, out)
	}
	if !strings.Contains(out, "git rm --cached .env") {
		t.Errorf("refusal should name the fix:\n%s", out)
	}
	if b, _ := os.ReadFile(".env"); string(b) != original {
		t.Errorf("tracked .env was rewritten:\n%s", b)
	}
	if _, err := os.Stat("main.go"); err == nil {
		t.Error("init wrote main.go before refusing")
	}
}
