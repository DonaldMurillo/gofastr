package packcmd

import (
	"fmt"
	"go/parser"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/framework"
)

func cleanupPackSpec() appspec.AppSpec {
	return appspec.AppSpec{
		App: appspec.AppSpecApp{Name: "Pack probe", Module: "example.com/packprobe", DBDriver: "sqlite", DBURL: "file:probe.db"},
		Entities: []framework.EntityDeclaration{{
			Name:   "notes",
			Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}},
		}},
	}
}

func TestPackRecoversScreenTypes(t *testing.T) {
	bp := cleanupPackSpec()
	kinds := []string{"drawer", "sheet", "dialog", "page"}
	for _, kind := range kinds {
		bp.Screens = append(bp.Screens, appspec.AppSpecScreen{
			Name: kind, Route: "/" + kind, Type: kind,
			Body: []appspec.AppSpecBlock{{Type: "paragraph", Text: "screen body"}},
		})
	}
	dir := materializeAppSpec(t, bp)
	check := func(t *testing.T, got []appspec.AppSpecScreen, err error) {
		t.Helper()
		if err != nil || len(got) != len(kinds) {
			t.Fatalf("recovered screens = %v, error = %v", got, err)
		}
		for i, kind := range kinds {
			actual, err := appspec.ScreenTypeConst(got[i].Type)
			want, _ := appspec.ScreenTypeConst(kind)
			if err != nil || actual != want {
				t.Errorf("%s screen recovered with type %q, want %q", got[i].Name, got[i].Type, kind)
			}
		}
	}
	t.Run("per-screen", func(t *testing.T) {
		screens, err := packReadScreens(dir)
		check(t, screens, err)
	})
	t.Run("aggregated", func(t *testing.T) {
		legacy := t.TempDir()
		var register, screens strings.Builder
		register.WriteString("package main\nfunc RegisterGenerated() {\n")
		screens.WriteString("package main\n")
		for _, kind := range kinds {
			screen := appspec.ToCamelCase(kind) + "Screen"
			constant, err := appspec.ScreenTypeConst(kind)
			if err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(&screens, "func (%s) ScreenType() app.ScreenType { return %s }\n", screen, constant)
			fmt.Fprintf(&register, "site.Register(%q, &%s{}, layout)\n", "/"+kind, screen)
		}
		register.WriteString("}\n")
		for name, src := range map[string]string{"app.go": register.String(), "screens.go": screens.String()} {
			writeCleanupFile(t, filepath.Join(legacy, name), src)
		}
		got, err := packReadScreens(legacy)
		check(t, got, err)
	})
}

func TestPackAuthFormUsesActionPath(t *testing.T) {
	bp := cleanupPackSpec()
	bp.App.Auth = appspec.AppSpecAuth{Enabled: true, DevMode: true, BasePath: "/register-area"}
	bp.Screens = []appspec.AppSpecScreen{{
		Name: "sign_in", Route: "/sign-in",
		Body: []appspec.AppSpecBlock{{Kind: "login_form", Props: map[string]any{
			"action": "/register-area/login?next=/register", "register_href": "/sign-up",
		}}},
	}}
	got, err := packAppSpec(materializeAppSpec(t, bp))
	if err != nil || len(got.Screens) != 1 || len(got.Screens[0].Body) != 1 {
		t.Fatalf("pack screens = %v, error = %v", got.Screens, err)
	}
	block := got.Screens[0].Body[0]
	if block.Kind != "login_form" || block.Props["register_href"] != "/sign-up" {
		t.Fatalf("packed login block = %#v", block)
	}
	if err := appspec.ValidateAppSpec(got); err != nil {
		t.Fatalf("validate packed app spec: %v", err)
	}
}

func TestPackWritesDecodableNumericStrings(t *testing.T) {
	bp := cleanupPackSpec()
	bp.Seed = []appspec.AppSpecSeedEntity{{Entity: "notes", Rows: []map[string]any{{"title": "1e999"}}}}
	dir := materializeAppSpec(t, bp)
	exit := recordPackExit(t)
	out := clitest.CaptureStdout(t, func() { RunPack([]string{dir}) })
	if *exit != 0 {
		t.Fatalf("pack exited %d", *exit)
	}
	got, err := decodeAppSpecString(out)
	if err != nil {
		t.Fatalf("decode packed YAML: %v", err)
	}
	if len(got.Seed) != 1 || len(got.Seed[0].Rows) != 1 || got.Seed[0].Rows[0]["title"] != "1e999" {
		t.Fatalf("packed seed = %#v", got.Seed)
	}
}

func TestPackRefusesYAMLThatCannotDecode(t *testing.T) {
	dir := materializeAppSpec(t, cleanupPackSpec())
	writeCleanupFile(t, filepath.Join(dir, "stubs.go"), `package main

func seedData() []seedEntity {
	return []seedEntity{{Rows: []map[string]any{{"title": "a row without an entity"}}}}
}
`)
	out := filepath.Join(t.TempDir(), "snapshot.yml")
	exit := recordPackExit(t)
	RunPack([]string{dir, "-o", out})
	if *exit != 1 {
		t.Errorf("pack with an undecodable seed exited %d, want 1", *exit)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("pack wrote undecodable YAML, stat error = %v", err)
	}
}

