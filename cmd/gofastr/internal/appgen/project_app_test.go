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
