package generatecmd

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
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
	clitest.CovT_capStdout(t, func() { appgen.RunGenerateCLI([]string{"--binary=myapp"}) })
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
	clitest.CovT_capStdout(t, func() { appgen.RunGenerateCLI([]string{"--binary=myapp"}) })
	if got, _ := os.ReadFile(custom); string(got) != string(edited) {
		t.Fatal("a rebuild touched custom.go")
	}
}

func TestCLIRemovesDroppedEntity(t *testing.T) {
	dir := cliTempModule(t)
	cliDir := filepath.Join(dir, "cmd", "myapp")
	clitest.CovT_capStdout(t, func() { appgen.RunGenerateCLI([]string{"--binary=myapp"}) })
	if !clitest.FileExists(dir, "cmd/myapp/documents.gen.go") {
		t.Fatal("documents.gen.go not written")
	}
	mine := filepath.Join(cliDir, "extra.go")
	if err := os.WriteFile(mine, []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := clitest.CovT_capStdout(t, func() { appgen.RunGenerateCLI([]string{"--binary=myapp", "--only=posts"}) })
	if clitest.FileExists(dir, "cmd/myapp/documents.gen.go") {
		t.Fatalf("documents.gen.go survived --only=posts:\n%s", out)
	}
	if !clitest.FileExists(dir, "cmd/myapp/posts.gen.go") || !clitest.FileExists(dir, "cmd/myapp/extra.go") {
		t.Fatal("removed a file the run still produces, or one the user wrote")
	}
}

func TestCLIMigratesLegacyLayout(t *testing.T) {
	dir := cliTempModule(t)
	cliDir := filepath.Join(dir, "cmd", "myapp")
	writeLegacyCLIFile(t, cliDir, "main.go", "package main\n")
	writeLegacyCLIFile(t, cliDir, "posts.go", "package main\n")
	clitest.CovT_capStdout(t, func() { appgen.RunGenerateCLI([]string{"--binary=myapp"}) })
	if clitest.FileExists(dir, "cmd/myapp/main.go") || clitest.FileExists(dir, "cmd/myapp/posts.go") {
		t.Fatal("untouched scaffold-era files left beside their *.gen.go replacements")
	}
	if !clitest.FileExists(dir, "cmd/myapp/main.gen.go") {
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
	out := clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() { appgen.RunGenerateCLI([]string{"--binary=myapp"}) })
	})
	if code != 1 || !strings.Contains(out, "auth.go") || !strings.Contains(out, "custom.go") {
		t.Fatalf("want a refusal naming auth.go and custom.go, exit %d:\n%s", code, out)
	}
	if !clitest.FileExists(dir, "cmd/myapp/auth.go") || clitest.FileExists(dir, "cmd/myapp/main.gen.go") {
		t.Fatal("the refusal wrote or removed files")
	}
	out = clitest.CovT_capStdout(t, func() { appgen.RunGenerateCLI([]string{"--binary=myapp", "--force"}) })
	if clitest.FileExists(dir, "cmd/myapp/auth.go") || !strings.Contains(out, "auth.go") {
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
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { appgen.RunGenerateCLI([]string{"--binary=myapp"}) })
	})
	if code != 1 || !clitest.FileExists(dir, "cmd/myapp/verbs.go") {
		t.Fatalf("an unmarked verbs.go beside verbs.gen.go must need --force: exit %d", code)
	}
}

func TestSettingsCLIEntryBuildsCLI(t *testing.T) {
	dir := cliTempModule(t)
	clitest.WriteSettings(t, dir, `codegen:
  generators:
    - name: cli
      output: cmd/shop
      config:
        binary: shop
        only: [posts]
`)
	out := clitest.CovT_capStdout(t, func() { appgen.GenerateProject(nil) })
	if !clitest.FileExists(dir, "cmd/shop/posts.gen.go") || clitest.FileExists(dir, "cmd/shop/documents.gen.go") {
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
		"comma string": "    - name: cli\n      config:\n        only: posts,documents\n",
		"non-string":   "    - name: cli\n      config:\n        binary: [shop]\n",
		"verbs string": "    - name: cli\n      config:\n        verbs: list,get\n",
		"verbs key":    "    - name: cli\n      config:\n        verbs:\n          posts;x: [list]\n",
		"openapi only": "    - name: cli\n      config:\n        from_openapi: openapi.json\n        only: [posts]\n",
		"sdk name":     "    - name: sdk\n      config:\n        name: [x]\n",
		"sdk target":   "    - name: sdk\n      config:\n        targets: [go, 5]\n",
		"sdk key":      "    - name: sdk\n      config:\n        modul: x\n",
		"verb pair":    "    - name: cli\n      config:\n        verbs: [\"posts=list\"]\n",
		"bad verb":     "    - name: cli\n      config:\n        verbs:\n          posts: [list, frob]\n",
		"sdk comma":    "    - name: sdk\n      config:\n        only: posts\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := cliTempModule(t)
			clitest.WriteSettings(t, dir, "codegen:\n  generators:\n"+body)
			var code int
			out := clitest.CovT_capStdout(t, func() { code = clitest.CovT_capExit(t, func() { appgen.GenerateProject(nil) }) })
			if code != 1 || !strings.Contains(out, "codegen.generators[0]") {
				t.Fatalf("want a refusal naming the entry, exit %d:\n%s", code, out)
			}
		})
	}
}

