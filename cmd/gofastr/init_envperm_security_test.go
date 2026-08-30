package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/DonaldMurillo/gofastr/codegen"
	"github.com/DonaldMurillo/gofastr/framework"
)

// Property: the scaffolded .env is never readable by anyone but its owner.
//
// It carries DATABASE_URL and is the documented home of GOFASTR_SECRET, the
// session-signing key.
//
// The pre-existing case is the one that matters: os.WriteFile applies its
// mode only when it CREATES a file, so `gofastr init .` in a directory that
// already holds a 0644 .env would have truncated it, written the secrets in,
// and left it world-readable, the permission argument silently doing
// nothing on exactly the path where it was needed.
func TestEnvFileIsOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits")
	}
	for _, tc := range []struct {
		name    string
		preMode os.FileMode // 0 = no pre-existing file
	}{
		{"new file", 0},
		{"pre-existing world-readable", 0o644},
		{"pre-existing group-writable", 0o664},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), ".env")
			if tc.preMode != 0 {
				if err := os.WriteFile(path, []byte("STALE=1\n"), tc.preMode); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, tc.preMode); err != nil {
					t.Fatal(err) // defeat umask so the precondition is exact
				}
			}

			if err := writeEnvFile(path, "GOFASTR_SECRET=shhh\n"); err != nil {
				t.Fatalf("writeEnvFile: %v", err)
			}

			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := info.Mode().Perm(); got&0o077 != 0 {
				t.Errorf("SECURITY: .env is %04o — readable beyond its owner", got)
			}
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != "GOFASTR_SECRET=shhh\n" {
				t.Errorf("content = %q, want the new content with no stale remnant", body)
			}
		})
	}
}

// Same property, second surface: the .env written by `gofastr generate` from
// a secrets-bearing blueprint is owner-only too, on create AND on overwrite.
//
// renderBlueprintEnv puts JWT_SECRET / DATABASE_URL / ADMIN_SEED_PASSWORD
// into the generated .env ("Secrets never land in committed source",
// blueprint.go), and generate routes every emitted file through
// codegen.WriteFiles — the full run and --add/--force alike — whose single
// write call is os.WriteFile(..., 0o644): a mode that only applies when the
// file is CREATED. init's writeEnvFile (pinned above) already solves exactly
// this class with an open-handle chmod before the write; the generate path
// must not be the looser sibling, or `gofastr generate --force` over a
// project whose .env is 0644 rewrites the signing key and DB credentials
// and leaves them world-readable.
func TestGeneratedEnvIsOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits")
	}
	bp := Blueprint{
		App: BlueprintApp{
			Name:     "Envperm",
			Module:   "example.com/envperm",
			DBDriver: "postgres",
			// One credentialed URL DSN + one auth secret: both classes
			// renderBlueprintEnv writes into the generated .env.
			DBURL: "postgres://user:pw@db:5432/envperm",
			Auth:  BlueprintAuth{Enabled: true, JWTSecret: "shhh"},
		},
		Entities: []framework.EntityDeclaration{{
			Name:   "notes",
			Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}},
		}},
	}
	files, err := renderBlueprintFiles(bp)
	if err != nil {
		t.Fatalf("renderBlueprintFiles: %v", err)
	}
	hasEnv := false
	for _, f := range files {
		if f.name == ".env" {
			hasEnv = true
		}
	}
	if !hasEnv {
		t.Fatal("blueprint rendered no .env: the secret-bearing artifact this test asserts on is absent, so the property is untested")
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
			fileSet, err := fileSetFromGeneratedFiles(files, "blueprint")
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
