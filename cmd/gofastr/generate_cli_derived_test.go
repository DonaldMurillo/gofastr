package main

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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
        only: [posts]
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
	writeSettings(t, dir, `codegen:
  generators:
    - name: cli
      config:
        binary: shop
        verbs:
          posts: [list, get]
          documents: ["*"]
`)
	covT_capStdout(t, func() { generateProject(nil) })
	posts := commandNames(t, dir, "cmd/shop/posts.gen.go")
	docs := commandNames(t, dir, "cmd/shop/documents.gen.go")
	if strings.Contains(posts, `"posts delete"`) || !strings.Contains(docs, `"documents delete"`) {
		t.Fatalf("verbs map not applied per entity:\nposts:\n%s", posts)
	}
}

func TestGenerateCLIUsesSettingsEntry(t *testing.T) {
	dir := cliTempModule(t)
	writeSettings(t, dir, "codegen:\n  generators:\n    - name: cli\n      output: tools/shop\n      config:\n        binary: shop\n        only: [posts]\n")
	out := covT_capStdout(t, func() { runGenerateCLI(nil) })
	if !fileExists(dir, "tools/shop/posts.gen.go") || fileExists(dir, "tools/shop/documents.gen.go") {
		t.Fatalf("explicit run ignored the entry:\n%s", out)
	}
	if strings.Contains(out, "- name: cli") {
		t.Fatalf("an entry-driven run printed an entry to paste:\n%s", out)
	}
	// A flag wins its key; the entry still names the binary and output.
	covT_capStdout(t, func() { runGenerateCLI([]string{"--only=documents"}) })
	if !fileExists(dir, "tools/shop/documents.gen.go") || fileExists(dir, "tools/shop/posts.gen.go") {
		t.Fatal("--only did not win over the entry's only")
	}
}

func TestGenerateCLIPicksEntryByOut(t *testing.T) {
	dir := cliTempModule(t)
	writeSettings(t, dir, "codegen:\n  generators:\n    - name: cli\n      config:\n        binary: shop\n        only: [posts]\n    - name: cli\n      config:\n        binary: docs\n        only: [documents]\n")
	var code int
	out := covT_capStdout(t, func() { code = covT_capExit(t, func() { runGenerateCLI(nil) }) })
	if code != 1 || !strings.Contains(out, "pass --out") {
		t.Fatalf("two entries and no --out must be refused, exit %d:\n%s", code, out)
	}
	covT_capStdout(t, func() { runGenerateCLI([]string{"--out=cmd/docs"}) })
	if !fileExists(dir, "cmd/docs/documents.gen.go") || fileExists(dir, "cmd/docs/posts.gen.go") {
		t.Fatal("--out=cmd/docs did not pick the docs entry")
	}
	covT_capStdout(t, func() { runGenerateCLI([]string{"--binary=shop"}) })
	if !fileExists(dir, "cmd/shop/posts.gen.go") || fileExists(dir, "cmd/shop/documents.gen.go") {
		t.Fatal("--binary=shop did not pick the shop entry")
	}
}

// The entry a run prints must rebuild the same CLI from settings.
func TestGenerateCLIPrintsItsEntry(t *testing.T) {
	dir := cliTempModule(t)
	out := covT_capStdout(t, func() {
		runGenerateCLI([]string{"--binary=shop", "--api-prefix=", "--exclude=documents", "--verbs=posts=list,get"})
	})
	_, after, ok := strings.Cut(out, "keeps the CLI current")
	if !ok {
		t.Fatalf("no entry to paste:\n%s", out)
	}
	_, entry, _ := strings.Cut(after, "\n\n")
	var lines []string
	for _, line := range strings.Split(entry, "\n") {
		lines = append(lines, strings.TrimPrefix(line, "      "))
	}
	writeSettings(t, dir, strings.Join(lines, "\n"))
	before := commandNames(t, dir, "cmd/shop/posts.gen.go")
	mainBefore := commandNames(t, dir, "cmd/shop/main.gen.go")
	if err := os.RemoveAll(filepath.Join(dir, "cmd")); err != nil {
		t.Fatal(err)
	}
	settingsOut := covT_capStdout(t, func() { generateProject(nil) })
	if got := commandNames(t, dir, "cmd/shop/posts.gen.go"); got != before {
		t.Fatalf("the printed entry built a different CLI:\n%s\n%s", strings.Join(lines, "\n"), settingsOut)
	}
	if got := commandNames(t, dir, "cmd/shop/main.gen.go"); got != mainBefore {
		t.Fatal("the printed entry lost the empty api_prefix")
	}
	if fileExists(dir, "cmd/shop/documents.gen.go") {
		t.Fatal("the printed entry lost --exclude")
	}
}

