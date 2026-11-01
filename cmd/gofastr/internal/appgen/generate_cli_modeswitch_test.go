package appgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// runOpenAPICLI runs the --from-openapi generator for binary oapp and
// returns its exit code (-1 when it did not exit) and output.
func runOpenAPICLI(t *testing.T, extra ...string) (int, string) {
	t.Helper()
	code := -1
	out := clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() {
			RunGenerateCLI(append([]string{"--from-openapi=openapi.json", "--binary=oapp"}, extra...))
		})
	})
	return code, out
}

// Issue #614: custom.go is never rewritten, so moving between the entity
// and --from-openapi modes kept a custom.go importing the other mode's
// client, and the CLI stopped building.
func TestCLIModeSwitchRefusesStaleCustom(t *testing.T) {
	dir := oaTempModule(t)
	if code, out := runOpenAPICLI(t); code != -1 {
		t.Fatalf("first run exit %d:\n%s", code, out)
	}
	custom := filepath.Join(dir, "cmd", "oapp", "custom.go")
	data, err := os.ReadFile(custom)
	if err != nil {
		t.Fatal(err)
	}
	// The custom.go an entity-mode run writes imports the entity SDK.
	stale := strings.Replace(string(data), `"example.com/oapp/cmd/oapp/internal/client"`, `"example.com/oapp/entities/client"`, 1)
	if stale == string(data) {
		t.Fatalf("custom.go does not import the OpenAPI client:\n%s", data)
	}
	stale += "\n// my command\n"
	if err := os.WriteFile(custom, []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}

	code, out := runOpenAPICLI(t)
	if code != 1 || !strings.Contains(out, "custom.go") || !strings.Contains(out, "--force") {
		t.Fatalf("want a refusal naming custom.go and --force, exit %d:\n%s", code, out)
	}
	if got, _ := os.ReadFile(custom); string(got) != stale {
		t.Fatal("the refusal touched custom.go")
	}

	if code, out := runOpenAPICLI(t, "--force"); code != -1 {
		t.Fatalf("--force exit %d:\n%s", code, out)
	}
	if got, _ := os.ReadFile(custom + ".bak"); string(got) != stale {
		t.Fatalf("custom.go.bak does not hold the old custom.go:\n%s", got)
	}
	fresh, _ := os.ReadFile(custom)
	if !strings.Contains(string(fresh), `"example.com/oapp/cmd/oapp/internal/client"`) {
		t.Fatalf("--force did not write a custom.go for this mode:\n%s", fresh)
	}
}

// --force never overwrites an earlier backup: that file may hold the only
// copy of the user's commands.
func TestCLIModeSwitchKeepsOldBackup(t *testing.T) {
	dir := oaTempModule(t)
	if code, out := runOpenAPICLI(t); code != -1 {
		t.Fatalf("first run exit %d:\n%s", code, out)
	}
	cliDir := filepath.Join(dir, "cmd", "oapp")
	stale := "package main\n\nimport client \"example.com/oapp/entities/client\"\n\nfunc customCommands() []command { return nil }\n\nfunc configureClient(c *client.Client) {}\n"
	if err := os.WriteFile(filepath.Join(cliDir, "custom.go"), []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cliDir, "custom.go.bak"), []byte("earlier backup"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out := runOpenAPICLI(t, "--force")
	if code != 1 || !strings.Contains(out, "custom.go.bak") {
		t.Fatalf("want a refusal naming custom.go.bak, exit %d:\n%s", code, out)
	}
	if got, _ := os.ReadFile(filepath.Join(cliDir, "custom.go.bak")); string(got) != "earlier backup" {
		t.Fatal("--force overwrote an earlier custom.go.bak")
	}
}

// staleCustomClient names the other mode's client in either direction and
// stays quiet on a custom.go importing this mode's client or neither.
func TestStaleCustomClientBothModes(t *testing.T) {
	entity := cliSpec{ClientImport: "example.com/app/entities/client"}
	openapi := cliSpec{ClientImport: "example.com/app/cmd/app/internal/client", SelfClient: true}
	cases := []struct {
		name   string
		spec   cliSpec
		custom string
		want   string
	}{
		{"entity run, openapi custom", entity, "example.com/app/cmd/app/internal/client", "example.com/app/cmd/app/internal/client"},
		{"openapi run, entity custom", openapi, "example.com/app/entities/client", "example.com/app/entities/client"},
		{"entity run, entity custom", entity, "example.com/app/entities/client", ""},
		{"openapi run, openapi custom", openapi, "example.com/app/cmd/app/internal/client", ""},
		{"neither", entity, "net/http", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			src := "package main\n\nimport x \"" + c.custom + "\"\n\nvar _ = x.X\n"
			if err := os.WriteFile(filepath.Join(dir, "custom.go"), []byte(src), 0o600); err != nil {
				t.Fatal(err)
			}
			if got := staleCustomClient(dir, "cmd/app", c.spec); got != c.want {
				t.Fatalf("staleCustomClient = %q, want %q", got, c.want)
			}
		})
	}
}
