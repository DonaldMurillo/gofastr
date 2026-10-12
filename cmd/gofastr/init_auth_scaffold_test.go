package main

import (
	"os"
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
		`os.Getenv("APP_ENV") == "development"`,
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
	if !strings.Contains(env, "\nAPP_ENV=development\n") {
		t.Errorf(".env must opt the local app into auth dev mode:\n%s", env)
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
