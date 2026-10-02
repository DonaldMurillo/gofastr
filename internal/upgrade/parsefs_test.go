package upgrade

import (
	"strings"
	"testing"
	"testing/fstest"
)

const fsTop = "through: v0.3.0\n"

func fsRelease(version string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte("version: " + version + "\nnotes:\n  - change: c\n    guidance: g\n    nodetect: none\n")}
}

func TestParseFSSortsBySemver(t *testing.T) {
	reg, err := ParseFS(fstest.MapFS{
		"registry.yml":          {Data: []byte(fsTop)},
		"releases/v0.10.0.yml":  fsRelease("v0.10.0"),
		"releases/v0.9.0.yml":   fsRelease("v0.9.0"),
		"releases/v0.10.1.yml":  fsRelease("v0.10.1"),
		"releases/notyaml.txt":  {Data: []byte("ignored")},
		"releases/v0.2.0.yml":   fsRelease("v0.2.0"),
		"releases/v0.100.0.yml": fsRelease("v0.100.0"),
	})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range reg.Releases {
		got = append(got, r.Version)
	}
	want := "v0.2.0 v0.9.0 v0.10.0 v0.10.1 v0.100.0"
	if strings.Join(got, " ") != want {
		t.Errorf("order %v, want %s", got, want)
	}
	if n := reg.Releases[0].Notes[0]; n.File != "releases/v0.2.0.yml" || n.Line != 3 {
		t.Errorf("note position %s:%d, want releases/v0.2.0.yml:3", n.File, n.Line)
	}
}

func TestParseFSRefusesMisnamedFile(t *testing.T) {
	_, err := ParseFS(fstest.MapFS{
		"registry.yml":         {Data: []byte(fsTop)},
		"releases/v0.9.0.yml":  fsRelease("v0.9.0"),
		"releases/v0.11.0.yml": fsRelease("v0.10.0"),
	})
	if err == nil || !strings.HasPrefix(err.Error(), "releases/v0.11.0.yml:1:") || !strings.Contains(err.Error(), "name it releases/v0.10.0.yml") {
		t.Fatalf("err = %v, want a misnamed-file refusal at releases/v0.11.0.yml:1", err)
	}
}

func TestParseFSRefusesReleasesInTop(t *testing.T) {
	_, err := ParseFS(fstest.MapFS{
		"registry.yml": {Data: []byte(fsTop + "releases: []\n")},
	})
	if err == nil || !strings.HasPrefix(err.Error(), "registry.yml:2:") {
		t.Fatalf("err = %v, want registry.yml:2: unknown key releases", err)
	}
}

func TestParseFSNamesFileInNoteErrors(t *testing.T) {
	_, err := ParseFS(fstest.MapFS{
		"registry.yml":        {Data: []byte(fsTop)},
		"releases/v0.9.0.yml": {Data: []byte("version: v0.9.0\nnotes:\n  - change: c\n    bogus: 1\n")},
	})
	if err == nil || !strings.HasPrefix(err.Error(), "releases/v0.9.0.yml:4:") {
		t.Fatalf("err = %v, want releases/v0.9.0.yml:4:", err)
	}
}
