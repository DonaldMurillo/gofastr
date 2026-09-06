package main

// Tests for the runTool seam and the --notarize pipeline: the exact
// argv of every external command on the ad-hoc, identity, and notarize
// paths, the refusal of --notarize without a real identity before any
// tool runs, and each pipeline step's failure failing the build.
// Nothing here calls a real codesign, xcrun, or Apple's service; the
// seam fake answers every tool.

import (
	"archive/zip"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeToolCall is one recorded external command.
type fakeToolCall struct {
	name string
	args []string
}

// fakeRunTool replaces the runTool seam for the test, recording every
// call's argv and answering from fn (nil: success, no output).
func fakeRunTool(t *testing.T, fn func(name string, args []string) (string, error)) *[]fakeToolCall {
	t.Helper()
	calls := &[]fakeToolCall{}
	orig := runTool
	runTool = func(name string, args ...string) (string, error) {
		*calls = append(*calls, fakeToolCall{name, slices.Clone(args)})
		if fn != nil {
			return fn(name, args)
		}
		return "", nil
	}
	t.Cleanup(func() { runTool = orig })
	return calls
}

// assertCalls fails unless the recorded calls match want exactly, argv
// for argv.
func assertCalls(t *testing.T, got, want []fakeToolCall) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("tool calls:\ngot  %v\nwant %v", got, want)
	}
	for i := range got {
		if got[i].name != want[i].name || !slices.Equal(got[i].args, want[i].args) {
			t.Fatalf("call %d:\ngot  %s %v\nwant %s %v", i, got[i].name, got[i].args, want[i].name, want[i].args)
		}
	}
}

// buildFixtureBundle runs runDesktopBuild for a tiny fixture app named
// name inside a temp dir and returns the captured output, the exit
// code (-1 = returned normally), and the dist dir it built into.
func buildFixtureBundle(t *testing.T, name string, extra ...string) (string, int, string) {
	t.Helper()
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
	id := "dev.gofastr." + strings.ToLower(strings.ReplaceAll(name, " ", ""))
	args := append([]string{"--id", id, "--name", name, "--pkg", ".", "-o", out}, extra...)
	var code int
	captured := covT_capStdout(t, func() {
		code = covT_capExit(t, func() { runDesktopBuild(args) })
	})
	return captured, code, out
}

func TestDesktopBuildAdhocSigningArgv(t *testing.T) {
	if _, err := exec.LookPath("codesign"); err != nil {
		t.Skip("the default ad-hoc path needs codesign on PATH (the LookPath gate)")
	}
	calls := fakeRunTool(t, nil)
	_, code, out := buildFixtureBundle(t, "Adhoc")
	if code != -1 {
		t.Fatalf("desktop build exited %d", code)
	}
	assertCalls(t, *calls, []fakeToolCall{{
		"codesign", []string{"--force", "--deep", "--sign", "-", filepath.Join(out, "Adhoc.app")},
	}})
}

func TestDesktopBuildIdentitySigningArgv(t *testing.T) {
	identity := "Developer ID Application: Example Corp (ABCD12345)"
	calls := fakeRunTool(t, nil)
	_, code, out := buildFixtureBundle(t, "Ident", "--sign", identity)
	if code != -1 {
		t.Fatalf("desktop build exited %d", code)
	}
	assertCalls(t, *calls, []fakeToolCall{{
		"codesign", []string{"--force", "--deep", "--sign", identity, filepath.Join(out, "Ident.app")},
	}})
}

func TestDesktopBuildNotarizeRefusesAdhoc(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"no sign", []string{"--notarize"}},
		{"sign dash", []string{"--notarize", "--sign", "-"}},
		{"no-sign flag", []string{"--notarize", "--no-sign"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := fakeRunTool(t, nil)
			wd := t.TempDir()
			oldWD, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chdir(wd); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chdir(oldWD) })

			args := append([]string{"--id", "dev.gofastr.x", "--pkg", "./does/not/exist"}, tc.args...)
			var code int
			out := covT_capStdout(t, func() {
				code = covT_capExit(t, func() { runDesktopBuild(args) })
			})
			if code != 1 {
				t.Fatalf("exit = %d, want 1:\n%s", code, out)
			}
			if !strings.Contains(out, "--notarize requires --sign") {
				t.Fatalf("error does not name the rule:\n%s", out)
			}
			if len(*calls) != 0 {
				t.Fatalf("tools ran before the refusal: %v", *calls)
			}
			if _, err := os.Stat(filepath.Join(wd, "dist")); err == nil {
				t.Fatal("dist/ created before the refusal")
			}
		})
	}
}