func TestGenerateSDKRefusesCommaString(t *testing.T) {
	dir := cliTempModule(t)
	writeSettings(t, dir, "codegen:\n  generators:\n    - name: sdk\n      config:\n        exclude: documents\n")
	var code int
	out := covT_capStdout(t, func() { code = covT_capExit(t, func() { runGenerateSDK(nil) }) })
	if code != 1 || !strings.Contains(out, "exclude: [documents]") {
		t.Fatalf("want the list spelling in the refusal, exit %d:\n%s", code, out)
	}
	if fileExists(dir, "gen/sdk") {
		t.Fatal("refused entry still wrote an SDK")
	}
}

func TestCLIEntryYAMLQuotesStar(t *testing.T) {
	got := cliEntryYAML(cliOptions{outDir: "cmd/shop", apiPrefix: "api", verbs: "posts=*;documents=list"}, "shop")
	if !strings.Contains(got, `posts: ["*"]`) || !strings.Contains(got, "documents: [list]") {
		t.Fatalf("a bare * is a YAML alias:\n%s", got)
	}
}

func TestGenerateCLIFlagPrefixWins(t *testing.T) {
	dir := cliTempModule(t)
	writeSettings(t, dir, "codegen:\n  generators:\n    - name: cli\n      config:\n        binary: shop\n        api_prefix: v1\n")
	covT_capStdout(t, func() { runGenerateCLI([]string{"--api-prefix=v2"}) })
	main := commandNames(t, dir, "cmd/shop/main.gen.go")
	if !regexp.MustCompile(`apiPrefix\s+= "/v2"`).MatchString(main) {
		t.Fatal("--api-prefix did not win over the entry's api_prefix")
	}
	covT_capStdout(t, func() { runGenerateCLI(nil) })
	if main := commandNames(t, dir, "cmd/shop/main.gen.go"); !regexp.MustCompile(`apiPrefix\s+= "/v1"`).MatchString(main) {
		t.Fatal("the entry's api_prefix was not used")
	}
}

func TestGenerateCLIRefusesBadEntry(t *testing.T) {
	dir := cliTempModule(t)
	writeSettings(t, dir, "codegen:\n  generators:\n    - name: cli\n      config:\n        only: posts\n")
	var code int
	out := covT_capStdout(t, func() { code = covT_capExit(t, func() { runGenerateCLI(nil) }) })
	if code != 1 || !strings.Contains(out, "only: [posts]") || fileExists(dir, "cmd") {
		t.Fatalf("want a refusal before any write, exit %d:\n%s", code, out)
	}
}

// A settings file in a parent directory names paths from there; a run in
// a subdirectory must not apply it.
func TestParentSettingsNotApplied(t *testing.T) {
	dir := cliTempModule(t)
	writeSettings(t, dir, "codegen:\n  generators:\n    - name: cli\n      config:\n        binary: shop\n    - name: sdk\n      output: client-sdk\n")
	sub := filepath.Join(dir, "tools")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	cli := cliOptions{apiPrefix: "api"}
	if used, err := applyCLIConfigDefaults(&cli); used || err != nil || cli.binary != "" {
		t.Fatalf("cli took a parent entry: used=%v err=%v binary=%q", used, err, cli.binary)
	}
	var sdk sdkOptions
	if err := applySDKConfigDefaults(&sdk); err != nil || sdk.outDir != "" {
		t.Fatalf("sdk took a parent entry: err=%v outDir=%q", err, sdk.outDir)
	}
}

