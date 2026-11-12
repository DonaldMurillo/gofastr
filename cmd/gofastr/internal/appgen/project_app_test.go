package appgen

import (
	"os"
	"path/filepath"
	"testing"
)

// Other batteries take a Config with a PathPrefix too (print.Config). Only
// admin.Config names the admin path, whichever literal comes last.
func TestProjectAdminPathIgnoresOtherConfig(t *testing.T) {
	dir := t.TempDir()
	write := func(name, src string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("app.go", "package main\n")
	write("main.go", `package main

func main() {
	adminCfg := admin.Config{PathPrefix: "/backoffice", Title: appName}
	printer := print.New(print.Config{PathPrefix: "/print"})
	_, _ = adminCfg, printer
}
`)
	app, ok := projectRouteApp(dir)
	if !ok {
		t.Fatal("projectRouteApp: project not read")
	}
	if !app.Admin.Enabled || app.Admin.Path != "/backoffice" {
		t.Fatalf("admin = %+v, want enabled at /backoffice", app.Admin)
	}
}

// An explicit base_url of http://localhost:8080 is a literal like any
// other; only the devBaseURL constant means the spec set none. Reading
// the literal as unset made --add refuse an absolute login action the
// whole spec accepts.
func TestProjectBaseURLExplicitLocalhost(t *testing.T) {
	for fallback, want := range map[string]string{
		`"http://localhost:8080"`: "http://localhost:8080",
		`"https://shop.example"`:  "https://shop.example",
		"devBaseURL":              "",
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "app.go"), []byte("package main\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		src := "package main\n\nfunc appBaseURL() string {\n\treturn getEnv(\"APP_BASE_URL\", " + fallback + ")\n}\n"
		if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
		app, ok := projectRouteApp(dir)
		if !ok {
			t.Fatal("projectRouteApp: project not read")
		}
		if app.BaseURL != want {
			t.Errorf("fallback %s: BaseURL = %q, want %q", fallback, app.BaseURL, want)
		}
	}
}
