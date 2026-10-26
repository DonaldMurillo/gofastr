package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sourceFixture(t *testing.T) (string, sourceManifest) {
	t.Helper()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS("../..")); err != nil {
		t.Fatal(err)
	}
	m, _, err := loadSources(dir)
	if err != nil {
		t.Fatal(err)
	}
	return dir, m
}

func writeFixtureManifest(t *testing.T, dir string, m sourceManifest) {
	t.Helper()
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sources.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestGenerationRefusesUnverifiedSourceChanges(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*testing.T, string, *sourceManifest)
		want   string
	}{
		{"edited SVG", func(t *testing.T, dir string, m *sourceManifest) {
			path := filepath.Join(dir, m.Icons[0].VendoredPath)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, append(data, '\n'), 0600); err != nil {
				t.Fatal(err)
			}
		}, "SHA-256"},
		{"missing license", func(t *testing.T, dir string, m *sourceManifest) {
			if err := os.Remove(filepath.Join(dir, "LICENSE.feather")); err != nil {
				t.Fatal(err)
			}
		}, "LICENSE.feather"},
		{"changed license", func(t *testing.T, dir string, m *sourceManifest) {
			if err := os.WriteFile(filepath.Join(dir, "LICENSE.feather"), []byte("replacement"), 0600); err != nil {
				t.Fatal(err)
			}
		}, "SHA-256"},
		{"missing timestamp", func(t *testing.T, dir string, m *sourceManifest) {
			m.Icons[0].RetrievedAt = ""
		}, "retrieved_at"},
		{"floating source", func(t *testing.T, dir string, m *sourceManifest) {
			for _, source := range m.Sources {
				if source.ID == m.Icons[0].Source {
					m.Icons[0].SourceURL = strings.Replace(m.Icons[0].SourceURL, source.Commit, "main", 1)
				}
			}
		}, "unpinned"},
		{"unrecorded SVG", func(t *testing.T, dir string, m *sourceManifest) {
			if err := os.WriteFile(filepath.Join(dir, "svg/unrecorded.svg"), []byte(testSVG), 0600); err != nil {
				t.Fatal(err)
			}
		}, "record every"},
		{"missing record", func(t *testing.T, dir string, m *sourceManifest) {
			m.Icons = m.Icons[1:]
		}, "record every"},
		{"duplicate record", func(t *testing.T, dir string, m *sourceManifest) {
			m.Icons[1] = m.Icons[0]
		}, "duplicate"},
		{"modified artwork", func(t *testing.T, dir string, m *sourceManifest) {
			m.Icons[0].Modifications = []string{"redrawn"}
		}, "unmodified"},
		{"unknown source", func(t *testing.T, dir string, m *sourceManifest) {
			m.Icons[0].Source = "unrecorded"
		}, "unknown source"},
		{"missing secondary license", func(t *testing.T, dir string, m *sourceManifest) {
			if err := os.Remove(filepath.Join(dir, "LICENSE.lucide")); err != nil {
				t.Fatal(err)
			}
		}, "LICENSE.lucide"},
		{"unknown alias target", func(t *testing.T, dir string, m *sourceManifest) {
			m.Aliases["card"] = "missing-icon"
		}, "alias"},
		{"shadowed drawing", func(t *testing.T, dir string, m *sourceManifest) {
			m.Aliases["search"] = "check"
		}, "alias"},
		{"generated identifier collision", func(t *testing.T, dir string, m *sourceManifest) {
			m.Aliases["bar-chart2"] = "bar-chart-2"
		}, "identifier"},
		{"handwritten function collision", func(t *testing.T, dir string, m *sourceManifest) {
			m.Aliases["names"] = "search"
		}, "identifier"},
		{"handwritten type collision", func(t *testing.T, dir string, m *sourceManifest) {
			m.Aliases["category"] = "search"
		}, "identifier"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, m := sourceFixture(t)
			tc.change(t, dir, &m)
			writeFixtureManifest(t, dir, m)
			before, err := os.ReadFile(filepath.Join(dir, "icons_gen.go"))
			if err != nil {
				t.Fatal(err)
			}
			t.Chdir(dir)
			if err := generate(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected generation to refuse %s with %q, got %v", tc.name, tc.want, err)
			}
			after, err := os.ReadFile("icons_gen.go")
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) {
				t.Fatal("failed provenance verification overwrote generated data")
			}
		})
	}
}

func TestGeneratedDataRetainsUpstreamLicense(t *testing.T) {
	m, license, err := loadSources("../..")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("../../icons_gen.go")
	if err != nil {
		t.Fatal(err)
	}
	got := strings.ReplaceAll(string(data), "\r\n", "\n")
	for _, line := range strings.Split(strings.TrimSpace(license), "\n") {
		if !strings.Contains(got, "// "+strings.TrimSuffix(line, "\r")+"\n") && strings.TrimSpace(line) != "" {
			t.Errorf("generated data lost upstream license line %q", line)
		}
	}
	for _, source := range m.Sources {
		if !strings.Contains(got, source.Commit) || !strings.Contains(got, source.RetrievedAt) {
			t.Fatal("generated data lost its pinned source or retrieval timestamp")
		}
	}
}

func TestGenerationRefusesEscapingOutputSymlink(t *testing.T) {
	dir, _ := sourceFixture(t)
	out := filepath.Join(t.TempDir(), "sentinel.go")
	const sentinel = "preserve external contents\n"
	if err := os.WriteFile(out, []byte(sentinel), 0600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "icons_gen.go")
	if err := os.Remove(output); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(out, output); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	t.Chdir(dir)
	if err := generate(); err == nil {
		t.Fatal("generator followed an escaping output symlink")
	}
	got, err := os.ReadFile(out)
	if err != nil || string(got) != sentinel {
		t.Fatalf("external output changed: %q, %v", got, err)
	}
}

func TestEmittedSVGReadRejectsReplacementAfterManifestValidation(t *testing.T) {
	dir, manifest := sourceFixture(t)
	icon := manifest.Icons[0]
	if err := os.WriteFile(filepath.Join(dir, icon.VendoredPath), []byte(testSVG), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if _, err := readVerifiedSVG(root, icon.VendoredPath, icon.SHA256); err == nil {
		t.Fatal("emission read accepted SVG bytes changed after manifest validation")
	}
}
