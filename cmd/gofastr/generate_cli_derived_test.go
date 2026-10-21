package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/codegen"
)

// writeLegacyCLIFile writes name under the CLI dir stamped the way a
// scaffold-era `generate cli` stamped it.
func writeLegacyCLIFile(t *testing.T, cliDir, name, body string) {
	t.Helper()
	content, err := codegen.Stamp(name, body, codegen.Marker{Generator: "cli", Kind: codegen.KindScaffold, Version: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cliDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cliDir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCLIKeepsCustomGo(t *testing.T) {
	dir := cliTempModule(t)
	covT_capStdout(t, func() { runGenerateCLI([]string{"--binary=myapp"}) })
	custom := filepath.Join(dir, "cmd", "myapp", "custom.go")
	data, err := os.ReadFile(custom)
	if err != nil {
		t.Fatal(err)
	}
	if m, _, ok := codegen.ReadMarker(custom, string(data)); !ok || m.Kind != codegen.KindScaffold {
		t.Fatalf("custom.go marker = %+v (marked %v), want scaffold: it is the user's file", m, ok)
	}
	edited := append(data, "\n// mine\n"...)
	if err := os.WriteFile(custom, edited, 0o600); err != nil {
		t.Fatal(err)
	}
	covT_capStdout(t, func() { runGenerateCLI([]string{"--binary=myapp"}) })
	if got, _ := os.ReadFile(custom); string(got) != string(edited) {
		t.Fatal("a rebuild touched custom.go")
	}
}

func TestCLIRemovesDroppedEntity(t *testing.T) {
	dir := cliTempModule(t)
	cliDir := filepath.Join(dir, "cmd", "myapp")
	covT_capStdout(t, func() { runGenerateCLI([]string{"--binary=myapp"}) })
	if !fileExists(dir, "cmd/myapp/documents.gen.go") {
		t.Fatal("documents.gen.go not written")
	}
	mine := filepath.Join(cliDir, "extra.go")
	if err := os.WriteFile(mine, []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := covT_capStdout(t, func() { runGenerateCLI([]string{"--binary=myapp", "--only=posts"}) })
	if fileExists(dir, "cmd/myapp/documents.gen.go") {
		t.Fatalf("documents.gen.go survived --only=posts:\n%s", out)
	}
	if !fileExists(dir, "cmd/myapp/posts.gen.go") || !fileExists(dir, "cmd/myapp/extra.go") {
		t.Fatal("removed a file the run still produces, or one the user wrote")
	}
}

func TestCLIMigratesLegacyLayout(t *testing.T) {
	dir := cliTempModule(t)
	cliDir := filepath.Join(dir, "cmd", "myapp")
	writeLegacyCLIFile(t, cliDir, "main.go", "package main\n")
	writeLegacyCLIFile(t, cliDir, "posts.go", "package main\n")
	covT_capStdout(t, func() { runGenerateCLI([]string{"--binary=myapp"}) })
	if fileExists(dir, "cmd/myapp/main.go") || fileExists(dir, "cmd/myapp/posts.go") {
		t.Fatal("untouched scaffold-era files left beside their *.gen.go replacements")
	}
	if !fileExists(dir, "cmd/myapp/main.gen.go") {
		t.Fatal("main.gen.go not written")
	}
}

func TestCLIRefusesEditedLegacyFile(t *testing.T) {
	dir := cliTempModule(t)
	cliDir := filepath.Join(dir, "cmd", "myapp")
	writeLegacyCLIFile(t, cliDir, "auth.go", "package main\n")
	edited := filepath.Join(cliDir, "auth.go")
	data, _ := os.ReadFile(edited)
	if err := os.WriteFile(edited, append(data, "// my login tweak\n"...), 0o600); err != nil {
		t.Fatal(err)
	}
	var code int
	out := covT_capStdout(t, func() {
		code = covT_capExit(t, func() { runGenerateCLI([]string{"--binary=myapp"}) })
	})
	if code != 1 || !strings.Contains(out, "auth.go") || !strings.Contains(out, "custom.go") {
		t.Fatalf("want a refusal naming auth.go and custom.go, exit %d:\n%s", code, out)
	}
	if !fileExists(dir, "cmd/myapp/auth.go") || fileExists(dir, "cmd/myapp/main.gen.go") {
		t.Fatal("the refusal wrote or removed files")
	}
	out = covT_capStdout(t, func() { runGenerateCLI([]string{"--binary=myapp", "--force"}) })
	if fileExists(dir, "cmd/myapp/auth.go") || !strings.Contains(out, "auth.go") {
		t.Fatalf("--force must remove the edited file and name it:\n%s", out)
	}
}

func TestCLIUnmarkedLegacyNeedsForce(t *testing.T) {
	dir := cliTempModule(t)
	cliDir := filepath.Join(dir, "cmd", "myapp")
	if err := os.MkdirAll(cliDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cliDir, "verbs.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runGenerateCLI([]string{"--binary=myapp"}) })
	})
	if code != 1 || !fileExists(dir, "cmd/myapp/verbs.go") {
		t.Fatalf("an unmarked verbs.go beside verbs.gen.go must need --force: exit %d", code)
	}
}

func writeSettings(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "gofastr.settings.yml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSettingsCLIEntryBuildsCLI(t *testing.T) {
	dir := cliTempModule(t)
	writeSettings(t, dir, `codegen:
  generators:
    - name: cli
      output: cmd/shop
      config:
        binary: shop
        only: posts
`)
	out := covT_capStdout(t, func() { generateProject(nil) })
	if !fileExists(dir, "cmd/shop/posts.gen.go") || fileExists(dir, "cmd/shop/documents.gen.go") {
		t.Fatalf("cli entry did not drive the CLI:\n%s", out)
	}
	main, _ := os.ReadFile(filepath.Join(dir, "cmd", "shop", "main.gen.go"))
	if !strings.Contains(string(main), `binaryName = "shop"`) {
		t.Fatal("binary from config.binary not used")
	}
}

func TestSettingsRefusesBadCLIEntry(t *testing.T) {
	for name, body := range map[string]string{
		"client entry": "    - name: client\n",
		"url source":   "    - name: cli\n      config:\n        from_openapi: https://x.example/openapi.json\n",
		"padded url":   "    - name: cli\n      config:\n        from_openapi: \" https://x.example/openapi.json\"\n",
		"unknown key":  "    - name: cli\n      config:\n        bianry: shop\n",
		"source":       "    - name: cli\n      source:\n        type: json_file\n        path: x.json\n",
		"non-string":   "    - name: cli\n      config:\n        only: [posts]\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := cliTempModule(t)
			writeSettings(t, dir, "codegen:\n  generators:\n"+body)
			var code int
			out := covT_capStdout(t, func() { code = covT_capExit(t, func() { generateProject(nil) }) })
			if code != 1 || !strings.Contains(out, "codegen.generators[0]") {
				t.Fatalf("want a refusal naming the entry, exit %d:\n%s", code, out)
			}
		})
	}
}

func TestSettingsCLIRunIsQuietWhenCurrent(t *testing.T) {
	dir := cliTempModule(t)
	writeSettings(t, dir, "codegen:\n  generators:\n    - name: cli\n      config:\n        binary: shop\n")
	covT_capStdout(t, func() { generateProject(nil) })
	mainGo := filepath.Join(dir, "cmd", "shop", "main.gen.go")
	before, err := os.Stat(mainGo)
	if err != nil {
		t.Fatal(err)
	}
	out := covT_capStdout(t, func() { generateProject(nil) })
	if strings.Contains(out, "cmd/shop") || strings.Contains(out, "Next steps") {
		t.Fatalf("a current CLI printed output:\n%s", out)
	}
	if after, _ := os.Stat(mainGo); !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("rewrote an unchanged file")
	}
	if !fileExists(dir, "cmd/shop/posts.gen.go") {
		t.Fatal("an unchanged file was removed as stale")
	}
}

