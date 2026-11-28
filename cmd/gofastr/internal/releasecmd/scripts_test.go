package releasecmd

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func scriptsFor(t *testing.T, targets ...target) ([]string, string) {
	t.Helper()
	dir := t.TempDir()
	r, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	res := result{name: "hello"}
	for _, tg := range targets {
		res.artifacts = append(res.artifacts, artifact{target: tg, file: archiveName("hello", "1.0.0", tg), sha256: strings.Repeat("a", 64)})
	}
	written, err := writeScripts(&outDir{root: r}, scriptData(res, options{version: "1.0.0", baseURL: "https://example.com/dl"}))
	if err != nil {
		t.Fatal(err)
	}
	sh, _ := os.ReadFile(dir + "/install.sh")
	return written, string(sh)
}

func TestEachScriptOnlyForItsPlatforms(t *testing.T) {
	if got, _ := scriptsFor(t, target{"linux", "amd64"}); !slices.Equal(got, []string{"install.sh"}) {
		t.Errorf("a Unix-only release wrote %v", got)
	}
	if got, _ := scriptsFor(t, target{"windows", "amd64"}); !slices.Equal(got, []string{"install.ps1"}) {
		t.Errorf("a Windows-only release wrote %v", got)
	}
}

func TestRosettaShellGetsTheArm64Build(t *testing.T) {
	if _, sh := scriptsFor(t, target{"darwin", "amd64"}, target{"darwin", "arm64"}); !strings.Contains(sh, "proc_translated") {
		t.Error("with a darwin/arm64 build, install.sh does not send a Rosetta shell to it")
	}
	if _, sh := scriptsFor(t, target{"darwin", "amd64"}); strings.Contains(sh, "proc_translated") {
		t.Error("with no darwin/arm64 build, install.sh sends a Rosetta shell to one")
	}
}
