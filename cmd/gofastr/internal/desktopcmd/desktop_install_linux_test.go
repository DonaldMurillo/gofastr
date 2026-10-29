package desktopcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func linuxInstallFixture(t *testing.T, scheme string) (string, linuxBundleManifest) {
	t.Helper()
	// A real small native ELF gives the same header checks as an app. printf
	// lets launcher tests observe each unchanged argument without a native UI.
	executable, err := exec.LookPath("printf")
	if err != nil {
		t.Fatal(err)
	}
	flags := desktopBuildFlags{id: "dev.example.install", version: "1.2.3", scheme: scheme}
	entries, err := linuxBundleEntries(flags, `Notes 100% "$HOME"`, runtime.GOARCH, executable, testLinuxPNG(t))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, entry := range entries {
		data := entry.data
		if entry.source != "" {
			data, err = os.ReadFile(entry.source)
			if err != nil {
				t.Fatal(err)
			}
		}
		name := filepath.Join(dir, filepath.FromSlash(entry.name))
		if err = os.MkdirAll(filepath.Dir(name), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(name, data, os.FileMode(entry.mode)); err != nil {
			t.Fatal(err)
		}
	}
	var manifest linuxBundleManifest
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	return dir, manifest
}
func linuxInstallTestHome(t *testing.T) linuxInstallPaths {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", filepath.Join(dir, `home space '$ & % quote"`))
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, `other data % '$ "`))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("XDG_DATA_DIRS", filepath.Join(dir, "system-data"))
	t.Setenv("XDG_CONFIG_DIRS", filepath.Join(dir, "system-config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
	p, err := linuxUserInstallPaths()
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func writeLinuxTestManifest(t *testing.T, dir string, m linuxBundleManifest) {
	t.Helper()
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}
func TestLinuxInstallRoundTrip(t *testing.T) {
	paths := linuxInstallTestHome(t)
	source, manifest := linuxInstallFixture(t, "gofastr-install-test")
	config := os.Getenv("XDG_CONFIG_HOME")
	if err := os.MkdirAll(config, 0700); err != nil {
		t.Fatal(err)
	}
	defaults := []byte("[Default Applications]\nx-scheme-handler/gofastr-install-test=other.desktop;\n")
	defaultsPath := filepath.Join(config, "mimeapps.list")
	if err := os.WriteFile(defaultsPath, defaults, 0600); err != nil {
		t.Fatal(err)
	}
	if err := installLinuxBundle(source); err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(paths.apps, manifest.ID)
	launcher := filepath.Join(paths.bin, manifest.ID)
	desktopFile := filepath.Join(paths.applications, manifest.ID+".desktop")
	for _, test := range []struct {
		path string
		mode os.FileMode
	}{{launcher, 0700}, {desktopFile, 0600}, {filepath.Join(installed, "bin", manifest.ID), 0700}, {filepath.Join(installed, linuxInstallReceiptName), 0600}} {
		st, err := os.Stat(test.path)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != test.mode {
			t.Errorf("%s mode %o, want %o", test.path, st.Mode().Perm(), test.mode)
		}
	}
	args := []string{"%s\\000", "", "route with spaces", "'\"$HOME; %u ", "gofastr-install-test://a?b=%20&c=x"}
	output, err := exec.Command(launcher, args...).Output()
	if err != nil {
		t.Fatal(err)
	}
	want := []byte(strings.Join(args[1:], "\x00") + "\x00")
	if !bytes.Equal(output, want) {
		t.Fatalf("launcher arguments %q, want %q", output, want)
	}
	entry, err := os.ReadFile(desktopFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(entry, []byte("MimeType=x-scheme-handler/gofastr-install-test;\n")) || !bytes.Contains(entry, []byte(" %u\n")) {
		t.Fatalf("scheme handler missing:\n%s", entry)
	}
	if bytes.Contains(entry, []byte("DBusActivatable")) || bytes.Contains(entry, []byte("@BUNDLE_ROOT@")) {
		t.Fatalf("unusable installed entry:\n%s", entry)
	}
	if validator, err := exec.LookPath("desktop-file-validate"); err == nil {
		if out, err := exec.Command(validator, desktopFile).CombinedOutput(); err != nil {
			t.Fatalf("desktop-file-validate: %v\n%s", err, out)
		}
	}
	if _, err := exec.LookPath("update-desktop-database"); err == nil {
		cache, err := os.ReadFile(filepath.Join(paths.applications, "mimeinfo.cache"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(cache, []byte("x-scheme-handler/gofastr-install-test="+manifest.ID+".desktop;")) {
			t.Fatalf("handler not in real MIME cache: %s", cache)
		}
	}
	if err := installLinuxBundle(source); err == nil {
		t.Fatal("in-place replacement accepted")
	}
	unrelated := filepath.Join(paths.applications, "unrelated.desktop")
	if err := os.WriteFile(unrelated, []byte("unrelated"), 0600); err != nil {
		t.Fatal(err)
	}
	appData := filepath.Join(config, manifest.ID, "session.txt")
	if err := os.MkdirAll(filepath.Dir(appData), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(appData, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := uninstallLinuxBundle(manifest.ID); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{installed, launcher, desktopFile} {
		if _, err := os.Lstat(name); !os.IsNotExist(err) {
			t.Fatalf("uninstall retained %s: %v", name, err)
		}
	}
	for _, name := range []string{unrelated, appData, defaultsPath} {
		if _, err := os.Stat(name); err != nil {
			t.Fatal(err)
		}
	}
	after, err := os.ReadFile(defaultsPath)
	if err != nil || !bytes.Equal(after, defaults) {
		t.Fatalf("default handler preferences changed: %v %s", err, after)
	}
	if err := installLinuxBundle(source); err != nil {
		t.Fatal(err)
	}
	if err := uninstallLinuxBundle(manifest.ID); err != nil {
		t.Fatal(err)
	}
}
func TestLinuxInstallManifestRefusals(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*linuxBundleManifest)
	}{
		{"schema", func(m *linuxBundleManifest) { m.SchemaVersion = 2 }},
		{"traversal-id", func(m *linuxBundleManifest) { m.ID = "../../escape" }},
		{"platform", func(m *linuxBundleManifest) { m.Platform = "darwin-arm64" }},
		{"foreign-architecture", func(m *linuxBundleManifest) {
			m.Platform = "linux-arm64"
			if runtime.GOARCH == "arm64" {
				m.Platform = "linux-amd64"
			}
		}},
		{"entrypoint", func(m *linuxBundleManifest) { m.EntryPoint = "../elsewhere" }},
		{"scheme", func(m *linuxBundleManifest) { m.EmbeddedScheme = "a;\nExec=bad" }},
		{"missing", func(m *linuxBundleManifest) { m.Files = m.Files[1:] }},
		{"duplicate", func(m *linuxBundleManifest) { m.Files[1] = m.Files[0] }},
		{"entry-traversal", func(m *linuxBundleManifest) { m.Files[0].Path = "../escape" }},
		{"mode", func(m *linuxBundleManifest) { m.Files[0].Mode = "0777" }},
		{"size", func(m *linuxBundleManifest) { m.Files[0].Size = 1 << 40 }},
		{"negative-size", func(m *linuxBundleManifest) { m.Files[0].Size = -1 }},
		{"hash", func(m *linuxBundleManifest) { m.Files[0].SHA256 = strings.Repeat("0", 64) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			paths := linuxInstallTestHome(t)
			dir, m := linuxInstallFixture(t, "")
			test.mutate(&m)
			writeLinuxTestManifest(t, dir, m)
			if err := installLinuxBundle(dir); err == nil {
				t.Fatal("invalid manifest accepted")
			}
			if _, err := os.Lstat(paths.data); !os.IsNotExist(err) {
				t.Fatalf("invalid bundle created installation directory: %v", err)
			}
		})
	}
}
func TestLinuxInstallPayloadRefusals(t *testing.T) {
	for _, kind := range []string{"extra", "symlink", "fifo", "truncated", "non-ELF", "wrong-ELF-machine", "wrong-ELF-OS", "ELF-no-entry", "invalid-icon", "extra-directory", "manifest-symlink", "duplicate-json", "casefold-json", "nested-casefold-json"} {
		t.Run(kind, func(t *testing.T) {
			paths := linuxInstallTestHome(t)
			dir, m := linuxInstallFixture(t, "")
			target := filepath.Join(dir, "README.txt")
			switch kind {
			case "extra":
				if err := os.WriteFile(filepath.Join(dir, "unrelated"), []byte("retain"), 0600); err != nil {
					t.Fatal(err)
				}
			case "extra-directory":
				if err := os.Mkdir(filepath.Join(dir, "other"), 0700); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Remove(target); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("launch.sh", target); err != nil {
					t.Fatal(err)
				}
			case "fifo":
				if err := os.Remove(target); err != nil {
					t.Fatal(err)
				}
				if err := syscall.Mkfifo(target, 0600); err != nil {
					t.Fatal(err)
				}
			case "truncated":
				if err := os.WriteFile(target, []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			case "manifest-symlink":
				old := filepath.Join(dir, "manifest.json")
				if err := os.Rename(old, old+".old"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("manifest.json.old", old); err != nil {
					t.Fatal(err)
				}
			case "nested-casefold-json":
				name := filepath.Join(dir, "manifest.json")
				data, err := os.ReadFile(name)
				if err != nil {
					t.Fatal(err)
				}
				data = bytes.Replace(data, []byte(`"path"`), []byte(`"PATH"`), 1)
				if err = os.WriteFile(name, data, 0600); err != nil {
					t.Fatal(err)
				}
			case "duplicate-json", "casefold-json":
				old, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
				if err != nil {
					t.Fatal(err)
				}
				prefix := `{"schema_version":1,`
				if kind == "casefold-json" {
					prefix = `{"Schema_version":1,`
				}
				if err = os.WriteFile(filepath.Join(dir, "manifest.json"), append([]byte(prefix), old[1:]...), 0600); err != nil {
					t.Fatal(err)
				}
			default:
				name := "bin/" + m.ID
				replacement := []byte("not an ELF")
				if strings.HasPrefix(kind, "wrong-ELF-") || kind == "ELF-no-entry" {
					var err error
					replacement, err = os.ReadFile(filepath.Join(dir, name))
					if err != nil {
						t.Fatal(err)
					}
					switch kind {
					case "wrong-ELF-machine":
						replacement[18] = 0
						replacement[19] = 0
					case "wrong-ELF-OS":
						replacement[7] = 9
					case "ELF-no-entry":
						clear(replacement[24:32])
					}
				}
				if kind == "invalid-icon" {
					name = "icon.png"
					replacement = []byte("not a PNG")
				}
				if err := os.WriteFile(filepath.Join(dir, name), replacement, 0700); err != nil {
					t.Fatal(err)
				}
				for i := range m.Files {
					if m.Files[i].Path == name {
						m.Files[i].Size = int64(len(replacement))
						m.Files[i].SHA256 = linuxInstallDigest(replacement)
					}
				}
				writeLinuxTestManifest(t, dir, m)
			}
			start := time.Now()
			if err := installLinuxBundle(dir); err == nil {
				t.Fatal("invalid payload accepted")
			}
			if time.Since(start) > time.Second {
				t.Fatal("special input did not fail promptly")
			}
			if _, err := os.Lstat(paths.data); !os.IsNotExist(err) {
				t.Fatalf("invalid payload changed destinations: %v", err)
			}
		})
	}
}
func TestLinuxUninstallRetainsChanges(t *testing.T) {
	for _, kind := range []string{"payload", "extra", "launcher", "entry", "receipt", "symlink", "missing", "permissions"} {
		t.Run(kind, func(t *testing.T) {
			paths := linuxInstallTestHome(t)
			source, m := linuxInstallFixture(t, "")
			if err := installLinuxBundle(source); err != nil {
				t.Fatal(err)
			}
			installed := filepath.Join(paths.apps, m.ID)
			target := filepath.Join(installed, "README.txt")
			original, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "extra":
				target = filepath.Join(installed, "unrelated.txt")
			case "launcher":
				target = filepath.Join(paths.bin, m.ID)
			case "entry":
				target = filepath.Join(paths.applications, m.ID+".desktop")
			case "receipt":
				target = filepath.Join(installed, linuxInstallReceiptName)
			}
			if kind == "symlink" || kind == "missing" {
				if err := os.Remove(target); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "symlink" {
				if err := os.Symlink("launch.sh", target); err != nil {
					t.Fatal(err)
				}
			} else if kind != "missing" && kind != "permissions" {
				if err := os.WriteFile(target, []byte("user change"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "permissions" {
				if err := os.Chmod(target, 0644); err != nil {
					t.Fatal(err)
				}
			}
			if err := uninstallLinuxBundle(m.ID); err == nil {
				t.Fatal("changed installation removed")
			}
			for _, name := range []string{installed, filepath.Join(paths.bin, m.ID), filepath.Join(paths.applications, m.ID+".desktop")} {
				if _, err := os.Lstat(name); err != nil {
					t.Fatalf("refusal removed %s: %v", name, err)
				}
			}
			if kind != "symlink" && kind != "missing" {
				got, err := os.ReadFile(target)
				want := []byte("user change")
				if kind == "permissions" {
					want = original
				}
				if err != nil || !bytes.Equal(got, want) {
					t.Fatalf("user change lost: %v %q", err, got)
				}
				if kind == "permissions" {
					st, err := os.Stat(target)
					if err != nil || st.Mode().Perm() != 0644 {
						t.Fatalf("user mode changed on refusal: %v %v", st, err)
					}
				}
			}
		})
	}
}
func TestLinuxInstallPathRefusals(t *testing.T) {
	for _, kind := range []string{"symlink-data", "symlink-bin", "collision", "unsafe-home", "unsafe-data", "shared-applications"} {
		t.Run(kind, func(t *testing.T) {
			paths := linuxInstallTestHome(t)
			source, m := linuxInstallFixture(t, "")
			switch kind {
			case "symlink-data":
				if err := os.Symlink(t.TempDir(), paths.data); err != nil {
					t.Fatal(err)
				}
			case "symlink-bin":
				if err := os.MkdirAll(filepath.Dir(paths.bin), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(t.TempDir(), paths.bin); err != nil {
					t.Fatal(err)
				}
			case "collision":
				if err := os.MkdirAll(paths.bin, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(paths.bin, m.ID), []byte("existing"), 0600); err != nil {
					t.Fatal(err)
				}
			case "unsafe-home":
				t.Setenv("HOME", paths.bin+"=bad")
			case "unsafe-data":
				t.Setenv("XDG_DATA_HOME", paths.data+"\nExec=bad")
			case "shared-applications":
				if err := os.MkdirAll(paths.applications, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(paths.applications, 0777); err != nil {
					t.Fatal(err)
				}
			}
			if err := installLinuxBundle(source); err == nil {
				t.Fatal("unsafe destination accepted")
			}
			if _, err := os.Lstat(filepath.Join(paths.apps, m.ID)); !os.IsNotExist(err) {
				t.Fatalf("failed install left bundle: %v", err)
			}
			if kind == "collision" {
				data, err := os.ReadFile(filepath.Join(paths.bin, m.ID))
				if err != nil || string(data) != "existing" {
					t.Fatalf("collision overwritten: %v %q", err, data)
				}
			}
		})
	}
}
func TestLinuxDesktopInstallCommands(t *testing.T) {
	for _, args := range [][]string{nil, {"--bundle"}, {"--bundle="}, {"--bundle=a", "--bundle=b"}, {"--other=x"}, {"--bundle=x", "positional"}} {
		if _, err := desktopInstallArgument(args, "--bundle"); err == nil {
			t.Errorf("bad flags accepted: %q", args)
		}
	}
	for _, target := range []string{"windows", "darwin"} {
		output := clitest.CovT_capStdout(t, func() {
			if code := clitest.CovT_capExit(t, func() { runDesktopInstall([]string{"--bundle=missing"}, false, target) }); code != 1 {
				t.Errorf("unsupported %s exit %d", target, code)
			}
		})
		if !strings.Contains(output, "Linux only") {
			t.Fatal(output)
		}
	}
}
func TestLinuxDesktopEntryWithoutScheme(t *testing.T) {
	data, err := linuxInstalledDesktopEntry(linuxBundleManifest{ID: "dev.example.notes", Name: "Notes"}, "/home/me/app", "/home/me/.local/bin/dev.example.notes")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"%u", "MimeType=", "DBusActivatable"} {
		if bytes.Contains(data, []byte(bad)) {
			t.Fatalf("unembedded entry claims %s", bad)
		}
	}
}
func TestLinuxInstallerRealDesktopExec(t *testing.T) {
	gio, err := exec.LookPath("gio")
	if err != nil {
		t.Skip("GIO command not installed")
	}
	paths := linuxInstallTestHome(t)
	source, m := linuxInstallFixture(t, "gofastr-install-test")
	echo, err := exec.LookPath("echo")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(echo)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(source, "bin", m.ID), payload, 0700); err != nil {
		t.Fatal(err)
	}
	for i := range m.Files {
		if m.Files[i].Path == "bin/"+m.ID {
			m.Files[i].Size = int64(len(payload))
			m.Files[i].SHA256 = linuxInstallDigest(payload)
		}
	}
	writeLinuxTestManifest(t, source, m)
	if err = installLinuxBundle(source); err != nil {
		t.Fatal(err)
	}
	// Real GIO parsing, MIME lookup, and handler resolution, not our own parser.
	// echo prints the literal URL; there is no shell -c or app/bus dependency.
	uri := "gofastr-install-test://route/space%20here?literal=$HOME&quote='&pct=%25"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, gio, "launch", filepath.Join(paths.applications, m.ID+".desktop"), uri)
	cmd.WaitDelay = time.Second
	out, err := cmd.CombinedOutput()
	if err != nil {
		entry, _ := os.ReadFile(filepath.Join(paths.applications, m.ID+".desktop"))
		t.Fatalf("gio launch: %v\n%s\n%s", err, out, entry)
	}
	exe := filepath.Join(paths.apps, m.ID, "bin", m.ID)
	want, wantErr := exec.Command(exe, uri).CombinedOutput()
	if wantErr != nil {
		t.Fatalf("fixture: %v %s", wantErr, want)
	}
	if !bytes.Equal(out, want) {
		t.Fatalf("real desktop activation argv differs:\ngot %q\nwant %q", out, want)
	}
	if _, err = exec.LookPath("update-desktop-database"); err != nil {
		t.Skip("handler lookup needs desktop-file-utils")
	}
	lookup := exec.CommandContext(ctx, gio, "mime", "x-scheme-handler/gofastr-install-test")
	registered, err := lookup.CombinedOutput()
	if err != nil || !bytes.Contains(registered, []byte(m.ID+".desktop")) {
		t.Fatalf("real GIO handler lookup: %v %s", err, registered)
	}
	open := exec.CommandContext(ctx, gio, "open", uri)
	open.WaitDelay = time.Second
	opened, err := open.CombinedOutput()
	if err != nil || !bytes.Equal(opened, want) {
		t.Fatalf("real handler activation: %v; got %q want %q", err, opened, want)
	}
}
func TestLinuxInstallCopyRollback(t *testing.T) {
	paths := linuxInstallTestHome(t)
	source, m := linuxInstallFixture(t, "")
	bundle, err := openLinuxInstallBundle(source, false, true)
	if err != nil {
		t.Fatal(err)
	}
	defer bundle.root.Close()
	// Mutate after preflight: copying must recheck integrity and roll back every
	// earlier copied file and the reserved app directory.
	if err = os.WriteFile(filepath.Join(source, "launch.sh"), []byte("changed after validation"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = installVerifiedLinuxBundle(bundle, paths); err == nil || !strings.Contains(err.Error(), "changed while copying") {
		t.Fatalf("mutation not caught during copy: %v", err)
	}
	for _, name := range []string{filepath.Join(paths.apps, m.ID), filepath.Join(paths.bin, m.ID), filepath.Join(paths.applications, m.ID+".desktop")} {
		if _, err = os.Lstat(name); !os.IsNotExist(err) {
			t.Fatalf("rollback left %s: %v", name, err)
		}
	}
}
func TestLinuxInstallRelativeXDG(t *testing.T) {
	paths := linuxInstallTestHome(t)
	t.Setenv("XDG_DATA_HOME", "relative/not-used")
	actual, err := linuxUserInstallPaths()
	if err != nil {
		t.Fatal(err)
	}
	if actual.data != filepath.Join(os.Getenv("HOME"), ".local", "share") || actual.bin != paths.bin {
		t.Fatalf("relative XDG handling: %+v", actual)
	}
}
func TestLinuxInstallCacheFailureWarning(t *testing.T) {
	paths := linuxInstallTestHome(t)
	source, m := linuxInstallFixture(t, "")
	t.Setenv("PATH", t.TempDir())
	output := clitest.CovT_capStdout(t, func() {
		if err := installLinuxBundle(source); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, "MIME cache refresh did not complete") {
		t.Fatal(output)
	}
	if _, err := os.Stat(filepath.Join(paths.applications, m.ID+".desktop")); err != nil {
		t.Fatal(err)
	}
}
func TestLinuxInstallDirectoryOwnership(t *testing.T) {
	uid := uint32(os.Geteuid())
	foreign := uid + 1
	for _, test := range []struct {
		name       string
		owner      uint32
		mode       os.FileMode
		leaf, want bool
	}{
		{"own ancestor", uid, 0755, false, true},
		{"root ancestor", 0, 0755, false, true},
		{"root sticky temp", 0, os.ModeSticky | 0777, false, true},
		{"own sticky temp", uid, os.ModeSticky | 0777, false, true},
		{"foreign 0755", foreign, 0755, false, false},
		{"foreign sticky", foreign, os.ModeSticky | 0777, false, false},
		{"root writable", 0, 0777, false, false},
		{"own leaf", uid, 0700, true, true},
		{"own writable leaf", uid, 0777, true, false},
		{"foreign leaf", foreign, 0700, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := linuxInstallDirectoryPolicy(test.owner, test.mode, test.leaf); got != test.want {
				t.Fatalf("policy=%v want %v", got, test.want)
			}
		})
	}
}
