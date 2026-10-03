package main

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The `gofastr desktop` subcommand: usage routing, flag parsing, the
// bundle layout (plist + icns) on a tiny fixture, and --id validation.
// `types` is exercised against examples/desktop-notes in
// desktop_types_example_test.go (it compiles the real example).

func TestDesktopHelpIsCommandSpecific(t *testing.T) {
	out := covT_capStdout(t, func() { dispatch([]string{"desktop", "--help"}) })
	if want := "Usage: gofastr desktop"; !strings.Contains(out, want) {
		t.Fatalf("desktop --help missing %q:\n%s", want, out)
	}
	for _, verb := range []string{"run", "build", "types"} {
		if !strings.Contains(out, verb) {
			t.Fatalf("desktop --help does not mention %q", verb)
		}
	}
	if strings.Contains(out, "Start dev server with auto-restart") {
		t.Fatal("desktop --help fell back to the global help page")
	}
}

func TestDesktopNoVerbPrintsUsageAndExits(t *testing.T) {
	var code int
	out := covT_capStdout(t, func() {
		code = covT_capExit(t, func() { dispatch([]string{"desktop"}) })
	})
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(out, "Usage: gofastr desktop") {
		t.Fatalf("no usage printed:\n%s", out)
	}
}

func TestDesktopRunFlagParsing(t *testing.T) {
	f := parseDesktopRunFlags([]string{"--dir", "/tmp/proj", "--pkg=./cmd/app", "--watch"})
	if f.dir != "/tmp/proj" || f.pkg != "./cmd/app" || !f.watch {
		t.Fatalf("flags = %+v", f)
	}
	def := parseDesktopRunFlags(nil)
	if def.dir != "." || def.pkg != "." || def.watch {
		t.Fatalf("defaults = %+v", def)
	}
}

func TestDesktopRunEnvConstruction(t *testing.T) {
	env := buildDevChildEnv([]string{"GOFASTR_DEV=0", "HOME=/x"}, "")
	if len(env) != 2 || env[0] != "GOFASTR_DEV=1" || env[1] != "HOME=/x" {
		t.Fatalf("env = %v", env)
	}
	if strings.HasPrefix(env[len(env)-1], "PORT=") {
		t.Fatal("desktop run must not inject PORT: the host picks its own port")
	}
}

func TestDesktopBuildIDValidation(t *testing.T) {
	for _, bad := range []string{"", "nodot", "has space", "a/b", strings.Repeat("x", 254)} {
		if err := validateDesktopID(bad); err == nil {
			t.Fatalf("id %q accepted", bad)
		}
	}
	for _, good := range []string{"dev.gofastr.notes", "a.b", "Dev.Notes-1"} {
		if err := validateDesktopID(good); err != nil {
			t.Fatalf("id %q rejected: %v", good, err)
		}
	}
}

func TestDesktopBuildNameValidation(t *testing.T) {
	for _, bad := range []string{"", "a/b", "a\\b", "line\nbreak", strings.Repeat("n", 65)} {
		if err := validateDesktopName(bad); err == nil {
			t.Fatalf("name %q accepted", bad)
		}
	}
	if err := validateDesktopName("Notes & Co"); err != nil {
		t.Fatalf("printable name rejected: %v", err)
	}
}

func TestDesktopBuildRejectsBadIDBeforeBuilding(t *testing.T) {
	var code int
	out := covT_capStdout(t, func() {
		code = covT_capExit(t, func() {
			runDesktopBuild([]string{"--id", "no-dot", "--pkg", "./does/not/exist"})
		})
	})
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(out, "reverse-DNS") {
		t.Fatalf("error does not name the rule:\n%s", out)
	}
	if _, err := os.Stat("dist"); err == nil {
		t.Fatal("dist/ created for an invalid --id")
	}
}

