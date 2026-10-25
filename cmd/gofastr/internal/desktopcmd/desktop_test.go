package desktopcmd

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/internal/fileperm/filepermtest"
)

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

func TestValidWindowsArtifactName(t *testing.T) {
	for _, bad := range []string{
		"bad<name", "bad>name", "bad:name", `bad"name`, "bad|name",
		"bad?name", "bad*name", "bad/name", `bad\name`, "bad\nname",
		"NUL.foo", "NUL .txt", "con.txt", "COM1.exe", "LPT9.log",
		"COM¹", "COM².txt", "COM³.exe", "LPT¹", "LPT².log", "LPT³",
		"name.", "name ", ".", "..",
	} {
		if validWindowsArtifactName(bad) {
			t.Errorf("validWindowsArtifactName(%q) = true, want false", bad)
		}
	}
	for _, good := range []string{"notes", "Notes & Co", "COM10", "LPT0", "ordinary.exe", ".NET Notes", ".notes", ".exe"} {
		if !validWindowsArtifactName(good) {
			t.Errorf("validWindowsArtifactName(%q) = false, want true", good)
		}
	}
}

func TestDesktopBuildRejectsBadIDBeforeBuilding(t *testing.T) {
	var code int
	out := clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() {
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
	clitest.WriteFileAll(t, filepath.Join(dir, "go.mod"), "module deskfixture\n\ngo 1.21\n")
	clitest.WriteFileAll(t, filepath.Join(dir, "main.go"),
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
	clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() {
			runDesktopBuildForOS([]string{
				"--id", "dev.gofastr.fixture",
				"--name", "Fixture Notes & Co",
				"--pkg", ".",
				"--version", "0.2.0",
				"-o", out,
			}, "darwin")
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
	filepermtest.AssertMode(t, filepath.Join(appDir, "Contents", "MacOS", "Fixture Notes & Co"), 0o755)
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

func TestBuildWindowsICOEmitsFourPNGFrames(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 4, 3))
	for y := 0; y < source.Bounds().Dy(); y++ {
		for x := 0; x < source.Bounds().Dx(); x++ {
			source.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 40), G: uint8(y * 60), A: 0xff})
		}
	}
	var pngBytes bytes.Buffer
	if err := png.Encode(&pngBytes, source); err != nil {
		t.Fatal(err)
	}
	ico, err := buildWindowsICO(pngBytes.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(ico) < 6 || binary.LittleEndian.Uint16(ico[0:2]) != 0 || binary.LittleEndian.Uint16(ico[2:4]) != 1 {
		t.Fatalf("ICO header = %x", ico[:cli.Min(len(ico), 6)])
	}
	count := int(binary.LittleEndian.Uint16(ico[4:6]))
	if count != 4 || len(ico) < 6+count*16 {
		t.Fatalf("ICO frame count = %d, length = %d", count, len(ico))
	}
	for i, wantSize := range []int{16, 32, 48, 256} {
		entry := 6 + i*16
		width, height := int(ico[entry]), int(ico[entry+1])
		if wantSize == 256 {
			width, height = 256, 256 // zero encodes 256 in ICO directories
		}
		if width != wantSize || height != wantSize {
			t.Errorf("frame %d size = %dx%d, want %dx%d", i, width, height, wantSize, wantSize)
		}
		length := int(binary.LittleEndian.Uint32(ico[entry+8 : entry+12]))
		offset := int(binary.LittleEndian.Uint32(ico[entry+12 : entry+16]))
		if length <= 0 || offset < 6+count*16 || offset+length > len(ico) {
			t.Fatalf("frame %d has invalid payload range %d:%d in %d bytes", i, offset, offset+length, len(ico))
		}
		frame, err := png.Decode(bytes.NewReader(ico[offset : offset+length]))
		if err != nil {
			t.Fatalf("frame %d is not a PNG payload: %v", i, err)
		}
		if frame.Bounds().Dx() != wantSize || frame.Bounds().Dy() != wantSize {
			t.Errorf("frame %d PNG size = %v, want %dx%d", i, frame.Bounds(), wantSize, wantSize)
		}
	}
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
	clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() {
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