func TestSettingsCLIRunIsQuietWhenCurrent(t *testing.T) {
	dir := cliTempModule(t)
	clitest.WriteSettings(t, dir, "codegen:\n  generators:\n    - name: cli\n      config:\n        binary: shop\n")
	clitest.CovT_capStdout(t, func() { appgen.GenerateProject(nil) })
	mainGo := filepath.Join(dir, "cmd", "shop", "main.gen.go")
	before, err := os.Stat(mainGo)
	if err != nil {
		t.Fatal(err)
	}
	out := clitest.CovT_capStdout(t, func() { appgen.GenerateProject(nil) })
	if strings.Contains(out, "cmd/shop") || strings.Contains(out, "Next steps") {
		t.Fatalf("a current CLI printed output:\n%s", out)
	}
	if after, _ := os.Stat(mainGo); !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("rewrote an unchanged file")
	}
	if !clitest.FileExists(dir, "cmd/shop/posts.gen.go") {
		t.Fatal("an unchanged file was removed as stale")
	}
}

func TestSettingsCLIRefusesJSON(t *testing.T) {
	for _, args := range [][]string{{"--json"}, {"--dry-run", "--json"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			dir := cliTempModule(t)
			clitest.WriteSettings(t, dir, "codegen:\n  generators:\n    - name: cli\n      config:\n        binary: shop\n")
			var code int
			out := clitest.CovT_capStdout(t, func() { code = clitest.CovT_capExit(t, func() { appgen.GenerateProject(args) }) })
			if code != 1 || !strings.HasPrefix(strings.TrimSpace(out), "{") {
				t.Fatalf("want a JSON refusal, exit %d:\n%s", code, out)
			}
			if clitest.FileExists(dir, "cmd/shop/main.gen.go") {
				t.Fatal("wrote the CLI anyway")
			}
		})
	}
}