func TestDesktopBuildNotarizeFlagParsing(t *testing.T) {
	f := parseDesktopBuildFlags([]string{
		"--id=dev.gofastr.x", "--notarize",
		"--notary-profile", "prod",
		"--entitlements", "ent.plist",
		"--sign", "Developer ID Application: Example",
	})
	if !f.notarize || f.notaryProfile != "prod" || f.entitlements != "ent.plist" {
		t.Fatalf("notarize flags = %+v", f)
	}
	f = parseDesktopBuildFlags([]string{
		"--id=dev.gofastr.x", "--notarize", "--notary-profile=ci", "--entitlements=e.plist",
	})
	if f.notaryProfile != "ci" || f.entitlements != "e.plist" {
		t.Fatalf("inline notarize flags = %+v", f)
	}
	def := parseDesktopBuildFlags([]string{"--id=dev.gofastr.x"})
	if def.notarize || def.notaryProfile != "gofastr" || def.entitlements != "" {
		t.Fatalf("defaults = %+v (want notaryProfile gofastr)", def)
	}
}

func TestNotarizeFlagsRefusalIsNamedError(t *testing.T) {
	f := parseDesktopBuildFlags([]string{"--id=dev.gofastr.x", "--notarize"})
	if err := validateNotarizeFlags(f); !errors.Is(err, errNotarizeRequiresIdentity) {
		t.Fatalf("validateNotarizeFlags = %v, want errNotarizeRequiresIdentity", err)
	}
	f = parseDesktopBuildFlags([]string{"--id=dev.gofastr.x", "--notarize", "--sign", "-"})
	if err := validateNotarizeFlags(f); !errors.Is(err, errNotarizeRequiresIdentity) {
		t.Fatalf("--sign - accepted: %v", err)
	}
	f = parseDesktopBuildFlags([]string{"--id=dev.gofastr.x", "--notarize", "--no-sign"})
	if err := validateNotarizeFlags(f); !errors.Is(err, errNotarizeRequiresIdentity) {
		t.Fatalf("--no-sign accepted: %v", err)
	}
	f = parseDesktopBuildFlags([]string{
		"--id=dev.gofastr.x", "--notarize", "--sign", "Developer ID Application: Example",
	})
	if err := validateNotarizeFlags(f); err != nil {
		t.Fatalf("real identity refused: %v", err)
	}
	f = parseDesktopBuildFlags([]string{"--id=dev.gofastr.x", "--sign", "anything"})
	if err := validateNotarizeFlags(f); err != nil {
		t.Fatalf("no notarize refused: %v", err)
	}
}

// notarizeFakeBundle prepares a fake bundle and the parsed flags for a
// direct pipeline run (no go build involved).
func notarizeFakeBundle(t *testing.T, flagArgs ...string) (desktopBuildFlags, string, string) {
	t.Helper()
	dir := t.TempDir()
	appDir := writeFakeBundle(t, dir)
	f := parseDesktopBuildFlags(flagArgs)
	f.out = dir
	return f, dir, appDir
}

