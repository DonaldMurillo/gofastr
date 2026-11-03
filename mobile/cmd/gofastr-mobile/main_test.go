package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const notesPkg = "github.com/DonaldMurillo/gofastr-mobile/examples/notes/notesmobile"

func notesApp(t *testing.T) app {
	t.Helper()
	a, err := newApp(notesPkg, "notesmobile", "dev.gofastr.notes", "Notes", "")
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// gobindOut runs the module's pinned gobind over the notes bind package
// and returns its output directory: the exact Objective-C and Java API
// gomobile will hand the generated hosts.
func gobindOut(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "gobind")
	if out, err := exec.Command("go", "build", "-o", bin, "golang.org/x/mobile/cmd/gobind").CombinedOutput(); err != nil {
		t.Skipf("cannot build gobind (offline module cache?): %v\n%s", err, out)
	}
	out := filepath.Join(dir, "out")
	cmd := exec.Command(bin, "-lang=java,objc", "-outdir", out, notesPkg)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("gobind: %v\n%s", err, b)
	}
	return out
}

// The Swift host calls the functions and methods gomobile really
// generates, with the shapes Swift imports them as: C functions keep their
// NSError** out-parameter, methods with one become `throws`.
func TestSwiftTemplateMatchesGeneratedObjC(t *testing.T) {
	out := gobindOut(t)
	header, err := os.ReadFile(filepath.Join(out, "src", "gobind", "Notesmobile.objc.h"))
	if err != nil {
		t.Fatal(err)
	}
	swift, err := render("templates/ios/App.swift.tmpl", notesApp(t).forIOS())
	if err != nil {
		t.Fatal(err)
	}
	h, s := string(header), string(swift)
	for _, fn := range []string{"Start", "Stop", "Handle", "Open"} {
		if !strings.Contains(h, " Notesmobile"+fn+"(") {
			t.Errorf("gomobile no longer exports Notesmobile%s; the bind contract changed", fn)
		}
		if !strings.Contains(s, "Notesmobile"+fn+"(") {
			t.Errorf("the Swift host never calls Notesmobile%s", fn)
		}
	}
	for _, want := range []struct{ objc, swift string }{
		{"- (NSData* _Nullable)read:(long)max error:(NSError* _Nullable* _Nullable)error;", "try s.read("},
		{"- (BOOL)close:(NSError* _Nullable* _Nullable)error;", "try? s.close()"},
		{"@property (nonatomic) long status;", "statusCode: status"},
		{"@property (nonatomic) NSData* _Nullable body;", "resp.body ?? Data()"},
	} {
		if !strings.Contains(h, want.objc) {
			t.Errorf("generated header lacks %q", want.objc)
		}
		if !strings.Contains(s, want.swift) {
			t.Errorf("the Swift host lacks %q (the call shape for %q)", want.swift, want.objc)
		}
	}
	if strings.Contains(s, "{{") {
		t.Error("an unfilled template action survived")
	}
}

// The Java host compiles against gomobile's real Java bindings and
// signature-exact stand-ins for the Android SDK classes it touches.
func TestJavaTemplateCompilesAgainstGeneratedBindings(t *testing.T) {
	javac, err := exec.LookPath("javac")
	if err != nil {
		t.Skip("no javac")
	}
	out := gobindOut(t)
	src := t.TempDir()
	stubs, err := os.ReadFile(filepath.Join("testdata", "androidstubs.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, chunk := range strings.Split(string(stubs), "\n--- ")[1:] {
		name, body, _ := strings.Cut(chunk, "\n")
		p := filepath.Join(src, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		files = append(files, p)
	}
	bindings, err := listFiles(filepath.Join(out, "java"), ".java")
	if err != nil || len(bindings) == 0 {
		t.Fatalf("no generated Java bindings: %v", err)
	}
	files = append(files, bindings...)
	a := notesApp(t).forAndroid(35)
	activity := filepath.Join(src, "dev", "gofastr", "notes", "MainActivity.java")
	if err := writeRendered("templates/android/MainActivity.java.tmpl", activity, a); err != nil {
		t.Fatal(err)
	}
	files = append(files, activity)
	args := append([]string{"--release", "11", "-Xlint:none", "-d", t.TempDir()}, files...)
	if b, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, b)
	}
}

func wellFormed(t *testing.T, name string, b []byte) {
	t.Helper()
	d := xml.NewDecoder(bytes.NewReader(b))
	d.Strict = true
	for {
		if _, err := d.Token(); err == io.EOF {
			return
		} else if err != nil {
			t.Fatalf("%s is not well-formed XML: %v\n%s", name, err, b)
		}
	}
}