func TestSettingsCLIRefusesJSON(t *testing.T) {
	for _, args := range [][]string{{"--json"}, {"--dry-run", "--json"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			dir := cliTempModule(t)
			writeSettings(t, dir, "codegen:\n  generators:\n    - name: cli\n      config:\n        binary: shop\n")
			var code int
			out := covT_capStdout(t, func() { code = covT_capExit(t, func() { generateProject(args) }) })
			if code != 1 || !strings.HasPrefix(strings.TrimSpace(out), "{") {
				t.Fatalf("want a JSON refusal, exit %d:\n%s", code, out)
			}
			if fileExists(dir, "cmd/shop/main.gen.go") {
				t.Fatal("wrote the CLI anyway")
			}
		})
	}
}

func TestSettingsCLIEmptyAPIPrefix(t *testing.T) {
	dir := cliTempModule(t)
	writeSettings(t, dir, "codegen:\n  generators:\n    - name: cli\n      config:\n        binary: shop\n        api_prefix: \"\"\n")
	out := covT_capStdout(t, func() { generateProject(nil) })
	main, err := os.ReadFile(filepath.Join(dir, "cmd", "shop", "main.gen.go"))
	if err != nil {
		t.Fatalf("no CLI written:\n%s", out)
	}
	if !regexp.MustCompile(`apiPrefix\s+= ""`).Match(main) {
		t.Fatalf("api_prefix \"\" not baked in as an empty prefix:\n%s", main)
	}
}