// desktopFixtureApp writes a minimal main package the build verbs can
// compile (the CLI builds whatever package it is given).
func desktopFixtureApp(t *testing.T, dir string) {
	t.Helper()
	devPkgWrite(t, filepath.Join(dir, "go.mod"), "module deskfixture\n\ngo 1.21\n")
	devPkgWrite(t, filepath.Join(dir, "main.go"),
		"package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(\"desktop fixture\") }\n")
}
func TestDesktopBuildProducesBundleLayout(t *testing.T) {
	fixture := t.TempDir()
	desktopFixtureApp(t, fixture)

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(fixture); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	out := filepath.Join(fixture, "dist")
	var code int
	covT_capStdout(t, func() {
		code = covT_capExit(t, func() {
			runDesktopBuild([]string{
				"--id", "dev.gofastr.fixture",
				"--name", "Fixture Notes & Co",
				"--pkg", ".",
				"--version", "0.2.0",
				"-o", out,
			})
		})
	})
	if code != -1 {
		t.Fatalf("desktop build exited %d", code)
	}

	appDir := filepath.Join(out, "Fixture Notes & Co.app")
	for _, rel := range []string{
		"Contents/Info.plist",
		"Contents/PkgInfo",
		"Contents/Resources/icon.icns",
	} {
		if _, err := os.Stat(filepath.Join(appDir, rel)); err != nil {
			t.Fatalf("bundle missing %s: %v", rel, err)
		}
	}
	exe, err := os.Stat(filepath.Join(appDir, "Contents", "MacOS", "Fixture Notes & Co"))
	if err != nil || exe.Mode()&0o111 == 0 {
		t.Fatalf("bundle executable missing or not executable: %v", err)
	}
	pkgInfo, err := os.ReadFile(filepath.Join(appDir, "Contents", "PkgInfo"))
	if err != nil || string(pkgInfo) != "APPL????" {
		t.Fatalf("PkgInfo = %q (%v)", pkgInfo, err)
	}

	// The plist parses back and carries the required keys, including
	// the escaped name.
	plistBytes, err := os.ReadFile(filepath.Join(appDir, "Contents", "Info.plist"))
	if err != nil {
		t.Fatal(err)
	}
	keys := parsePlistKeys(t, plistBytes)
	for k, want := range map[string]string{
		"CFBundleName":               "Fixture Notes & Co",
		"CFBundleIdentifier":         "dev.gofastr.fixture",
		"CFBundleExecutable":         "Fixture Notes & Co",
		"CFBundlePackageType":        "APPL",
		"CFBundleIconFile":           "icon",
		"CFBundleShortVersionString": "0.2.0",
		"CFBundleVersion":            "0.2.0",
		"LSMinimumSystemVersion":     "12.0",
		"LSApplicationCategoryType":  "public.app-category.productivity",
	} {
		if keys[k] != want {
			t.Errorf("plist %s = %q, want %q", k, keys[k], want)
		}
	}
	if !strings.Contains(string(plistBytes), "NSAllowsLocalNetworking") ||
		!strings.Contains(string(plistBytes), "NSHighResolutionCapable") {
		t.Error("plist missing the ATS / HiDPI keys")
	}

	// The icns header and entry table parse; every payload is a PNG.
	icns, err := os.ReadFile(filepath.Join(appDir, "Contents", "Resources", "icon.icns"))
	if err != nil {
		t.Fatal(err)
	}
	entries := parseICNS(t, icns)
	if len(entries) != len(icnsEntries) {
		t.Fatalf("icns entries = %d, want %d", len(entries), len(icnsEntries))
	}
	for _, e := range entries {
		if !bytes.HasPrefix(e.payload, []byte("\x89PNG\r\n\x1a\n")) {
			t.Errorf("entry %s payload is not a PNG", e.typ)
		}
	}
}

// parsePlistKeys decodes the top-level <key>/<string>/<true> pairs of a
func parsePlistKeys(t *testing.T, b []byte) map[string]string {
	t.Helper()
	dec := xml.NewDecoder(bytes.NewReader(b))
	out := map[string]string{}
	depth := 0
	lastKey := ""
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch el := tok.(type) {
		case xml.StartElement:
			switch el.Name.Local {
			case "dict":
				depth++
			case "key":
				var s string
				if err := dec.DecodeElement(&s, &el); err != nil {
					t.Fatalf("decode key: %v", err)
				}
				if depth == 1 {
					lastKey = s
				}
			case "string":
				var s string
				if err := dec.DecodeElement(&s, &el); err != nil {
					t.Fatalf("decode string: %v", err)
				}
				if depth == 1 && lastKey != "" {
					out[lastKey] = s
					lastKey = ""
				}
			case "true", "false":
				if depth == 1 && lastKey != "" {
					out[lastKey] = el.Name.Local
					lastKey = ""
				}
			}
		case xml.EndElement:
			if el.Name.Local == "dict" {
				depth--
			}
		}
	}
	if len(out) == 0 {
		t.Fatal("no plist keys decoded")
	}
	return out
}