func TestPackEntityRecoverySkipsTestFiles(t *testing.T) {
	dir := materializeAppSpec(t, cleanupPackSpec())
	writeCleanupFile(t, filepath.Join(dir, "entities", "broken_test.go"), "no valid Go source")
	got, err := appgen.PackReadEntities(dir)
	if err != nil || len(got) != 1 || got[0].Name != "notes" {
		t.Fatalf("packed entities = %v, error = %v", got, err)
	}
}

func TestPackRecoverySkipsSymlinkedGo(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	bp := cleanupPackSpec()
	bp.App.LLMMD = true
	bp.Screens = []appspec.AppSpecScreen{{
		Name: "notes", Route: "/notes",
		Body: []appspec.AppSpecBlock{{Type: "paragraph", Text: "linked screen"}},
	}}
	bp.Seed = []appspec.AppSpecSeedEntity{{Entity: "notes", Rows: []map[string]any{{"title": "linked seed"}}}}
	for _, leaf := range []string{"stubs.go", "app.go", "main.go", "entities/notes.go", "screen_notes.go"} {
		t.Run(leaf, func(t *testing.T) {
			dir := materializeAppSpec(t, bp)
			path := filepath.Join(dir, leaf)
			target := path + ".txt"
			if err := os.Rename(path, target); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Base(target), path); err != nil {
				t.Fatal(err)
			}
			got, err := packAppSpec(dir)
			if leaf == "app.go" {
				if err == nil || !strings.Contains(err.Error(), leaf) || !strings.Contains(err.Error(), "regular") {
					t.Fatalf("required symlink %s must be reported, got %v", leaf, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			switch leaf {
			case "main.go":
				if got.App.LLMMD {
					t.Error("packed LLM setting came from symlinked main.go")
				}
			case "stubs.go":
				if len(got.Seed) != 0 {
					t.Errorf("seed came from symlinked stubs.go: %v", got.Seed)
				}
			case "entities/notes.go":
				if len(got.Entities) != 0 {
					t.Errorf("entity came from symlinked source: %v", got.Entities)
				}
			case "screen_notes.go":
				if len(got.Screens) != 0 {
					t.Errorf("screen came from symlinked source: %v", got.Screens)
				}
			}
			check, err := checkPack(dir, got)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.ContainsFunc(check.Unread, func(note packFileNote) bool {
				return note.Path == leaf && strings.Contains(note.Note, "a symlink")
			}) {
				t.Errorf("check did not report skipping %s: %v", leaf, check.Unread)
			}
		})
	}
}

func TestPackRejectsExtraAppDirectory(t *testing.T) {
	dir := materializeAppSpec(t, cleanupPackSpec())
	out := filepath.Join(t.TempDir(), "snapshot.yml")
	exit := recordPackExit(t)
	RunPack([]string{t.TempDir(), "-o", out, dir})
	if *exit != 2 {
		t.Errorf("pack with two app directories exited %d, want 2", *exit)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("pack with two app directories wrote output, stat error = %v", err)
	}
}

func TestPackFlagUsageErrorsExit2(t *testing.T) {
	for _, arg := range []string{"-zzz", "-o", "--out="} {
		t.Run(arg, func(t *testing.T) {
			exit := recordPackExit(t)
			RunPack([]string{arg})
			if *exit != 2 {
				t.Errorf("pack %s exited %d, want 2", arg, *exit)
			}
		})
	}
}

func TestPackLineChartCallShapes(t *testing.T) {
	for _, tc := range []struct {
		expr  string
		title string
	}{
		{`lineChart(ctx, "notes", "title")`, ""},
		{`ui.Card(ui.CardConfig{Heading: "By title"}, lineChart(ctx, "notes", "title"))`, "By title"},
		{`render.Tag("div", nil, html.Heading(2, render.Text("By title")), appUI.LineChart(ctx, "notes", "title"))`, "By title"},
	} {
		t.Run(tc.expr, func(t *testing.T) {
			expr, err := parser.ParseExpr(tc.expr)
			if err != nil {
				t.Fatal(err)
			}
			got, ok := reverseBlock(expr, nil)
			source, _ := got.Props["source"].(map[string]any)
			title, _ := got.Props["title"].(string)
			if !ok || got.Kind != "line_chart" || source["entity"] != "notes" || source["group_by"] != "title" || title != tc.title {
				t.Fatalf("reversed block = %#v, ok = %t", got, ok)
			}
		})
	}
}

func TestPackWritesQuotedEntityPropertyKey(t *testing.T) {
	bp := cleanupPackSpec()
	bp.Entities[0].Properties = map[string]any{"host:port": "localhost"}
	dir := materializeAppSpec(t, bp)
	exit := recordPackExit(t)
	out := clitest.CaptureStdout(t, func() { RunPack([]string{dir}) })
	if *exit != 0 {
		t.Fatalf("pack exited %d", *exit)
	}
	got, err := decodeAppSpecString(out)
	if err != nil || len(got.Entities) != 1 || got.Entities[0].Properties["host:port"] != "localhost" {
		t.Fatalf("packed entities = %#v, error = %v", got.Entities, err)
	}
}

func writeCleanupFile(t *testing.T, path, src string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
}

func recordPackExit(t *testing.T) *int {
	t.Helper()
	exit := new(int)
	restore := cli.OsExit
	cli.OsExit = func(code int) { *exit = code }
	t.Cleanup(func() { cli.OsExit = restore })
	return exit
}
