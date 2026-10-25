package appgen

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/codegen"
	"github.com/DonaldMurillo/gofastr/framework"
)

// Same property, second surface: the .env written by `gofastr generate` from
// a secrets-bearing app spec is owner-only too, on create AND on overwrite.
//
// renderAppSpecEnv puts JWT_SECRET / DATABASE_URL / ADMIN_SEED_PASSWORD
// into the generated .env ("Secrets never land in committed source",
// appspec.go), and generate routes every emitted file through
// codegen.WriteFiles — the full run and --add/--force alike — whose single
// write call is os.WriteFile(..., 0o644): a mode that only applies when the
// file is CREATED. init's writeEnvFile (pinned above) already solves exactly
// this class with an open-handle chmod before the write; the generate path
// must not be the looser sibling, or `gofastr generate app --force` over a
// project whose .env is 0644 rewrites the signing key and DB credentials
// and leaves them world-readable.
func TestGeneratedEnvIsOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits")
	}
	bp := appspec.AppSpec{
		App: appspec.AppSpecApp{
			Name:     "Envperm",
			Module:   "example.com/envperm",
			DBDriver: "postgres",
			// One credentialed URL DSN + one auth secret: both classes
			// renderAppSpecEnv writes into the generated .env.
			DBURL: "postgres://user:pw@db:5432/envperm",
			Auth:  appspec.AppSpecAuth{Enabled: true, JWTSecret: "shhh"},
		},
		Entities: []framework.EntityDeclaration{{
			Name:   "notes",
			Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}},
		}},
	}
	files, err := RenderAppSpecFiles(bp)
	if err != nil {
		t.Fatalf("renderAppSpecFiles: %v", err)
	}
	hasEnv := false
	for _, f := range files {
		if f.Name == ".env" {
			hasEnv = true
		}
	}
	if !hasEnv {
		t.Fatal("app spec rendered no .env: the secret-bearing artifact this test asserts on is absent, so the property is untested")
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)

	for _, tc := range []struct {
		name    string
		preMode os.FileMode // 0 = no pre-existing file
	}{
		{"fresh generate", 0},
		{"--force over world-readable", 0o644},
		{"--force over group-writable", 0o664},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Chdir(dir); err != nil {
				t.Fatal(err)
			}
			if tc.preMode != 0 {
				if err := os.WriteFile(".env", []byte("STALE=1\n"), tc.preMode); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(".env", tc.preMode); err != nil {
					t.Fatal(err) // defeat umask so the precondition is exact
				}
			}

			// The exact write path of `gofastr generate` (full run and
			// --force/--add): fileSetFromGeneratedFiles → WriteFiles with
			// ConflictOverwrite (generate.go).
			fileSet, err := FileSetFromGeneratedFiles(files, "app")
			if err != nil {
				t.Fatalf("fileSetFromGeneratedFiles: %v", err)
			}
			if err := codegen.WriteFiles(fileSet, codegen.WriteOptions{
				OutputRoot:   ".",
				SkipManifest: true,
				Conflict:     codegen.ConflictOverwrite,
			}); err != nil {
				t.Fatalf("WriteFiles: %v", err)
			}

			info, err := os.Stat(filepath.Join(dir, ".env"))
			if err != nil {
				t.Fatal(err)
			}
			if got := info.Mode().Perm(); got&0o077 != 0 {
				t.Errorf("SECURITY: generated .env is %04o — readable beyond its owner", got)
			}
		})
	}
}
