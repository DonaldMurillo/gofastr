package packcmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/core/dotenv"
)

func fileNames(files []appgen.GeneratedFile) []string {
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = f.Name
	}
	return names
}

// Values the generator writes to .env must round-trip through BOTH
// readers of that file: core/dotenv (the generated app and its e2e test)
// and `gofastr pack`. Quote-leading or space-edged secrets used to be
// written bare, so one reader mangled them and the other failed outright.
func TestEnvValuesRoundTrip(t *testing.T) {
	jwt := `"leading quote" and $dollar`
	seed := `'quoted' #hash \backslash`
	dsn := "postgres://app:plainpw@db:5432/app"

	var bp appspec.AppSpec
	bp.App.Name = "RT"
	bp.App.Module = "example.com/rt"
	bp.App.DBURL = dsn
	bp.App.Auth.Enabled = true
	bp.App.Auth.JWTSecret = jwt
	bp.App.Admin.SeedEmail = "admin@example.com"
	bp.App.Admin.SeedPassword = seed

	env := appgen.RenderAppSpecEnv(bp)
	want := map[string]string{"JWT_SECRET": jwt, "DATABASE_URL": dsn, "ADMIN_SEED_PASSWORD": seed}

	parsed, err := dotenv.Parse(strings.NewReader(env))
	if err != nil {
		t.Fatalf("core/dotenv cannot parse the generated .env: %v\n%s", err, env)
	}
	for k, v := range want {
		if parsed[k] != v {
			t.Errorf("dotenv round-trip %s = %q, want %q", k, parsed[k], v)
		}
	}

	path := filepath.Join(t.TempDir(), ".env")
	clitest.WriteTestFile(t, path, env)
	packed := packReadDotEnv(path)
	for k, v := range want {
		if packed[k] != v {
			t.Errorf("pack round-trip %s = %q, want %q", k, packed[k], v)
		}
	}
}