func TestPlistAndManifestRender(t *testing.T) {
	a := notesApp(t)
	plist, err := render("templates/ios/Info.plist.tmpl", a.forIOS())
	if err != nil {
		t.Fatal(err)
	}
	wellFormed(t, "Info.plist", plist)
	for _, want := range []string{"<string>dev.gofastr.notes</string>", "<string>Notesmobile</string>", "<string>16.0</string>", "UILaunchScreen"} {
		if !bytes.Contains(plist, []byte(want)) {
			t.Errorf("Info.plist lacks %s", want)
		}
	}
	manifest, err := render("templates/android/AndroidManifest.xml.tmpl", a.forAndroid(35))
	if err != nil {
		t.Fatal(err)
	}
	wellFormed(t, "AndroidManifest.xml", manifest)
	for _, want := range []string{`package="dev.gofastr.notes"`, `android:name=".MainActivity"`, `android:targetSdkVersion="35"`, `android:minSdkVersion="24"`, `android:label="Notes"`} {
		if !bytes.Contains(manifest, []byte(want)) {
			t.Errorf("AndroidManifest.xml lacks %s", want)
		}
	}
}

// Every value that lands in generated source passes a grammar first: the
// templates never quote, so a name with a quote or an id with a Java
// keyword must be refused here.
func TestNewAppRefusesUnsafeValues(t *testing.T) {
	for _, tc := range []struct{ pkg, id, name, version string }{
		{"Notes", "", "", ""},                      // gomobile needs lowercase
		{"notes", "notes", "", ""},                 // one segment
		{"notes", "dev.class.notes", "", ""},       // Java keyword
		{"notes", "dev.gofastr.no-tes", "", ""},    // hyphen
		{"notes", "", `My "Notes"`, ""},            // quote into Swift/Java
		{"notes", "", "Notes</string>", ""},        // markup into the plist
		{"notes", "", strings.Repeat("n", 31), ""}, // too long for a home screen
		{"notes", "", "", "1.0-beta"},              // not N.N.N
	} {
		if _, err := newApp("x/"+tc.pkg, tc.pkg, tc.id, tc.name, tc.version); err == nil {
			t.Errorf("newApp(%q, id %q, name %q, version %q) accepted an unsafe value", tc.pkg, tc.id, tc.name, tc.version)
		}
	}
	a, err := newApp("x/notes", "notes", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != "dev.gofastr.notes" || a.Name != "Notes" || a.Prefix != "Notes" || a.Version != "0.1.0" {
		t.Fatalf("defaults = %+v", a)
	}
}

func TestPickSimulator(t *testing.T) {
	listing := []byte(`{"devices":{
	  "com.apple.CoreSimulator.SimRuntime.iOS-17-5":[{"udid":"OLD","name":"iPhone 15","state":"Shutdown","isAvailable":true}],
	  "com.apple.CoreSimulator.SimRuntime.iOS-18-2":[
	    {"udid":"PAD","name":"iPad Air","state":"Shutdown","isAvailable":true},
	    {"udid":"NEW","name":"iPhone 16","state":"Shutdown","isAvailable":true}],
	  "com.apple.CoreSimulator.SimRuntime.watchOS-11-0":[{"udid":"WATCH","name":"Apple Watch","state":"Booted","isAvailable":true}]
	}}`)
	if got, _ := pickSimulator(listing, ""); got != "NEW" {
		t.Errorf("default pick = %q, want the iPhone on the newest runtime", got)
	}
	booted := bytes.Replace(listing, []byte(`"OLD","name":"iPhone 15","state":"Shutdown"`), []byte(`"OLD","name":"iPhone 15","state":"Booted"`), 1)
	if got, _ := pickSimulator(booted, ""); got != "OLD" {
		t.Errorf("pick = %q, want the booted iPhone", got)
	}
	if got, _ := pickSimulator(listing, "iPad Air"); got != "PAD" {
		t.Errorf("--device by name = %q", got)
	}
	if _, err := pickSimulator(listing, "iPhone 99"); err == nil {
		t.Error("an unknown --device was accepted")
	}
	if _, err := pickSimulator([]byte(`{"devices":{}}`), ""); err == nil {
		t.Error("no simulators answered a UDID")
	}
}

func TestParseADBDevices(t *testing.T) {
	got := parseADBDevices("List of devices attached\nemulator-5554\tdevice\nR58M\tunauthorized\nZX1\tdevice\n\n")
	if strings.Join(got, ",") != "emulator-5554,ZX1" {
		t.Fatalf("serials = %v", got)
	}
}

func writeZip(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.WriteString(w, body)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

// The aar is unpacked to exactly classes.jar and jni/<abi>/*.so (a name
// that climbs out of the directory is never written), and the APK gains
// classes.dex and lib/<abi>/*.so beside aapt2's entries.
func TestUnpackAARAndAddToAPK(t *testing.T) {
	dir := t.TempDir()
	aar := filepath.Join(dir, "x.aar")
	writeZip(t, aar, map[string]string{
		"classes.jar":                 "jar",
		"jni/arm64-v8a/libgojni.so":   "arm",
		"jni/x86_64/libgojni.so":      "x86",
		"AndroidManifest.xml":         "skip",
		"jni/../../escape.so":         "no",
		"jni/arm64-v8a/../../../e.so": "no",
	})
	unpacked := filepath.Join(dir, "sub", "aar")
	if err := unpackAAR(aar, unpacked); err != nil {
		t.Fatal(err)
	}
	got, err := listFiles(unpacked, "")
	if err != nil {
		t.Fatal(err)
	}
	var rel []string
	for _, g := range got {
		r, _ := filepath.Rel(unpacked, g)
		rel = append(rel, filepath.ToSlash(r))
	}
	if strings.Join(rel, ",") != "classes.jar,jni/arm64-v8a/libgojni.so,jni/x86_64/libgojni.so" {
		t.Fatalf("unpacked %v", rel)
	}
	for _, p := range []string{filepath.Join(dir, "escape.so"), filepath.Join(dir, "sub", "escape.so"), filepath.Join(dir, "e.so")} {
		if fileExists(p) {
			t.Fatalf("an entry escaped the unpack directory: %s", p)
		}
	}

	base := filepath.Join(dir, "base.apk")
	writeZip(t, base, map[string]string{"AndroidManifest.xml": "binary manifest", "resources.arsc": "res"})
	dex := filepath.Join(dir, "classes.dex")
	if err := os.WriteFile(dex, []byte("dex"), 0o600); err != nil {
		t.Fatal(err)
	}
	apk := filepath.Join(dir, "out.apk")
	if err := addToAPK(base, apk, dex, filepath.Join(unpacked, "jni")); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(apk)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	for _, want := range []string{"AndroidManifest.xml", "resources.arsc", "classes.dex", "lib/arm64-v8a/libgojni.so", "lib/x86_64/libgojni.so"} {
		if !names[want] {
			t.Errorf("APK lacks %s (has %v)", want, names)
		}
	}
}

func TestHighestVersionDir(t *testing.T) {
	dir := t.TempDir()
	for _, v := range []string{"34.0.0", "35.0.1", "9.9.9", "35.0.0-rc1", "notaversion"} {
		if err := os.MkdirAll(filepath.Join(dir, v), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if got := highestVersionDir(dir); got != "35.0.1" {
		t.Fatalf("highest = %q", got)
	}
}

// A dry run prints the whole plan for each platform on any OS and runs
// nothing: the plan is what a reviewer (or a person without a Mac) reads.
func TestDryRunPlans(t *testing.T) {
	for _, tc := range []struct {
		platform string
		want     []string
	}{
		{"ios-sim", []string{"bind -target=iossimulator", "swiftc -parse-as-library", "-framework Notesmobile", "codesign --force --sign -", "simctl install", "simctl launch --terminate-running-process", "dev.gofastr.notes", "simctl io"}},
		{"android-emu", []string{"bind -target=android/arm64,android/amd64", "javac --release 11", "d8 --release --min-api 24", "aapt2 link", "zipalign -p -f 4", "apksigner sign", "install -r", "dev.gofastr.notes/.MainActivity", "screencap -p"}},
	} {
		t.Run(tc.platform, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			out := t.TempDir()
			code := run(context.Background(), []string{"run", "--platform", tc.platform, "--pkg", notesPkg,
				"--id", "dev.gofastr.notes", "--name", "Notes", "--out", out, "--screenshot", filepath.Join(out, "s.png"), "--dry-run"}, &stdout, &stderr)
			if code != 0 {
				t.Fatalf("exit %d\n%s\n%s", code, stdout.String(), stderr.String())
			}
			// Placeholder SDK paths ('$ANDROID_HOME/...') print shell-quoted.
			plan := strings.ReplaceAll(stdout.String(), "'", "")
			for _, w := range tc.want {
				if !strings.Contains(plan, w) {
					t.Errorf("plan lacks %q:\n%s", w, plan)
				}
			}
			if entries, _ := os.ReadDir(out); len(entries) != 0 {
				t.Errorf("a dry run wrote %d entries into --out", len(entries))
			}
		})
	}
}

func TestUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{"run"},
		{"run", "--platform", "windows-phone", "--pkg", notesPkg},
		{"build", "--platform", "ios-sim"},
		{"run", "--platform", "ios-sim", "--pkg", notesPkg, "--wait", "-1s"},
		{"frobnicate"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(context.Background(), args, &stdout, &stderr); code != 2 {
			t.Errorf("%v: exit %d, want 2 (usage)", args, code)
		}
	}
}

func TestSimArch(t *testing.T) {
	want := map[string]string{"amd64": "x86_64", "arm64": "arm64"}[runtime.GOARCH]
	if want != "" && simArch() != want {
		t.Fatalf("simArch = %s on %s", simArch(), runtime.GOARCH)
	}
}