type icnsEntryParsed struct {
	typ     string
	payload []byte
}

func parseICNS(t *testing.T, b []byte) []icnsEntryParsed {
	t.Helper()
	if string(b[:4]) != "icns" {
		t.Fatalf("icns magic = %q", b[:4])
	}
	total := binary.BigEndian.Uint32(b[4:8])
	if int(total) != len(b) {
		t.Fatalf("icns total = %d, file = %d", total, len(b))
	}
	var entries []icnsEntryParsed
	p := 8
	for p < len(b) {
		if p+8 > len(b) {
			t.Fatalf("truncated entry header at %d", p)
		}
		typ := string(b[p : p+4])
		length := binary.BigEndian.Uint32(b[p+4 : p+8])
		if int(length) < 8 || p+int(length) > len(b) {
			t.Fatalf("entry %s bad length %d at %d", typ, length, p)
		}
		entries = append(entries, icnsEntryParsed{typ: typ, payload: b[p+8 : p+int(length)]})
		p += int(length)
	}
	return entries
}

func TestDesktopBuildDefaultsNameFromID(t *testing.T) {
	f := parseDesktopBuildFlags([]string{"--id=dev.gofastr.desktop-notes"})
	if f.name != "" {
		t.Fatalf("explicit name default = %q, want empty (derived later)", f.name)
	}
	if f.out != "dist" || f.version != "0.1.0" || f.pkg != "." {
		t.Fatalf("build defaults = %+v", f)
	}
}

func TestDesktopTypesFlagParsing(t *testing.T) {
	f := parseDesktopTypesFlags([]string{"--pkg", "./examples/desktop-notes", "--out=/tmp/x.d.ts"})
	if f.pkg != "./examples/desktop-notes" || f.out != "/tmp/x.d.ts" {
		t.Fatalf("flags = %+v", f)
	}
	def := parseDesktopTypesFlags(nil)
	if def.pkg != "." || def.out != "desktop.d.ts" {
		t.Fatalf("defaults = %+v", def)
	}
}

func TestDesktopBuildSignFlagParsing(t *testing.T) {
	f := parseDesktopBuildFlags([]string{"--id=dev.gofastr.x", "--sign=Developer ID Application: Example"})
	if f.sign != "Developer ID Application: Example" || f.noSign {
		t.Fatalf("sign flags = %+v", f)
	}
	f = parseDesktopBuildFlags([]string{"--id=dev.gofastr.x", "--sign", "adhoc-test"})
	if f.sign != "adhoc-test" {
		t.Fatalf("space-separated --sign = %q", f.sign)
	}
	f = parseDesktopBuildFlags([]string{"--id=dev.gofastr.x", "--no-sign"})
	if !f.noSign || f.sign != "" {
		t.Fatalf("no-sign flags = %+v", f)
	}
	f = parseDesktopBuildFlags([]string{"--id=dev.gofastr.x"})
	if f.noSign || f.sign != "" {
		t.Fatalf("defaults = %+v (want ad-hoc default, decided later)", f)
	}
}

// TestDesktopBuildAdhocSignsBundle proves the default signing path on
// a real build: with codesign present (darwin dev machines, CI macOS
// runners) the produced bundle must carry an ad-hoc signature, which
// is what UNUserNotificationCenter needs before it will post.
func TestDesktopBuildAdhocSignsBundle(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("codesign ad-hoc check is darwin-only")
	}
	if _, err := exec.LookPath("codesign"); err != nil {
		t.Skip("no codesign on this host")
	}
	fixture := t.TempDir()
	desktopFixtureApp(t, fixture)

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(fixture); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	out := filepath.Join(fixture, "dist")
	var code int
	covT_capStdout(t, func() {
		code = covT_capExit(t, func() {
			runDesktopBuild([]string{"--id", "dev.gofastr.signed", "--name", "Signed", "--pkg", ".", "-o", out})
		})
	})
	if code != -1 {
		t.Fatalf("desktop build exited %d", code)
	}

	appDir := filepath.Join(out, "Signed.app")
	verify := exec.Command("codesign", "-dv", appDir)
	var stderr bytes.Buffer
	verify.Stderr = &stderr
	if err := verify.Run(); err != nil {
		t.Fatalf("codesign -dv: %v\n%s", err, stderr.String())
	}
	if !strings.Contains(stderr.String(), "Signature=adhoc") {
		t.Fatalf("bundle is not ad-hoc signed:\n%s", stderr.String())
	}
}
