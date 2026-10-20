package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/codegen"
	"github.com/DonaldMurillo/gofastr/framework"
)

func packCheckFixture() AppSpec {
	return AppSpec{
		App: AppSpecApp{Name: "Notes", Module: "example.com/notes", DBDriver: "sqlite", DBURL: "file:notes.db"},
		Entities: []framework.EntityDeclaration{{
			Name: "notes",
			Fields: []framework.FieldDeclaration{
				{Name: "title", Type: "string", Required: true},
				{Name: "body", Type: "text"},
			},
		}},
	}
}

// packAndCheck packs dir and runs the check over it.
func packAndCheck(t *testing.T, dir string) packCheck {
	t.Helper()
	bp, err := packAppSpec(dir)
	if err != nil {
		t.Fatalf("packAppSpec: %v", err)
	}
	c, err := checkPack(dir, bp)
	if err != nil {
		t.Fatalf("checkPack: %v", err)
	}
	return c
}

// An app nobody touched packs to a spec that generates it again byte for
// byte, so the check has nothing to report.
func TestPackCheckSilentOnUntouchedApp(t *testing.T) {
	dir := materializeAppSpec(t, packCheckFixture())
	c := packAndCheck(t, dir)
	if len(c.HandWritten) != 0 || len(c.Unrecovered) != 0 {
		t.Fatalf("untouched app reported: %+v", c)
	}
	files, _ := packCheckFiles(mustOpenRoot(t, dir))
	if n := len(files); n < 3 {
		t.Fatalf("check covered %d files; the fixture should give it several", n)
	}
}

func TestPackCheckClassifiesFiles(t *testing.T) {
	dir := materializeAppSpec(t, packCheckFixture())
	write := func(rel, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(rel)), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	read := func(rel string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	stamp := func(rel, body string) string {
		t.Helper()
		out, err := codegen.Stamp(rel, body, codegen.Marker{Generator: "app", Kind: codegen.KindScaffold, Version: "dev"})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}

	// Hand-written: no marker, not generated.
	write("helpers.go", "package main\n\nfunc helper() {}\n")
	// Tests and files outside the layout are not part of the spec.
	write("helpers_test.go", "package main\n")
	if err := os.MkdirAll(filepath.Join(dir, "internal"), 0o700); err != nil {
		t.Fatal(err)
	}
	write("internal/x.go", "package internal\n")
	// Edited: the marker stays, the body changes.
	write("entities/notes.go", read("entities/notes.go")+"\n// hand edit\n")
	// Untouched, yet not reproduced: a body change restamped so its sum
	// matches, the shape of a part pack does not read back.
	_, mainBody, _ := codegen.ReadMarker("main.go", read("main.go"))
	write("main.go", stamp("main.go", mainBody+"\n// not in the spec\n"))
	// Unmarked and different: the marker stripped, the body changed.
	_, appBody, _ := codegen.ReadMarker("app.go", read("app.go"))
	write("app.go", appBody+"\n// hand edit\n")
	// Unmarked and the same: an app from before markers.
	_, extBody, _ := codegen.ReadMarker("extensions.go", read("extensions.go"))
	write("extensions.go", extBody)
	// Marked by the app generator, but the spec no longer generates it,
	// and edited since.
	write("ghost.go", stamp("ghost.go", "package main\n")+"\n// hand edit\n")
	// Untouched since an older release wrote it: the template may have
	// changed, so the note does not blame pack alone.
	_, regBody, _ := codegen.ReadMarker("screens_register.go", read("screens_register.go"))
	old, err := codegen.Stamp("screens_register.go", regBody+"\n// older template\n", codegen.Marker{Generator: "app", Kind: codegen.KindScaffold, Version: "v0.1.0"})
	if err != nil {
		t.Fatal(err)
	}
	write("screens_register.go", old)
	// Another generator's output is not the app generator's to recover.
	styles, err := codegen.Stamp("theme_style.gen.go", "package main\n", codegen.Marker{Generator: "styles", Kind: codegen.KindDerived})
	if err != nil {
		t.Fatal(err)
	}
	write("theme_style.gen.go", styles)

	c := packAndCheck(t, dir)
	if strings.Join(c.HandWritten, ",") != "helpers.go" {
		t.Errorf("hand-written = %v, want [helpers.go]", c.HandWritten)
	}
	want := map[string]string{
		"app.go":              "has no gofastr marker and differs",
		"entities/notes.go":   "edited since it was generated, and differs from what the packed spec generates",
		"ghost.go":            "the packed spec no longer produces it; edited since it was generated",
		"main.go":             "untouched since it was generated, yet the packed spec generates it differently",
		"screens_register.go": "untouched since gofastr v0.1.0 generated it",
	}
	got := map[string]string{}
	for _, n := range c.Unrecovered {
		got[n.Path] = n.Note
	}
	for path, note := range want {
		if !strings.Contains(got[path], note) {
			t.Errorf("%s: note = %q, want it to say %q", path, got[path], note)
		}
	}
	if len(got) != len(want) {
		t.Errorf("unrecovered = %v, want exactly %v", got, want)
	}
	if !strings.Contains(got["entities/notes.go"], "(entities)") {
		t.Errorf("entities/notes.go note names no spec section: %q", got["entities/notes.go"])
	}
}

// A checkout with CRLF line endings (git's core.autocrlf on Windows) or a
// UTF-8 byte order mark holds the same code: nothing to report.
func TestPackCheckIgnoresCRLFAndBOM(t *testing.T) {
	dir := materializeAppSpec(t, packCheckFixture())
	root := mustOpenRoot(t, dir)
	files, _ := packCheckFiles(root)
	for _, rel := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		data, err := os.ReadFile(full)
		if err != nil {
			t.Fatal(err)
		}
		out := strings.ReplaceAll(string(data), "\n", "\r\n")
		if rel == "main.go" {
			out = "\uFEFF" + out
		}
		if err := os.WriteFile(full, []byte(out), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	c := packAndCheck(t, dir)
	if len(c.HandWritten) != 0 || len(c.Unrecovered) != 0 || len(c.Unread) != 0 {
		t.Fatalf("CRLF app reported: %+v", c)
	}
}

// Files the check cannot read, a symlink among them, are files pack left
// out too: the check names them. A generated file the app no longer has
// is named as well.
func TestPackCheckNamesUnreadAndMissing(t *testing.T) {
	dir := materializeAppSpec(t, packCheckFixture())
	ext := filepath.Join(dir, "extensions.go")
	outside := filepath.Join(t.TempDir(), "extensions.go")
	if err := os.Rename(ext, outside); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, ext); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "screens_register.go")); err != nil {
		t.Fatal(err)
	}
	unreadable := os.Geteuid() != 0
	if unreadable {
		if err := os.Chmod(filepath.Join(dir, "main.go"), 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, "main.go"), 0o600) })
	}
	bp, err := packAppSpec(dir)
	if err != nil {
		t.Fatalf("packAppSpec: %v", err)
	}
	c, err := checkPack(dir, bp)
	if err != nil {
		t.Fatalf("checkPack: %v", err)
	}
	unread := map[string]string{}
	for _, n := range c.Unread {
		unread[n.Path] = n.Note
	}
	if !strings.Contains(unread["extensions.go"], "symlink") {
		t.Errorf("extensions.go: unread note = %q, want it named as a symlink (all: %v)", unread["extensions.go"], unread)
	}
	if unreadable && unread["main.go"] == "" {
		t.Errorf("main.go is unreadable and not named (all: %v)", unread)
	}
	notes := map[string]string{}
	for _, n := range c.Unrecovered {
		notes[n.Path] = n.Note
	}
	if !strings.Contains(notes["screens_register.go"], "not in the app") {
		t.Errorf("screens_register.go: note = %q, want the deletion named", notes["screens_register.go"])
	}
	for _, rel := range []string{"extensions.go", "main.go"} {
		if notes[rel] != "" {
			t.Errorf("%s is reported missing as well as unread: %q", rel, notes[rel])
		}
	}
}