func TestNotarizePipelineArgv(t *testing.T) {
	identity := "Developer ID Application: Example Corp (ABCD12345)"
	f, dir, appDir := notarizeFakeBundle(t, "--notarize", "--sign", identity)

	var plistPath, plistContent string
	calls := fakeRunTool(t, func(name string, args []string) (string, error) {
		if name == "codesign" {
			if i := slices.Index(args, "--entitlements"); i >= 0 && i+1 < len(args) {
				plistPath = args[i+1]
				b, err := os.ReadFile(plistPath)
				if err != nil {
					t.Errorf("read generated entitlements: %v", err)
				} else {
					plistContent = string(b)
				}
			}
		}
		return "", nil
	})
	if err := notarizeDesktopBundle(f, "Notes", appDir); err != nil {
		t.Fatalf("notarizeDesktopBundle: %v", err)
	}

	zipPath := filepath.Join(dir, "Notes.zip")
	assertCalls(t, *calls, []fakeToolCall{
		{"codesign", []string{"--force", "--deep", "--sign", identity,
			"--options", "runtime", "--timestamp", "--entitlements", plistPath, appDir}},
		{"xcrun", []string{"notarytool", "submit", zipPath, "--keychain-profile", "gofastr", "--wait"}},
		{"xcrun", []string{"stapler", "staple", appDir}},
	})

	if !strings.Contains(plistContent, "<plist") || !strings.Contains(plistContent, "<dict/>") {
		t.Fatalf("generated entitlements = %q, want an empty-dict plist", plistContent)
	}
	if _, err := os.Stat(plistPath); !os.IsNotExist(err) {
		t.Fatalf("generated entitlements still on disk after the pipeline: %v", err)
	}

	// What the pipeline leaves behind is the re-zip after stapling: a
	// valid archive rooted at the bundle's base name.
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatalf("open the stapled zip: %v", err)
	}
	defer r.Close()
	if len(r.File) == 0 || r.File[0].Name != "Notes.app/" {
		t.Fatalf("stapled zip is not rooted at the bundle: first entry %q", r.File[0].Name)
	}
}

func TestNotarizeProfileAndEntitlements(t *testing.T) {
	identity := "Developer ID Application: Example Corp (ABCD12345)"
	f, dir, appDir := notarizeFakeBundle(t,
		"--notarize", "--sign", identity,
		"--notary-profile", "team-profile")
	// An absolute path, the way a caller names a real file; the flag
	// value reaches codesign verbatim.
	ent := filepath.Join(dir, "my-entitlements.plist")
	f.entitlements = ent
	if err := os.WriteFile(ent, []byte("<plist version=\"1.0\"><dict/></plist>"), 0o600); err != nil {
		t.Fatal(err)
	}

	calls := fakeRunTool(t, nil)
	if err := notarizeDesktopBundle(f, "Notes", appDir); err != nil {
		t.Fatalf("notarizeDesktopBundle: %v", err)
	}
	zipPath := filepath.Join(dir, "Notes.zip")
	assertCalls(t, *calls, []fakeToolCall{
		{"codesign", []string{"--force", "--deep", "--sign", identity,
			"--options", "runtime", "--timestamp", "--entitlements", ent, appDir}},
		{"xcrun", []string{"notarytool", "submit", zipPath, "--keychain-profile", "team-profile", "--wait"}},
		{"xcrun", []string{"stapler", "staple", appDir}},
	})
	// A caller-provided plist is the caller's file: the pipeline must
	// not delete it.
	if _, err := os.Stat(ent); err != nil {
		t.Fatalf("caller-provided entitlements removed: %v", err)
	}
}

func TestNotarizeSubmitFailureMessage(t *testing.T) {
	f, _, appDir := notarizeFakeBundle(t, "--notarize", "--sign", "Developer ID Application: X")
	calls := fakeRunTool(t, func(name string, args []string) (string, error) {
		if name == "xcrun" && args[0] == "notarytool" {
			return "id: abc-123\nstatus: Invalid\nmessage: binary is not signed with a valid Developer ID certificate",
				errors.New("exit status 69")
		}
		return "", nil
	})
	err := notarizeDesktopBundle(f, "Notes", appDir)
	if err == nil {
		t.Fatal("a failed notary submission did not fail the pipeline")
	}
	msg := err.Error()
	for _, want := range []string{"notarytool submit", "status: Invalid", "exit status 69"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error missing %q:\n%s", want, msg)
		}
	}
	if len(*calls) != 2 {
		t.Fatalf("stapler ran after a failed submission: %v", *calls)
	}
}

