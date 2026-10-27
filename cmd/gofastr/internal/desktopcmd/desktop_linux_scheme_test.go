package desktopcmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinuxDesktopLinkerFlags(t *testing.T) {
	flags, err := linuxDesktopLinkerFlags("")
	if err != nil || flags != "-s -w" {
		t.Fatalf("default flags = %q, %v", flags, err)
	}
	for _, scheme := range []string{"a", "notes+test-v1.2", strings.Repeat("a", 32)} {
		flags, err := linuxDesktopLinkerFlags(scheme)
		want := "-s -w -X github.com/DonaldMurillo/gofastr/battery/desktop.builtDeepLinkScheme=" + scheme
		if err != nil || flags != want {
			t.Errorf("scheme %q: flags = %q, %v; want %q", scheme, flags, err, want)
		}
	}
}

func TestLinuxInvalidSchemeBeforeOutput(t *testing.T) {
	t.Setenv("GOARCH", "amd64")
	for _, scheme := range []string{"Notes", "1notes", "note_s", "notes -X main.other=x", "notes\x00", "notes://", strings.Repeat("a", 33)} {
		for _, builder := range []string{"executable", "archive", "direct archive", "entries"} {
			t.Run(builder+"/"+scheme, func(t *testing.T) {
				out := filepath.Join(t.TempDir(), "not-created")
				f := desktopBuildFlags{id: "dev.example.scheme", version: "1.0.0", pkg: "./missing", out: out, scheme: scheme}
				var err error
				switch builder {
				case "archive":
					f.format = "tar.gz"
					err = buildLinuxDesktop(f, "Notes")
				case "direct archive":
					err = buildLinuxDesktopArchive(f, "Notes", "amd64")
				case "entries":
					_, err = linuxBundleEntries(f, "Notes", "amd64", "./missing", nil)
				default:
					err = buildLinuxDesktop(f, "Notes")
				}
				if err == nil || !strings.Contains(err.Error(), "--scheme") {
					t.Fatalf("invalid scheme error = %v", err)
				}
				if _, err := os.Stat(out); !os.IsNotExist(err) {
					t.Fatalf("invalid scheme changed output: %v", err)
				}
			})
		}
	}
}

func TestLinuxArchiveEmbeddedSchemeMetadata(t *testing.T) {
	f, executable, _ := linuxArchiveFixture(t)
	for _, scheme := range []string{"", "notes+v1"} {
		t.Run(scheme, func(t *testing.T) {
			f.scheme = scheme
			entries, err := linuxBundleEntries(f, "Notes", "amd64", executable, testLinuxPNG(t))
			if err != nil {
				t.Fatal(err)
			}
			contents := make(map[string][]byte)
			for _, entry := range entries {
				contents[entry.name] = entry.data
			}
			var manifest linuxBundleManifest
			if err := json.Unmarshal(contents["manifest.json"], &manifest); err != nil {
				t.Fatal(err)
			}
			if manifest.EmbeddedScheme != scheme {
				t.Fatalf("manifest embedded scheme = %q, want %q", manifest.EmbeddedScheme, scheme)
			}
			if scheme == "" {
				if strings.Contains(string(contents["manifest.json"]), "embedded_scheme") || len(manifest.Runtime) != 4 {
					t.Fatal("default artifact unexpectedly opts into activation")
				}
			} else {
				if len(manifest.Runtime) != 5 || !strings.Contains(manifest.Runtime[4], "local Unix D-Bus") {
					t.Fatalf("activation dependency missing: %q", manifest.Runtime)
				}
				readme := string(contents["README.txt"])
				for _, want := range []string{scheme + "://route", "DBUS_SESSION_BUS_ADDRESS", "SAME session bus", "app stays open", "no browser/OS protocol association"} {
					if !strings.Contains(strings.ToLower(readme), strings.ToLower(want)) {
						t.Errorf("activation README omits %q", want)
					}
				}
			}
			entry := string(contents[f.id+".desktop.in"])
			for _, unsupported := range []string{"MimeType=", "DBusActivatable=", "%u", "%U", "x-scheme-handler/"} {
				if strings.Contains(entry, unsupported) {
					t.Errorf("inert template claims OS activation through %q", unsupported)
				}
			}
		})
	}
}