// With no -o the YAML is stdout, so the check's notes go to stderr and
// the captured spec still parses.
func TestPackCheckNotesGoToStderr(t *testing.T) {
	dir := materializeAppSpec(t, packCheckFixture())
	if err := os.WriteFile(filepath.Join(dir, "helpers.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	errF, err := os.CreateTemp(t.TempDir(), "stderr-*")
	if err != nil {
		t.Fatal(err)
	}
	oldErr := os.Stderr
	os.Stderr = errF
	out := captureStdout(t, func() { runPack([]string{dir}) })
	os.Stderr = oldErr
	if _, err := errF.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	stderr, err := io.ReadAll(errF)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stderr), "1 hand-written file(s) are not in the spec: helpers.go") {
		t.Fatalf("stderr:\n%s", stderr)
	}
	if strings.Contains(out, "hand-written") {
		t.Fatalf("stdout carries the note:\n%s", out)
	}
	if _, err := decodeAppSpecString(out); err != nil {
		t.Fatalf("stdout does not parse: %v\n%s", err, out)
	}
}

// The untouched note blames pack only when this release wrote the file.
// A file another release or a dev build wrote may differ because the
// template changed, and a dev build cannot rule that out either.
func TestUntouchedNoteNamesTemplateDrift(t *testing.T) {
	cases := []struct {
		cli, file string
		want      string
		not       string
	}{
		{"v0.92.0", "v0.92.0", "pack lost part of it", "template"},
		{"v0.92.0", "v0.91.0", "since gofastr v0.91.0 generated it; this gofastr (v0.92.0)", ""},
		{"v0.92.0", "dev", "or the template changed since", "gofastr dev"},
		{"dev", "dev", "or the template changed since", "gofastr dev"},
		{"dev", "v0.91.0", "since gofastr v0.91.0 generated it; this gofastr (dev)", ""},
	}
	old := version
	t.Cleanup(func() { version = old })
	for _, tc := range cases {
		version = tc.cli
		got := untouchedNote(tc.file)
		if !strings.Contains(got, tc.want) || (tc.not != "" && strings.Contains(got, tc.not)) {
			t.Errorf("cli %s, file %s: %q; want %q and not %q", tc.cli, tc.file, got, tc.want, tc.not)
		}
	}
}

func mustOpenRoot(t *testing.T, dir string) *os.Root {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return root
}

// The code-only render never fetches webfonts: validate and pack's check
// read the render and must not reach the network.
func TestRenderAppSpecCodeFetchesNoFonts(t *testing.T) {
	stubFontFetcher(t, func(family string) ([]byte, error) {
		t.Errorf("fetched %q", family)
		return nil, nil
	})
	bp := packCheckFixture()
	bp.App.Theme = map[string]string{"font_heading": "Test Sans"}
	if _, err := renderAppSpecCode(bp); err != nil {
		t.Fatal(err)
	}
}

func TestValidateFetchesNoFonts(t *testing.T) {
	stubFontFetcher(t, func(family string) ([]byte, error) {
		t.Errorf("validate fetched %q", family)
		return nil, nil
	})
	path := filepath.Join(t.TempDir(), "gofastr.yml")
	writeValidateFile(t, path, `app:
  name: Fonts
  module: example.com/fonts
  theme:
    font_heading: Test Sans
`)
	var code int
	out := covT_capStdout(t, func() {
		code = covT_capExit(t, func() { runValidate([]string{path}) })
	})
	if code != -1 && code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
}
