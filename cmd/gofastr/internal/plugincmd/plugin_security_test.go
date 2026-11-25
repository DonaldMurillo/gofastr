package plugincmd

import (
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/core/cliplugin"
)

func TestUntrustFromChildWithBrokenSettings(t *testing.T) {
	project := cliplugin.CanonicalProjectDir(clitest.WriteModule(t, map[string]string{
		"gofastr.settings.yml": "plugins:\n  - name: check\n    commnad: [./check]\n",
		"internal/keep":        "",
	}))
	other := cliplugin.CanonicalProjectDir(t.TempDir())
	registry := filepath.Join(t.TempDir(), "plugins.yml")
	t.Setenv(cliplugin.EnvConfigPath, registry)
	cfg := cliplugin.UserConfig{
		Version: cliplugin.ConfigVersion,
		TrustedProjects: []cliplugin.TrustedProjectPlugin{
			{ProjectDir: project, Name: "check", Command: []string{filepath.Join(project, "check")}},
			{ProjectDir: other, Name: "check", Command: []string{filepath.Join(other, "check")}},
		},
	}
	if err := cliplugin.SaveUserConfig(registry, cfg); err != nil {
		t.Fatal(err)
	}
	t.Chdir(filepath.Join(project, "internal"))
	var code int
	out := clitest.CovT_capOutput(t, func() {
		code = clitest.CovT_capExit(t, func() { RunPlugin([]string{"untrust", "check"}) })
	})
	if code != -1 {
		t.Fatalf("untrust from project subdirectory exited %d: %s", code, out)
	}
	got, err := cliplugin.LoadUserConfig(registry)
	if err != nil {
		t.Fatal(err)
	}
	if records := got.TrustedProjects; len(records) != 1 || records[0].ProjectDir != other {
		t.Errorf("trust records after untrust = %+v, want only the other project's record", records)
	}
}