func TestSettingsCLIFromOpenAPI(t *testing.T) {
	dir := oaTempModule(t)
	writeSettings(t, dir, "codegen:\n  generators:\n    - name: cli\n      config:\n        binary: oapp\n        from_openapi: openapi.json\n")
	out := covT_capStdout(t, func() { generateProject(nil) })
	if !fileExists(dir, "cmd/oapp/operations.gen.go") {
		t.Fatalf("from_openapi entry did not build the CLI:\n%s", out)
	}
}

// An entry with no binary or output is matched by the defaults a run uses.
func TestGenerateCLIMatchesDefaultedEntry(t *testing.T) {
	dir := cliTempModule(t)
	writeSettings(t, dir, "codegen:\n  generators:\n    - name: cli\n      config:\n        only: [posts]\n")
	binary := strings.ToLower(filepath.Base(dir))
	for _, args := range [][]string{{"--out=cmd/" + binary}, {"--binary=" + binary}} {
		if err := os.RemoveAll(filepath.Join(dir, "cmd")); err != nil {
			t.Fatal(err)
		}
		covT_capStdout(t, func() { runGenerateCLI(args) })
		if fileExists(dir, "cmd/"+binary+"/documents.gen.go") {
			t.Fatalf("%v missed the entry that writes there", args)
		}
	}
}

func TestGenerateCLIEmptyFlagClearsEntry(t *testing.T) {
	dir := cliTempModule(t)
	writeSettings(t, dir, "codegen:\n  generators:\n    - name: cli\n      config:\n        binary: shop\n        only: [posts]\n")
	covT_capStdout(t, func() { runGenerateCLI([]string{"--only="}) })
	if !fileExists(dir, "cmd/shop/documents.gen.go") {
		t.Fatal("--only= did not clear the entry's only")
	}
}

// The printed entry must read back, through the settings parser, as the
// options that printed it.
func TestCLIEntryYAMLRoundTrips(t *testing.T) {
	for name, opts := range map[string]cliOptions{
		"bool binary":    {binary: "true", outDir: "cmd/true", apiPrefix: "api"},
		"number binary":  {binary: "7", outDir: "cmd/7", apiPrefix: "1.5"},
		"empty prefix":   {binary: "shop", outDir: "cmd/shop", apiPrefix: ""},
		"repeated verbs": {binary: "shop", outDir: "cmd/shop", apiPrefix: "api", verbs: "posts=list;posts=get;documents=*"},
		"global verbs":   {binary: "shop", outDir: "cmd/shop", apiPrefix: "api", verbs: "list,get", only: []string{"posts"}},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeSettings(t, dir, cliEntryYAML(opts, opts.binary))
			discovery, err := codegen.DiscoverConfig(dir)
			if err != nil {
				t.Fatal(err)
			}
			got, err := decodeCLIEntry(0, discovery.Config.Codegen.Generators[0])
			if err != nil {
				t.Fatalf("%v\n%s", err, cliEntryYAML(opts, opts.binary))
			}
			wantGlobal, wantPer, _ := parseVerbSelection(opts.verbs)
			gotGlobal, gotPer, _ := parseVerbSelection(got.verbs)
			if got.binary != opts.binary || got.outDir != opts.outDir || got.apiPrefix != opts.apiPrefix ||
				!slices.Equal(got.only, opts.only) || !slices.Equal(gotGlobal, wantGlobal) || !maps.EqualFunc(gotPer, wantPer, slices.Equal) {
				t.Fatalf("read back %+v from:\n%s", got, cliEntryYAML(opts, opts.binary))
			}
		})
	}
}

// styles leaves the list before the sdk entry is decoded; the error must
// still name the sdk entry's own index.
func TestSettingsErrorKeepsEntryIndex(t *testing.T) {
	dir := cliTempModule(t)
	writeSettings(t, dir, "codegen:\n  generators:\n    - name: styles\n    - name: sdk\n      config:\n        only: posts\n")
	var code int
	out := covT_capStdout(t, func() { code = covT_capExit(t, func() { generateProject(nil) }) })
	if code != 1 || !strings.Contains(out, "codegen.generators[1] (sdk)") {
		t.Fatalf("want the sdk entry's index 1, exit %d:\n%s", code, out)
	}
}