func TestSettingsCLIEmptyAPIPrefix(t *testing.T) {
	dir := cliTempModule(t)
	clitest.WriteSettings(t, dir, "codegen:\n  generators:\n    - name: cli\n      config:\n        binary: shop\n        api_prefix: \"\"\n")
	out := clitest.CovT_capStdout(t, func() { appgen.GenerateProject(nil) })
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
	clitest.CovT_capStdout(t, func() { appgen.RunGenerateCLI([]string{"--binary=shop"}) })
	out := filepath.Join(dir, "cmd", "shop")
	if err := os.MkdirAll(filepath.Join(out, "internal"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("..", filepath.Join(out, "internal", "client")); err != nil {
		t.Fatal(err)
	}
	var code int
	msg := clitest.CovT_capStdout(t, func() { code = clitest.CovT_capExit(t, func() { appgen.RunGenerateCLI([]string{"--binary=shop"}) }) })
	if code != 1 || !strings.Contains(msg, "symlink") {
		t.Fatalf("want a symlink refusal, exit %d:\n%s", code, msg)
	}
	if !clitest.FileExists(dir, "cmd/shop/custom.go") || !clitest.FileExists(dir, "cmd/shop/main.gen.go") {
		t.Fatal("a symlinked alias deleted real files")
	}
}

func TestWatchHashesCLIOpenAPIDoc(t *testing.T) {
	dir := cliTempModule(t)
	clitest.WriteSettings(t, dir, "codegen:\n  generators:\n    - name: cli\n      config:\n        from_openapi: openapi.json\n")
	doc := filepath.Join(dir, "openapi.json")
	if err := os.WriteFile(doc, []byte(`{"a":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	before := hashGenerateInputs(appgen.GenerateOptions{})
	if err := os.WriteFile(doc, []byte(`{"a":2}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if hashGenerateInputs(appgen.GenerateOptions{}) == before {
		t.Fatal("editing the cli entry's OpenAPI document did not change the watch hash")
	}
}

func TestWatchHashFollowsEntitiesOnly(t *testing.T) {
	dir := cliTempModule(t)
	before := hashGenerateInputs(appgen.GenerateOptions{})
	client := filepath.Join(dir, "entities", "client", "client.go")
	data, err := os.ReadFile(client)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(client, append(data, "\n// rebuilt\n"...), 0o600); err != nil {
		t.Fatal(err)
	}
	if hashGenerateInputs(appgen.GenerateOptions{}) != before {
		t.Fatal("rewriting entities/client moved the watch hash: --watch would loop on its own output")
	}
	addPostsField(t, dir)
	if hashGenerateInputs(appgen.GenerateOptions{}) == before {
		t.Fatal("editing an entity did not move the watch hash")
	}
}

// commandNames reads the verbs an <entity>.gen.go command table offers.
func commandNames(t *testing.T, dir, file string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(dir, file))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestSettingsCLIVerbsMap(t *testing.T) {
	dir := cliTempModule(t)
	clitest.WriteSettings(t, dir, `codegen:
  generators:
    - name: cli
      config:
        binary: shop
        verbs:
          posts: [list, get]
          documents: ["*"]
`)
	clitest.CovT_capStdout(t, func() { appgen.GenerateProject(nil) })
	posts := commandNames(t, dir, "cmd/shop/posts.gen.go")
	docs := commandNames(t, dir, "cmd/shop/documents.gen.go")
	if strings.Contains(posts, `"posts delete"`) || !strings.Contains(docs, `"documents delete"`) {
		t.Fatalf("verbs map not applied per entity:\nposts:\n%s", posts)
	}
}

func TestGenerateCLIUsesSettingsEntry(t *testing.T) {
	dir := cliTempModule(t)
	clitest.WriteSettings(t, dir, "codegen:\n  generators:\n    - name: cli\n      output: tools/shop\n      config:\n        binary: shop\n        only: [posts]\n")
	out := clitest.CovT_capStdout(t, func() { appgen.RunGenerateCLI(nil) })
	if !clitest.FileExists(dir, "tools/shop/posts.gen.go") || clitest.FileExists(dir, "tools/shop/documents.gen.go") {
		t.Fatalf("explicit run ignored the entry:\n%s", out)
	}
	if strings.Contains(out, "- name: cli") {
		t.Fatalf("an entry-driven run printed an entry to paste:\n%s", out)
	}
	// A flag wins its key; the entry still names the binary and output.
	clitest.CovT_capStdout(t, func() { appgen.RunGenerateCLI([]string{"--only=documents"}) })
	if !clitest.FileExists(dir, "tools/shop/documents.gen.go") || clitest.FileExists(dir, "tools/shop/posts.gen.go") {
		t.Fatal("--only did not win over the entry's only")
	}
}

func TestGenerateCLIPicksEntryByOut(t *testing.T) {
	dir := cliTempModule(t)
	clitest.WriteSettings(t, dir, "codegen:\n  generators:\n    - name: cli\n      config:\n        binary: shop\n        only: [posts]\n    - name: cli\n      config:\n        binary: docs\n        only: [documents]\n")
	var code int
	out := clitest.CovT_capStdout(t, func() { code = clitest.CovT_capExit(t, func() { appgen.RunGenerateCLI(nil) }) })
	if code != 1 || !strings.Contains(out, "pass --out or --binary") {
		t.Fatalf("two entries and no --out must be refused, exit %d:\n%s", code, out)
	}
	clitest.CovT_capStdout(t, func() { appgen.RunGenerateCLI([]string{"--out=cmd/docs"}) })
	if !clitest.FileExists(dir, "cmd/docs/documents.gen.go") || clitest.FileExists(dir, "cmd/docs/posts.gen.go") {
		t.Fatal("--out=cmd/docs did not pick the docs entry")
	}
	clitest.CovT_capStdout(t, func() { appgen.RunGenerateCLI([]string{"--binary=shop"}) })
	if !clitest.FileExists(dir, "cmd/shop/posts.gen.go") || clitest.FileExists(dir, "cmd/shop/documents.gen.go") {
		t.Fatal("--binary=shop did not pick the shop entry")
	}
}

// The entry a run prints must rebuild the same CLI from settings.
func TestGenerateCLIPrintsItsEntry(t *testing.T) {
	dir := cliTempModule(t)
	out := clitest.CovT_capStdout(t, func() {
		appgen.RunGenerateCLI([]string{"--binary=shop", "--api-prefix=", "--exclude=documents", "--verbs=posts=list,get"})
	})
	_, after, ok := strings.Cut(out, "keeps the CLI current")
	if !ok || !strings.Contains(out, "codegen.generators") {
		t.Fatalf("no entry to paste:\n%s", out)
	}
	_, entry, _ := strings.Cut(after, "\n\n")
	var lines []string
	for _, line := range strings.Split(entry, "\n") {
		lines = append(lines, strings.TrimPrefix(line, "      "))
	}
	clitest.WriteSettings(t, dir, strings.Join(lines, "\n"))
	before := commandNames(t, dir, "cmd/shop/posts.gen.go")
	mainBefore := commandNames(t, dir, "cmd/shop/main.gen.go")
	if err := os.RemoveAll(filepath.Join(dir, "cmd")); err != nil {
		t.Fatal(err)
	}
	settingsOut := clitest.CovT_capStdout(t, func() { appgen.GenerateProject(nil) })
	if got := commandNames(t, dir, "cmd/shop/posts.gen.go"); got != before {
		t.Fatalf("the printed entry built a different CLI:\n%s\n%s", strings.Join(lines, "\n"), settingsOut)
	}
	if got := commandNames(t, dir, "cmd/shop/main.gen.go"); got != mainBefore {
		t.Fatal("the printed entry lost the empty api_prefix")
	}
	if clitest.FileExists(dir, "cmd/shop/documents.gen.go") {
		t.Fatal("the printed entry lost --exclude")
	}
}

func TestGenerateSDKRefusesCommaString(t *testing.T) {
	dir := cliTempModule(t)
	clitest.WriteSettings(t, dir, "codegen:\n  generators:\n    - name: sdk\n      config:\n        exclude: documents\n")
	var code int
	out := clitest.CovT_capStdout(t, func() { code = clitest.CovT_capExit(t, func() { appgen.RunGenerateSDK(nil) }) })
	if code != 1 || !strings.Contains(out, "exclude: [documents]") {
		t.Fatalf("want the list spelling in the refusal, exit %d:\n%s", code, out)
	}
	if clitest.FileExists(dir, "gen/sdk") {
		t.Fatal("refused entry still wrote an SDK")
	}
}

func TestGenerateCLIFlagPrefixWins(t *testing.T) {
	dir := cliTempModule(t)
	clitest.WriteSettings(t, dir, "codegen:\n  generators:\n    - name: cli\n      config:\n        binary: shop\n        api_prefix: v1\n")
	clitest.CovT_capStdout(t, func() { appgen.RunGenerateCLI([]string{"--api-prefix=v2"}) })
	main := commandNames(t, dir, "cmd/shop/main.gen.go")
	if !regexp.MustCompile(`apiPrefix\s+= "/v2"`).MatchString(main) {
		t.Fatal("--api-prefix did not win over the entry's api_prefix")
	}
	clitest.CovT_capStdout(t, func() { appgen.RunGenerateCLI(nil) })
	if main := commandNames(t, dir, "cmd/shop/main.gen.go"); !regexp.MustCompile(`apiPrefix\s+= "/v1"`).MatchString(main) {
		t.Fatal("the entry's api_prefix was not used")
	}
}

func TestGenerateCLIRefusesBadEntry(t *testing.T) {
	dir := cliTempModule(t)
	clitest.WriteSettings(t, dir, "codegen:\n  generators:\n    - name: cli\n      config:\n        only: posts\n")
	var code int
	out := clitest.CovT_capStdout(t, func() { code = clitest.CovT_capExit(t, func() { appgen.RunGenerateCLI(nil) }) })
	if code != 1 || !strings.Contains(out, "only: [posts]") || clitest.FileExists(dir, "cmd") {
		t.Fatalf("want a refusal before any write, exit %d:\n%s", code, out)
	}
}

// A settings file in a parent directory names paths from there; a run in
// a subdirectory must not apply it.
func TestParentSettingsNotApplied(t *testing.T) {
	dir := cliTempModule(t)
	clitest.WriteSettings(t, dir, "codegen:\n  generators:\n    - name: cli\n      config:\n        binary: shop\n    - name: sdk\n      output: client-sdk\n")
	sub := filepath.Join(dir, "tools")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	cli := appgen.CliOptions{ApiPrefix: "api"}
	if used, err := appgen.ApplyCLIConfigDefaults(&cli); used || err != nil || cli.Binary != "" {
		t.Fatalf("cli took a parent entry: used=%v err=%v binary=%q", used, err, cli.Binary)
	}
	var sdk appgen.SdkOptions
	if err := appgen.ApplySDKConfigDefaults(&sdk); err != nil || sdk.OutDir != "" {
		t.Fatalf("sdk took a parent entry: err=%v outDir=%q", err, sdk.OutDir)
	}
}

// An entry with no binary or output is matched by the defaults a run uses.
func TestGenerateCLIMatchesDefaultedEntry(t *testing.T) {
	dir := cliTempModule(t)
	clitest.WriteSettings(t, dir, "codegen:\n  generators:\n    - name: cli\n      config:\n        only: [posts]\n")
	binary := strings.ToLower(filepath.Base(dir))
	for _, args := range [][]string{{"--out=cmd/" + binary}, {"--binary=" + binary}} {
		if err := os.RemoveAll(filepath.Join(dir, "cmd")); err != nil {
			t.Fatal(err)
		}
		clitest.CovT_capStdout(t, func() { appgen.RunGenerateCLI(args) })
		if clitest.FileExists(dir, "cmd/"+binary+"/documents.gen.go") {
			t.Fatalf("%v missed the entry that writes there", args)
		}
	}
}

func TestGenerateCLIEmptyFlagClearsEntry(t *testing.T) {
	dir := cliTempModule(t)
	clitest.WriteSettings(t, dir, "codegen:\n  generators:\n    - name: cli\n      config:\n        binary: shop\n        only: [posts]\n")
	clitest.CovT_capStdout(t, func() { appgen.RunGenerateCLI([]string{"--only="}) })
	if !clitest.FileExists(dir, "cmd/shop/documents.gen.go") {
		t.Fatal("--only= did not clear the entry's only")
	}
}

// styles leaves the list before the sdk entry is decoded; the error must
// still name the sdk entry's own index.
func TestSettingsErrorKeepsEntryIndex(t *testing.T) {
	dir := cliTempModule(t)
	clitest.WriteSettings(t, dir, "codegen:\n  generators:\n    - name: styles\n    - name: sdk\n      config:\n        only: posts\n")
	var code int
	out := clitest.CovT_capStdout(t, func() { code = clitest.CovT_capExit(t, func() { appgen.GenerateProject(nil) }) })
	if code != 1 || !strings.Contains(out, "codegen.generators[1] (sdk)") {
		t.Fatalf("want the sdk entry's index 1, exit %d:\n%s", code, out)
	}
}

// A refusal quotes the value it read, so a control byte in the settings
// file reaches the terminal escaped.
func TestListRefusalEscapesValue(t *testing.T) {
	for name, body := range map[string]string{
		"only":  "        only: \"posts\\x1b[2J\"\n",
		"verbs": "        verbs:\n          posts: \"list\\x1b[2J\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := cliTempModule(t)
			clitest.WriteSettings(t, dir, "codegen:\n  generators:\n    - name: cli\n      config:\n"+body)
			var code int
			out := clitest.CovT_capStdout(t, func() { code = clitest.CovT_capExit(t, func() { appgen.GenerateProject(nil) }) })
			if code != 1 || strings.Contains(out, "\x1b[2J") || !strings.Contains(out, `\x1b[2J`) {
				t.Fatalf("want the value escaped, exit %d:\n%q", code, out)
			}
		})
	}
}

// A selection typo names where the value came from: the entry when the
// settings file set it, the flag when the command line did.
func TestSelectionTypoNamesItsSource(t *testing.T) {
	dir := cliTempModule(t)
	clitest.WriteSettings(t, dir, "codegen:\n  generators:\n    - name: cli\n      config:\n        binary: shop\n        only: [postz]\n")
	for _, tc := range []struct {
		run  func()
		want string
	}{
		{func() { appgen.GenerateProject(nil) }, "codegen.generators[0] (cli): config.only"},
		{func() { appgen.RunGenerateCLI(nil) }, "codegen.generators[0] (cli): config.only"},
		{func() { appgen.RunGenerateCLI([]string{"--only=postz"}) }, "--only: no entity"},
	} {
		var code int
		out := clitest.CovT_capStdout(t, func() { code = clitest.CovT_capExit(t, tc.run) })
		if code != 1 || !strings.Contains(out, tc.want) {
			t.Errorf("want %q, exit %d:\n%s", tc.want, code, out)
		}
	}
}