func TestNotarizeStapleFailure(t *testing.T) {
	f, _, appDir := notarizeFakeBundle(t, "--notarize", "--sign", "Developer ID Application: X")
	fakeRunTool(t, func(name string, args []string) (string, error) {
		if name == "xcrun" && args[0] == "stapler" {
			return "The staple and validate action worked on the wrong app?",
				errors.New("exit status 65")
		}
		return "", nil
	})
	err := notarizeDesktopBundle(f, "Notes", appDir)
	if err == nil {
		t.Fatal("a failed staple did not fail the pipeline")
	}
	for _, want := range []string{"stapler staple", "exit status 65"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q:\n%s", want, err)
		}
	}
}

func TestNotarizeSignFailure(t *testing.T) {
	f, dir, appDir := notarizeFakeBundle(t, "--notarize", "--sign", "Developer ID Application: X")
	fakeRunTool(t, func(name string, args []string) (string, error) {
		if name == "codesign" {
			return "errSecInternalComponent", errors.New("exit status 1")
		}
		return "", nil
	})
	err := notarizeDesktopBundle(f, "Notes", appDir)
	if err == nil {
		t.Fatal("a failed hardened-runtime signing did not fail the pipeline")
	}
	for _, want := range []string{"signing", "errSecInternalComponent"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q:\n%s", want, err)
		}
	}
	if _, statErr := os.Stat(filepath.Join(dir, "Notes.zip")); statErr == nil {
		t.Error("a zip was written despite the signing failure")
	}
}

func TestNotarizeZipFailureNamesSymlink(t *testing.T) {
	f, _, appDir := notarizeFakeBundle(t, "--notarize", "--sign", "Developer ID Application: X")
	if err := os.Symlink(
		filepath.Join(appDir, "Contents", "Info.plist"),
		filepath.Join(appDir, "Contents", "MacOS", "lnk"),
	); err != nil {
		t.Skipf("host cannot create symlinks: %v", err)
	}
	calls := fakeRunTool(t, nil)
	err := notarizeDesktopBundle(f, "Notes", appDir)
	if err == nil {
		t.Fatal("a bundle with a symlink was submitted anyway")
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("error does not name the symlink:\n%s", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("tools ran past the zip refusal: %v", *calls)
	}
}

func TestDesktopBuildNotarizeEndToEnd(t *testing.T) {
	identity := "Developer ID Application: Example Corp (ABCD12345)"
	var plist string
	calls := fakeRunTool(t, func(name string, args []string) (string, error) {
		if name == "codesign" {
			if i := slices.Index(args, "--entitlements"); i >= 0 && i+1 < len(args) {
				plist = args[i+1]
			}
		}
		return "", nil
	})
	_, code, out := buildFixtureBundle(t, "Notar", "--notarize", "--sign", identity)
	if code != -1 {
		t.Fatalf("desktop build exited %d", code)
	}
	appDir := filepath.Join(out, "Notar.app")
	zipPath := filepath.Join(out, "Notar.zip")
	assertCalls(t, *calls, []fakeToolCall{
		{"codesign", []string{"--force", "--deep", "--sign", identity,
			"--options", "runtime", "--timestamp", "--entitlements", plist, appDir}},
		{"xcrun", []string{"notarytool", "submit", zipPath, "--keychain-profile", "gofastr", "--wait"}},
		{"xcrun", []string{"stapler", "staple", appDir}},
	})
	if _, err := os.Stat(zipPath); err != nil {
		t.Fatalf("no distributed zip next to the bundle: %v", err)
	}
}

func TestDesktopBuildNotarizeFailsBuild(t *testing.T) {
	identity := "Developer ID Application: Example Corp (ABCD12345)"
	fakeRunTool(t, func(name string, args []string) (string, error) {
		if name == "xcrun" {
			return "status: Rejected", errors.New("exit status 69")
		}
		return "", nil
	})
	out, code, _ := buildFixtureBundle(t, "FailNot", "--notarize", "--sign", identity)
	if code != 1 {
		t.Fatalf("exit = %d, want 1:\n%s", code, out)
	}
	if !strings.Contains(out, "status: Rejected") {
		t.Fatalf("tool output missing from the failure message:\n%s", out)
	}
}
