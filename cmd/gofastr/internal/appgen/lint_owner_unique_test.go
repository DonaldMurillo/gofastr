package appgen

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/framework"
)

// An owner-scoped entity with a uniqueness rule that leaves out the owner
// column makes one account's row block every other account's create, and
// the 409 tells the caller the value exists in someone else's rows. The
// lint warns (never blocks): a globally unique value on per-user rows can
// be deliberate (a public handle).

func ownerUniqueEntity() framework.EntityDeclaration {
	return framework.EntityDeclaration{
		Name:  "customers",
		Scope: &framework.ScopeDeclaration{OwnerField: "user_id"},
		Fields: []framework.FieldDeclaration{
			{Name: "email", Type: "string"},
			{Name: "user_id", Type: "string", Hidden: true},
		},
	}
}

func TestOwnerScopedUniqueFieldWarns(t *testing.T) {
	decl := ownerUniqueEntity()
	decl.Fields[0].Unique = true
	got := LintOwnerScopedUnique(appspec.AppSpec{Entities: []framework.EntityDeclaration{decl}})
	if len(got) != 1 {
		t.Fatalf("want 1 finding, got %d: %+v", len(got), got)
	}
	msg := got[0].Message()
	for _, want := range []string{`"customers"`, "email", "user_id", "indices", "409"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message should mention %q:\n%s", want, msg)
		}
	}
}

func TestOwnerScopedUniqueIndexWarns(t *testing.T) {
	decl := ownerUniqueEntity()
	decl.Indices = []framework.Index{{Name: "idx_email", Columns: []string{"email"}, Unique: true}}
	got := LintOwnerScopedUnique(appspec.AppSpec{Entities: []framework.EntityDeclaration{decl}})
	if len(got) != 1 || !strings.Contains(got[0].Message(), "idx_email") {
		t.Fatalf("want 1 finding naming idx_email, got %+v", got)
	}
}

func TestOwnerScopedUniqueQuietCases(t *testing.T) {
	cases := map[string]func(*framework.EntityDeclaration){
		"composite with owner": func(d *framework.EntityDeclaration) {
			d.Indices = []framework.Index{{Name: "i", Columns: []string{"user_id", "email"}, Unique: true}}
		},
		"non-unique index": func(d *framework.EntityDeclaration) {
			d.Indices = []framework.Index{{Name: "i", Columns: []string{"email"}}}
		},
		"unique owner column": func(d *framework.EntityDeclaration) {
			d.Fields[1].Unique = true
		},
		// A read_only value is server-assigned (ecommerce's order_number,
		// auto_generate: uuid): no caller can collide with it or probe it.
		"read_only field": func(d *framework.EntityDeclaration) {
			d.Fields[0].Unique, d.Fields[0].ReadOnly = true, true
		},
		"read_only index": func(d *framework.EntityDeclaration) {
			d.Fields[0].ReadOnly = true
			d.Indices = []framework.Index{{Name: "i", Columns: []string{"email", "created_at"}, Unique: true}}
		},
		"no owner_field": func(d *framework.EntityDeclaration) {
			d.Scope = nil
			d.Fields[0].Unique = true
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			decl := ownerUniqueEntity()
			mutate(&decl)
			if got := LintOwnerScopedUnique(appspec.AppSpec{Entities: []framework.EntityDeclaration{decl}}); len(got) != 0 {
				t.Fatalf("want no findings, got %+v", got)
			}
		})
	}
}

func TestGenerateWarnsOwnerScopedUnique(t *testing.T) {
	dir := t.TempDir()
	clitest.CovT_chdir(t, dir)
	clitest.WriteFileAll(t, filepath.Join(dir, "bp.yml"), clitest.OwnerUniqueYML)
	out := clitest.CovT_capStdout(t, func() {
		clitest.CovT_capExit(t, func() {
			GenerateFromAppSpec(GenerateOptions{Spec: filepath.Join(dir, "bp.yml"), outputDir: "gen", dryRun: true})
		})
	})
	if !strings.Contains(out, `"customers"`) || !strings.Contains(out, "409") {
		t.Fatalf("generate should warn about customers.email:\n%s", out)
	}
}

// The shipped app specs are what readers copy; none of them may carry
// the shape this lint warns about.
//
//gofastr:test-reads examples/*/gofastr.yml
func TestShippedAppSpecsScopeUnique(t *testing.T) {
	paths := clitest.ExampleAppSpecPaths(t)
	if len(paths) == 0 {
		t.Fatal("no shipped app specs found")
	}
	for _, path := range paths {
		bp, lerr := appspec.LoadAppSpec(path)
		if lerr != nil {
			t.Fatalf("load %s: %v", path, lerr)
		}
		for _, f := range LintOwnerScopedUnique(bp) {
			t.Errorf("%s: %s", path, f.Message())
		}
	}
}