func TestCLIRefusesSymlinkedClientDir(t *testing.T) {
	dir := cliTempModule(t)
	covT_capStdout(t, func() { runGenerateCLI([]string{"--binary=shop"}) })
	out := filepath.Join(dir, "cmd", "shop")
	if err := os.MkdirAll(filepath.Join(out, "internal"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("..", filepath.Join(out, "internal", "client")); err != nil {
		t.Fatal(err)
	}
	var code int
	msg := covT_capStdout(t, func() { code = covT_capExit(t, func() { runGenerateCLI([]string{"--binary=shop"}) }) })
	if code != 1 || !strings.Contains(msg, "symlink") {
		t.Fatalf("want a symlink refusal, exit %d:\n%s", code, msg)
	}
	if !fileExists(dir, "cmd/shop/custom.go") || !fileExists(dir, "cmd/shop/main.gen.go") {
		t.Fatal("a symlinked alias deleted real files")
	}
}

func TestWatchHashesCLIOpenAPIDoc(t *testing.T) {
	dir := cliTempModule(t)
	writeSettings(t, dir, "codegen:\n  generators:\n    - name: cli\n      config:\n        from_openapi: openapi.json\n")
	doc := filepath.Join(dir, "openapi.json")
	if err := os.WriteFile(doc, []byte(`{"a":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	before := hashGenerateInputs(generateOptions{})
	if err := os.WriteFile(doc, []byte(`{"a":2}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if hashGenerateInputs(generateOptions{}) == before {
		t.Fatal("editing the cli entry's OpenAPI document did not change the watch hash")
	}
}

func TestWatchHashFollowsEntitiesOnly(t *testing.T) {
	dir := cliTempModule(t)
	before := hashGenerateInputs(generateOptions{})
	client := filepath.Join(dir, "entities", "client", "client.go")
	data, err := os.ReadFile(client)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(client, append(data, "\n// rebuilt\n"...), 0o600); err != nil {
		t.Fatal(err)
	}
	if hashGenerateInputs(generateOptions{}) != before {
		t.Fatal("rewriting entities/client moved the watch hash: --watch would loop on its own output")
	}
	addPostsField(t, dir)
	if hashGenerateInputs(generateOptions{}) == before {
		t.Fatal("editing an entity did not move the watch hash")
	}
}
